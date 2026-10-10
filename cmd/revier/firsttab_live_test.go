//go:build live

// Layer L4 for a first tab against a real tmux server (decisions.md D124). On
// tmux a tab is a window of the workspace's session, and the order of the
// windows is the order revier opened them in.
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const firstTabTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "lead"
  match = { title = "^lead$" }
  [[target.runtime.panels]]
  kind = "agent"
  command = ["sh", "-c", "sleep 300"]
  [[target.runtime.panels]]
  kind = "shell"
  command = ["sh", "-c", "sleep 301"]

[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
  first = true
  launch = ["sh", "-c", "sleep 302"]
`

// lead opens the project whose workspace has tickets as its first tab.
func lead(t *testing.T) {
	t.Helper()
	workdir := scratch(t)
	body := strings.ReplaceAll(firstTabTOML, "%PATH%", workdir)
	if err := os.WriteFile(filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects", "lead.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, "open", "lead")
}

// leadPanes is every pane of the workspace in the order of its windows, as
// the command it runs, whether its window is the current one, and whether it
// is the current pane of its window.
func leadPanes(t *testing.T) []string {
	t.Helper()
	out := tmuxRun(t, "list-panes", "-s", "-t", "lead", "-F", "#{pane_start_command} window=#{window_active} pane=#{pane_active}")
	return strings.Split(strings.ReplaceAll(strings.TrimSpace(out), `"`, ""), "\n")
}

var (
	leadAtHome = []string{
		"sh -c sleep 302 window=0 pane=1",
		"sh -c sleep 300 window=1 pane=1",
		"sh -c sleep 301 window=1 pane=0",
	}
	leadAtTickets = []string{
		"sh -c sleep 302 window=1 pane=1",
		"sh -c sleep 300 window=0 pane=1",
		"sh -c sleep 301 window=0 pane=0",
	}
)

// The tab is the first window, the workspace's panels are the second, and the
// agent is the current pane of the current window.
func TestAWorkspaceOpensWithItsFirstTabOnTmux(t *testing.T) {
	lead(t)
	if got := leadPanes(t); !slices.Equal(got, leadAtHome) {
		t.Errorf("panes = %q, want %q", got, leadAtHome)
	}
}

// The tab's key finds the tab the workspace opened with, and a second press
// goes back to the agent.
func TestTheKeyOfAFirstTabFindsItAndReturnsOnTmux(t *testing.T) {
	lead(t)
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtTickets) {
		t.Errorf("panes after the key = %q, want %q", got, leadAtTickets)
	}
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtHome) {
		t.Errorf("panes after the second press = %q, want %q", got, leadAtHome)
	}
}

// A restore opens the workspace as a press does: the tab first, the agent
// current. A shutdown closes it with the tab in it.
func TestAFirstTabComesBackWithItsWorkspaceOnTmux(t *testing.T) {
	lead(t)
	capture(t, "session", "save")
	reboot(t)
	capture(t, "session", "restore")
	if got := leadPanes(t); !slices.Equal(got, leadAtHome) {
		t.Errorf("panes after the restore = %q, want %q", got, leadAtHome)
	}

	capture(t, "shutdown", "--force")
	if got := workspaces(t); got != "" {
		t.Errorf("workspaces after the shutdown = %q, want none", got)
	}
}
