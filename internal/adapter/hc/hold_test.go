package hc_test

import (
	"bytes"
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

// A window whose terminal's program ended on its own shows what the program
// left and how it ended, removes the terminal, and closes on Enter, as the
// kitty runtime holds a panel's command (decisions.md D187).
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
			c.terminals = []*fakeTerminal{{
				Address: "t1", State: "finished", Reason: "exited", ExitCode: tc.exit, Signal: tc.sig,
				Argv: []string{"/usr/bin/claude", "--resume"}, Labels: map[string]string{},
			}}
			var out bytes.Buffer
			held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader("\n"), &out)
			if err != nil || !held {
				t.Fatalf("Hold = %v, %v; want held", held, err)
			}
			if want := "boom: no such file\n" + tc.wantEnding; out.String() != want {
				t.Errorf("the window shows %q, want %q", out.String(), want)
			}
			if records := c.records(); len(records) != 0 {
				t.Errorf("records = %q, want the terminal removed", records)
			}
			if reads := c.paramsOf("terminal.read"); len(reads) != 1 || reads[0]["lines"] != float64(1000) {
				t.Errorf("reads = %v, want one with the history above the screen", reads)
			}
		})
	}
}

// A detach leaves a live terminal, a close kills one, and a shell hc removes
// as it exits: none of them holds the window, and nothing is removed, as the
// close removes its own.
func TestAWindowIsNotHeldAfterADetachACloseOrAShellsExit(t *testing.T) {
	for name, terminals := range map[string][]*fakeTerminal{
		"a detach":     {{Address: "t1", State: "live", Labels: map[string]string{}}},
		"a close":      {{Address: "t1", State: "finished", Reason: "killed", Labels: map[string]string{}}},
		"a shell exit": nil,
	} {
		t.Run(name, func(t *testing.T) {
			c, sock := newCore(t)
			c.terminals = terminals
			var out bytes.Buffer
			held, err := (&hc.Host{Socket: sock}).Hold(t.Context(), "t1", strings.NewReader(""), &out)
			if err != nil || held || out.Len() != 0 {
				t.Errorf("Hold = %v, %v and showed %q; want not held, nothing shown", held, err, out.String())
			}
			if len(c.records()) != len(terminals) {
				t.Errorf("records = %q, want the terminal left as it was", c.records())
			}
		})
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
// moment ago waits for its window, and one started by hand is the user's.
func TestAnOpenSweepsTheTerminalsNoWindowTook(t *testing.T) {
	c, sock := newCore(t)
	instance := map[string]string{"revier.instance": "0123456789abcdef"}
	old := time.Now().Add(-2 * time.Minute).UnixMilli()
	c.terminals = []*fakeTerminal{
		{Address: "t1", State: "finished", Reason: "exited", FinishedMS: old, Labels: instance},
		{Address: "t2", State: "finished", Reason: "exited", FinishedMS: time.Now().UnixMilli(), Labels: instance},
		{Address: "t3", State: "finished", Reason: "exited", FinishedMS: old, Labels: map[string]string{}},
		{Address: "t4", State: "live", Labels: instance},
	}
	c.runs = 4
	if _, err := (&hc.Host{Socket: sock}).Open(t.Context(), revier.Realization{Name: "home", Launch: []string{"sh", "-c", "sleep 30"}}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := c.records(); !slices.Equal(got, []string{"t2", "t3", "t4", "t5"}) {
		t.Errorf("records = %q, want t1 swept and the rest left", got)
	}
}

// A view's window runs revier's attach when the host names revier's binary,
// with hc by its path and the socket named.
func TestAttachPanelRunsRevierToHoldTheWindow(t *testing.T) {
	dir := hcLink(t)
	t.Setenv("PATH", dir)
	got, err := (&hc.Host{Socket: "/s.sock", Revier: "/opt/revier"}).AttachPanel("t7")
	if want := []string{"env", "HC_SOCKET=/s.sock", "/opt/revier", hc.AttachCmd, filepath.Join(dir, "hc"), "t7"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("AttachPanel = %q, %v; want %q", got, err, want)
	}
	if _, err := (&hc.Host{Socket: "/s.sock", Revier: "/opt/a=b/revier"}).AttachPanel("t7"); err == nil {
		t.Error("AttachPanel with a \"=\" in revier's path passed; env would read it as a variable")
	}
}
