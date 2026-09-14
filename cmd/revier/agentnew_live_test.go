//go:build live

// Layer L4 for `revier agent new` against a real tmux server. An agent tab is
// a tab (decisions.md D65), and on tmux a tab is a window of the workspace's
// session.
package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The agent opens in a new window of the workspace, on the conversation and in
// the directory the command names, with the declared shell split beside it.
func TestAgentNewOpensATabInTheWorkspace(t *testing.T) {
	args := work(t)
	agent := strings.TrimSuffix(args, ".args")
	_ = os.Remove(agent + ".started")
	dir := t.TempDir()

	if err := run([]string{"agent", "new", "-p", "work", "--resume", "abc-123", "--dir", dir}); err != nil {
		t.Fatalf("agent new: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var started string
	for started == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		read, _ := os.ReadFile(agent + ".started")
		started = strings.TrimSpace(string(read))
	}
	if want := dir + " --resume abc-123"; started != want {
		t.Errorf("the agent started as %q, want %q", started, want)
	}
	windows := strings.Split(strings.TrimSpace(tmuxRun(t, "list-windows", "-t", "work", "-F", "#{window_panes} #{window_active}")), "\n")
	if len(windows) != 2 || windows[1] != "2 1" {
		t.Errorf("windows (panes, active) = %q, want the layout and a current second window of agent and shell", windows)
	}
}

// A panel no workspace holds is named, whatever the runtime.
func TestAgentNewNamesAPanelNoWorkspaceHolds(t *testing.T) {
	work(t)
	err := run([]string{"agent", "new", "--panel", "%999"})
	if err == nil || !strings.Contains(err.Error(), "%999") {
		t.Errorf("err = %v, want the unknown panel named", err)
	}
}

// A project's agent new needs its workspace open.
func TestAgentNewNeedsTheWorkspaceOpen(t *testing.T) {
	work(t)
	reboot(t)
	err := run([]string{"agent", "new", "-p", "work"})
	if err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("err = %v, want the workspace named as not open", err)
	}
}
