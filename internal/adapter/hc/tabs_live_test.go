//go:build live

// Layer L4: hc's tabs against a real core of the test's own, headless.
package hc_test

import (
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/pkg/revier"
)

// A tab is the terminals that share a token, after the tabs before it, and
// two tabs of the same vars stay two; the names OpenTab gives are names hc
// takes (decisions.md D179).
func TestOpenTabThenInstancesListEachTabInOrder(t *testing.T) {
	h, c := world(t), ctx(t)
	dir := t.TempDir()
	sleep := []string{"sh", "-c", "exec sleep 30"}
	ref, err := h.Open(c, revier.Realization{Name: "home", Vars: map[string]string{"revier_target": "tickets"}, Panels: []revier.PanelSpec{{Command: sleep, Dir: dir}}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent := revier.Realization{Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: sleep, Dir: dir}, {Kind: revier.PanelShell, Dir: dir}}}
	var firsts []revier.PanelID
	for range 2 {
		first, err := h.OpenTab(c, ref, agent, map[string]string{"revier_target": "agent"})
		if err != nil {
			t.Fatalf("OpenTab: %v", err)
		}
		firsts = append(firsts, first)
	}

	panels := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 && len(in[0].Panels) == 5 })[0].Panels
	if panels[1].ID != firsts[0] || panels[3].ID != firsts[1] {
		t.Errorf("OpenTab = %v, panels = %+v; want each tab's first panel after the tab before it", firsts, panels)
	}
	tabs := map[string]int{}
	for _, p := range panels {
		tabs[p.Tab]++
	}
	if len(tabs) != 3 || panels[1].Tab != panels[2].Tab || panels[3].Tab != panels[4].Tab || panels[0].Vars["revier_target"] != "tickets" || panels[4].Vars["revier_target"] != "agent" {
		t.Errorf("panels = %+v; want three tabs, the first under its own mark and two agent tabs", panels)
	}
}

// A tab whose program exits at once is no tab, and the tab before it runs
// on. OpenTab fails where the program ended before the listing that checks
// it, and a real core gives no bound on that: one that ends a moment later
// passes the check, and hc then removes its terminal. The failure itself is
// TestATabThatExitedAtOnceFailsTheOpenTab's, on a core that ends it first.
func TestATabThatExitsAtOnceLeavesTheInstance(t *testing.T) {
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"sh", "-c", "exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	own := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	_, err = h.OpenTab(c, ref, revier.Realization{Launch: []string{"true"}}, nil)
	if want := panelstart.ExitedAtOnce(revier.PanelSpec{Command: []string{"true"}}).Error(); err != nil && err.Error() != want {
		t.Fatalf("OpenTab = %v, want it passed or %q", err, want)
	}
	waitFor(t, h, func(in []revier.Instance) bool {
		return len(in) == 1 && in[0].Ref == ref && len(in[0].Panels) == 1 && in[0].Panels[0].ID == own
	})
}

// A view's window runs hc's attach of one terminal, which ends when the
// terminal is closed: the window ends with its panel, so it needs no hold
// (decisions.md D180). script gives the attach the terminal it needs.
func TestAnAttachedPanelEndsWithItsTerminal(t *testing.T) {
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is not installed; the attach needs a terminal")
	}
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"sh", "-c", "exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	argv, err := h.AttachPanel(panel)
	if err != nil {
		t.Fatalf("AttachPanel: %v", err)
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + a + "'"
	}
	attach := exec.Command(script, "-qec", strings.Join(quoted, " "), "/dev/null")
	if err := attach.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- attach.Wait() }()
	t.Cleanup(func() { _ = attach.Process.Kill() })

	select {
	case err := <-done:
		t.Fatalf("the attach ended before its terminal: %v", err)
	case <-time.After(time.Second):
	}
	if err := h.ClosePanel(c, ref, panel); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the attach still runs after its terminal closed")
	}
	if got, err := h.Instances(c); err != nil || slices.ContainsFunc(got, func(in revier.Instance) bool { return in.Ref == ref }) {
		t.Errorf("after the close = %+v, %v; want no instance", got, err)
	}
}

// A view's window that closes or crashes ends its attach as kitty does, by a
// hang-up or a kill of the attach's process group, and its terminal runs on:
// closing kitty ends no agent (decisions.md D181).
func TestAnAttachThatIsHungUpOrKilledLeavesItsTerminal(t *testing.T) {
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is not installed; the attach needs a terminal")
	}
	h, c := world(t), ctx(t)
	ref, err := h.Open(c, revier.Realization{Name: "home", Launch: []string{"sh", "-c", "exec sleep 30"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	panel := waitFor(t, h, func(in []revier.Instance) bool { return len(in) == 1 })[0].Panels[0].ID
	argv, err := h.AttachPanel(panel)
	if err != nil {
		t.Fatalf("AttachPanel: %v", err)
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = "'" + a + "'"
	}
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGKILL} {
		attach := exec.Command(script, "-qec", strings.Join(quoted, " "), "/dev/null")
		attach.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := attach.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- attach.Wait() }()
		select {
		case err := <-done:
			t.Fatalf("the attach ended before its %v: %v", sig, err)
		case <-time.After(time.Second):
		}
		if err := syscall.Kill(-attach.Process.Pid, sig); err != nil {
			t.Fatalf("%v the attach: %v", sig, err)
		}
		<-done
		time.Sleep(500 * time.Millisecond)
		got, err := h.Instances(c)
		if err != nil || len(got) != 1 || got[0].Ref != ref || len(got[0].Panels) != 1 || got[0].Panels[0].ID != panel {
			t.Fatalf("after %v of the attach = %+v, %v; want the instance with its terminal", sig, got, err)
		}
	}
}
