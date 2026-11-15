package hc_test

import (
	"bytes"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// code is a pointer to n, for a terminal's exit code or signal.
func code(n int) *int { return &n }

// revierTerminal is a terminal of revier's instance 0123456789abcdef.
func revierTerminal(address, state, reason string) *fakeTerminal {
	return &fakeTerminal{
		Address: address, State: state, Reason: reason, Argv: []string{"/usr/bin/claude", "--resume"},
		Labels: map[string]string{"revier.instance": "0123456789abcdef", "revier.panel": "0"},
	}
}

// A window whose terminal's program ended on its own shows what the program
// left and how it ended, holds the terminal while it waits for Enter, and
// removes it on Enter (decisions.md D187).
func TestAWindowHoldsATerminalWhoseProgramEnded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		exit, sig  *int
		wantEnding string
	}{
		{"an exit code", code(3), nil, "[claude exited with code 3; press Enter to close]"},
		{"exit 0", code(0), nil, "[claude exited with code 0; press Enter to close]"},
		{"a signal", nil, code(11), "[claude ended on signal 11; press Enter to close]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, sock := newCore(t)
			c.read = "boom: no such file\n"
			term := revierTerminal("t1", "finished", "exited")
			term.ExitCode, term.Signal = tc.exit, tc.sig
			c.terminals = []*fakeTerminal{term}
			var out bytes.Buffer
			held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader("\r"), &out)
			if err != nil || !held {
				t.Fatalf("Hold = %v, %v; want held", held, err)
			}
			if want := "boom: no such file\n" + tc.wantEnding; out.String() != want {
				t.Errorf("the window shows %q, want %q", out.String(), want)
			}
			if labels := c.paramsOf("terminal.label"); len(labels) != 1 || asMap(labels[0]["labels"])["revier.held"] != "1" {
				t.Errorf("labels set = %v, want the terminal marked held", labels)
			}
			if reads := c.paramsOf("terminal.read"); len(reads) != 1 || reads[0]["lines"] != float64(1000) {
				t.Errorf("reads = %v, want one with the history above the screen", reads)
			}
			if records := c.records(); len(records) != 0 {
				t.Errorf("records = %q, want the terminal removed on Enter", records)
			}
		})
	}
}

// A read that fails still shows how the program ended: the window says so
// in place of the text.
func TestAWindowWhoseReadFailsStillShowsTheEnd(t *testing.T) {
	c, sock := newCore(t)
	c.readErr = map[string]any{"code": -32000, "message": "busy", "data": map[string]string{"kind": "busy"}}
	term := revierTerminal("t1", "finished", "exited")
	term.ExitCode = code(1)
	c.terminals = []*fakeTerminal{term}
	var out bytes.Buffer
	if held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader("\n"), &out); err != nil || !held {
		t.Fatalf("Hold = %v, %v; want held", held, err)
	}
	if want := "(revier could not read what it left)\n[claude exited with code 1; press Enter to close]"; out.String() != want {
		t.Errorf("the window shows %q, want %q", out.String(), want)
	}
}

// A held terminal that revier removes, as a close or a shutdown does, ends
// its window with no Enter.
func TestAHeldWindowEndsWhenRevierRemovesItsTerminal(t *testing.T) {
	c, sock := newCore(t)
	c.terminals = []*fakeTerminal{revierTerminal("t1", "finished", "exited")}
	never, _ := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", never, io.Discard)
		done <- err
	}()
	for len(c.paramsOf("terminal.label")) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	c.terminals = nil
	c.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Hold = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held window still waits after its terminal was removed")
	}
	if rms := c.paramsOf("terminal.rm"); len(rms) != 0 {
		t.Errorf("removed %v, want nothing: the terminal was gone", rms)
	}
}

// A detach leaves a live terminal, a close kills one, a shell hc removes as
// it exits, and a terminal started by hand is the user's: none of them holds
// the window, and nothing is removed, as the close removes its own.
func TestAWindowIsNotHeldAfterADetachACloseAShellsExitOrForAUsersTerminal(t *testing.T) {
	byHand := revierTerminal("t1", "finished", "exited")
	byHand.Labels = map[string]string{}
	for name, terminals := range map[string][]*fakeTerminal{
		"a detach":     {revierTerminal("t1", "live", "")},
		"a close":      {revierTerminal("t1", "finished", "killed")},
		"a shell exit": nil,
		"by hand":      {byHand},
	} {
		t.Run(name, func(t *testing.T) {
			c, sock := newCore(t)
			c.terminals = terminals
			var out bytes.Buffer
			held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader(""), &out)
			if err != nil || held || out.Len() != 0 {
				t.Errorf("Hold = %v, %v and showed %q; want not held, nothing shown", held, err, out.String())
			}
			if len(c.records()) != len(terminals) || len(c.paramsOf("terminal.label")) != 0 {
				t.Errorf("records = %q, labels set %v; want the terminal left as it was", c.records(), c.paramsOf("terminal.label"))
			}
		})
	}
}

// A held terminal is still its workspace's panel, so a press raises the
// workspace and a key in the held window is the workspace's, as a held
// kitty window is (decisions.md D187); a finished one no window holds is
// no instance.
func TestInstancesListAHeldTerminal(t *testing.T) {
	c, sock := newCore(t)
	held := revierTerminal("t1", "finished", "exited")
	held.Labels["revier.held"] = "1"
	other := revierTerminal("t2", "finished", "exited")
	other.Labels["revier.instance"] = "fedcba9876543210"
	c.terminals = []*fakeTerminal{held, other}
	got, err := (&hc.Host{Socket: sock}).Instances(t.Context())
	if err != nil || len(got) != 1 || len(got[0].Panels) != 1 || got[0].Panels[0].ID != "t1" {
		t.Fatalf("Instances = %+v, %v; want the held terminal's instance alone", got, err)
	}
	if _, ok := got[0].Panels[0].Vars["revier.held"]; ok {
		t.Errorf("panel vars = %v, want revier's held label left out", got[0].Panels[0].Vars)
	}
}

// Once the core is gone, as after "hc server stop", the window ends: it
// dials the socket alone, as any hc command would start a core.
func TestAWindowIsNotHeldWhenTheCoreIsGone(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	var out bytes.Buffer
	held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader(""), &out)
	if err != nil || held || out.Len() != 0 {
		t.Errorf("Hold = %v, %v and showed %q; want not held", held, err, out.String())
	}
}

// An Open removes each terminal of revier's that ended over a minute ago,
// which no window took, as with no view or kitty closed; one that ended a
// moment ago waits for its window, one a window holds is the window's, one
// started by hand is the user's, and one on another machine is its revier's.
func TestAnOpenSweepsTheTerminalsNoWindowTook(t *testing.T) {
	c, sock := newCore(t)
	instance := map[string]string{"revier.instance": "0123456789abcdef"}
	old := time.Now().Add(-2 * time.Minute).UnixMilli()
	c.terminals = []*fakeTerminal{
		{Address: "t1", State: "finished", Reason: "exited", FinishedMS: old, Labels: instance},
		{Address: "t2", State: "finished", Reason: "exited", FinishedMS: time.Now().UnixMilli(), Labels: instance},
		{Address: "t3", State: "finished", Reason: "exited", FinishedMS: old, Labels: map[string]string{}},
		{Address: "t4", State: "live", Labels: instance},
		{Address: "t5", State: "finished", Reason: "exited", FinishedMS: old, Labels: map[string]string{"revier.instance": "0123456789abcdef", "revier.held": "1"}},
		{Address: "box/t1", State: "finished", Reason: "exited", FinishedMS: old, Labels: instance},
	}
	c.runs = 5
	if _, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := c.records(); !slices.Equal(got, []string{"t2", "t3", "t4", "t5", "box/t1", "t6"}) {
		t.Errorf("records = %q, want t1 swept and the rest left", got)
	}
}

// A view's window runs revier's attach when the host names revier's binary,
// with hc by its path and the socket named.
func TestAttachPanelRunsRevierToHoldTheWindow(t *testing.T) {
	dir := hcLink(t)
	t.Setenv("PATH", dir)
	self := filepath.Join(fakeBin(t, "revier"), "revier")
	got, err := (&hc.Host{Socket: "/s.sock", Revier: self}).AttachPanel("t7")
	if want := []string{"env", "HC_SOCKET=/s.sock", self, hc.AttachCmd, filepath.Join(dir, "hc"), "t7"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("AttachPanel = %q, %v; want %q", got, err, want)
	}
	if _, err := (&hc.Host{Socket: "/s.sock", Revier: "/opt/a=b/revier"}).AttachPanel("t7"); err == nil {
		t.Error("AttachPanel with a \"=\" in revier's path passed; env would read it as a variable")
	}
	// An upgrade can remove the binary under a running revier: the window
	// then shows the terminal unheld rather than not at all.
	got, err = (&hc.Host{Socket: "/s.sock", Revier: filepath.Join(t.TempDir(), "gone")}).AttachPanel("t7")
	if want := []string{"env", "HC_SOCKET=/s.sock", filepath.Join(dir, "hc"), "attach", "t7"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("AttachPanel with revier gone = %q, %v; want %q", got, err, want)
	}
}
