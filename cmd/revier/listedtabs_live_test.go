//go:build live

// Layer L4 for a target that lists its tabs against a real tmux server
// (decisions.md D126). On tmux a tab is a window of the workspace's session,
// and the order of the windows is the order revier opened them in.
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const listedTabsTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "lead"
  match = { title = "^lead$" }
  tabs = ["tickets", "agent"]
  active = "agent"

[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
  launch = ["sh", "-c", "sleep 302"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
  [[target.runtime.panels]]
  kind = "agent"
  command = ["sh", "-c", "sleep 300"]
  [[target.runtime.panels]]
  kind = "shell"
  command = ["sh", "-c", "sleep 301"]
`

// lead writes the project whose workspace lists tickets and agent as its
// tabs, with agent the active one.
func lead(t *testing.T) {
	t.Helper()
	workdir := scratch(t)
	body := strings.ReplaceAll(listedTabsTOML, "%PATH%", workdir)
	if err := os.WriteFile(filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects", "lead.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
	leadAtAgent = []string{
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

// Window 1 runs the launch of tickets, window 2 holds the two panes of
// agent, and the first pane of window 2 is the current one.
func TestAWorkspaceOpensWithTheTabsItListsOnTmux(t *testing.T) {
	lead(t)
	capture(t, "open", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtAgent) {
		t.Errorf("panes = %q, want %q", got, leadAtAgent)
	}
}

// The key of a listed tab finds the tab the workspace opened with, and a
// second press goes back to the agent.
func TestTheKeyOfAListedTabFindsItAndReturnsOnTmux(t *testing.T) {
	lead(t)
	capture(t, "open", "lead")
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtTickets) {
		t.Errorf("panes after the key = %q, want %q", got, leadAtTickets)
	}
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtAgent) {
		t.Errorf("panes after the second press = %q, want %q", got, leadAtAgent)
	}
}

// The key of a listed tab with the workspace closed opens the workspace and
// stays on that tab, with no second copy of it.
func TestTheKeyOfAListedTabOpensTheWorkspaceOnThatTabOnTmux(t *testing.T) {
	lead(t)
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtTickets) {
		t.Errorf("panes = %q, want %q", got, leadAtTickets)
	}
}

// A restore opens the workspace as a press does: tickets first, the agent
// current. A shutdown closes it with its tabs in it.
func TestListedTabsComeBackWithTheirWorkspaceOnTmux(t *testing.T) {
	lead(t)
	capture(t, "open", "lead")
	capture(t, "session", "save")
	reboot(t)
	capture(t, "session", "restore")
	if got := leadPanes(t); !slices.Equal(got, leadAtAgent) {
		t.Errorf("panes after the restore = %q, want %q", got, leadAtAgent)
	}

	capture(t, "shutdown", "--force")
	if got := workspaces(t); got != "" {
		t.Errorf("workspaces after the shutdown = %q, want none", got)
	}
}
