//go:build live

// Layer L4 for `revier agent new` against a real tmux server. An agent tab is
// a tab (decisions.md D65), and on tmux a tab is a window of the workspace's
// session.
package main

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The agent opens in a new window of the workspace, on the conversation and in
// the directory the command names, with the declared shell split beside it.
func TestAgentNewOpensATabInTheWorkspace(t *testing.T) {
	args := work(t)
	agent := strings.TrimSuffix(args, ".args")
	dir := t.TempDir()

	if err := run(io.Discard, []string{"agent", "new", "-p", "work", "--resume", "abc-123", "--dir", dir}); err != nil {
		t.Fatalf("agent new: %v", err)
	}
	// The declared agent writes its own line whenever it gets to run, before
	// or after this one, so the new agent is its line among the lines.
	want := dir + " --resume abc-123"
	deadline := time.Now().Add(5 * time.Second)
	var started []string
	for !slices.Contains(started, want) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		read, _ := os.ReadFile(agent + ".started")
		started = strings.Split(strings.TrimSpace(string(read)), "\n")
	}
	if !slices.Contains(started, want) {
		t.Errorf("agents started as %q, want one started as %q", started, want)
	}
	windows := strings.Split(strings.TrimSpace(tmuxRun(t, "list-windows", "-t", "work", "-F", "#{window_panes} #{window_active}")), "\n")
	if len(windows) != 2 || windows[1] != "2 1" {
		t.Errorf("windows (panes, active) = %q, want the layout and a current second window of agent and shell", windows)
	}
}

// A panel no workspace holds is named, whatever the runtime.
func TestAgentNewNamesAPanelNoWorkspaceHolds(t *testing.T) {
	work(t)
	err := run(io.Discard, []string{"agent", "new", "--panel", "%999"})
	if err == nil || !strings.Contains(err.Error(), "%999") {
		t.Errorf("err = %v, want the unknown panel named", err)
	}
}

// A project's agent new needs its workspace open.
func TestAgentNewNeedsTheWorkspaceOpen(t *testing.T) {
	work(t)
	reboot(t)
	err := run(io.Discard, []string{"agent", "new", "-p", "work"})
	if err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("err = %v, want the workspace named as not open", err)
	}
}
