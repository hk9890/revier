//go:build live

// Layer L4 for a target that lists its tabs against a real hc core, shown in
// a fake view (decisions.md D126). On hc a tab is the terminals that share a
// tab's label, and the labels keep the order revier opened them in (D179).
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
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
	if err := os.WriteFile(filepath.Join(config.ProjectsDir(os.Getenv("REVIER_CONFIG_HOME")), "lead.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// leadPanes is every panel of the workspace in the order of its tabs, as the
// place of its tab and the target its tab is, marked * when the view shows
// it current.
func leadPanes(t *testing.T) []string {
	t.Helper()
	lead := instance(t, "lead")
	current, err := liveView.FocusedPanel(t.Context(), lead.Ref)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i, tab := range tabs(lead) {
		for _, p := range tab {
			line := strconv.Itoa(i) + " " + p.Vars[core.PanelTargetVar]
			if p.ID == current {
				line += " *"
			}
			out = append(out, line)
		}
	}
	return out
}

var (
	leadAtAgent   = []string{"0 tickets", "1 agent *", "1 agent"}
	leadAtTickets = []string{"0 tickets *", "1 agent", "1 agent"}
)

// The first tab runs the launch of tickets, the second holds the two panels
// of agent, and the first panel of the second is the current one.
func TestAWorkspaceOpensWithTheTabsItListsOnHc(t *testing.T) {
	lead(t)
	capture(t, "open", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtAgent) {
		t.Errorf("panes = %q, want %q", got, leadAtAgent)
	}
}

// The key of a listed tab finds the tab the workspace opened with, and a
// second press goes back to the agent.
func TestTheKeyOfAListedTabFindsItAndReturnsOnHc(t *testing.T) {
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
func TestTheKeyOfAListedTabOpensTheWorkspaceOnThatTabOnHc(t *testing.T) {
	lead(t)
	capture(t, "go", "tickets", "-p", "lead")
	if got := leadPanes(t); !slices.Equal(got, leadAtTickets) {
		t.Errorf("panes = %q, want %q", got, leadAtTickets)
	}
}

// `revier agent new` finds the agent panel and the shell panel in the tab the
// workspace lists (decisions.md D128): the new tab is a third tab that holds
// a copy of both, and the command prints the address of its agent.
func TestAgentNewOpensATabInAWorkspaceThatListsItsTabsOnHc(t *testing.T) {
	lead(t)
	capture(t, "open", "lead")

	address := strings.TrimSpace(capture(t, "agent", "new", "-p", "lead", "--no-focus"))
	if !regexp.MustCompile(`^lead:home:t\d+$`).MatchString(address) {
		t.Fatalf("printed %q, want lead:home:<address>", address)
	}
	got := tabs(instance(t, "lead"))
	if len(got) != 3 || len(got[2]) != 2 || string(got[2][0].ID) != strings.TrimPrefix(address, "lead:home:") {
		t.Errorf("tabs = %+v, want a third tab of two whose first panel is %s", got, address)
	}
}

// A restore opens the workspace as a press does: tickets first, the agent
// current. A shutdown closes it with its tabs in it.
func TestListedTabsComeBackWithTheirWorkspaceOnHc(t *testing.T) {
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
