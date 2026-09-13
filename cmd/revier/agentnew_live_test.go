//go:build live

// Layer L4 for `revier agent new` against a real tmux server. An agent tab is
// a tab (decisions.md D65), and a tmux window is one instance with no tabs of
// its own, so tmux refuses it; the kitty host is where the tab opens, checked
// at L3 and by hand.
package main

import (
	"strings"
	"testing"
)

// On tmux the command is refused, naming the runtime, and nothing is split
// into the workspace or opened beside it.
func TestAgentNewIsRefusedOnARuntimeWithoutTabs(t *testing.T) {
	work(t)
	before := tmuxRun(t, "list-panes", "-a", "-F", "#{pane_id}")

	err := run([]string{"agent", "new", "-p", "work", "--resume", "abc-123", "--dir", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "tmux") || !strings.Contains(err.Error(), "no tabs") {
		t.Errorf("err = %v, want the tmux runtime named as having no tabs", err)
	}
	if after := tmuxRun(t, "list-panes", "-a", "-F", "#{pane_id}"); after != before {
		t.Errorf("panes changed from\n%s\nto\n%s", before, after)
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
