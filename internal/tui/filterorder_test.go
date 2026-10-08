package tui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// named is a world of projects with the names given, in that file order, none
// of them open.
func named(t *testing.T, names ...string) (*hosttest.FakeRuntime, *core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Window: hosttest.New("wm")}
	var raw []revier.Project
	for _, name := range names {
		raw = append(raw, revier.Project{Name: revier.ProjectName(name), Path: "/p/" + name, Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}},
		}})
	}
	return rt, c, core.Prepare(raw)
}

// namedOrder is the projects on the list, top row first.
func namedOrder(m tui.Model, names ...string) []string {
	var out []string
	for _, row := range rows(m) {
		// A row's second line is its path, which ends in the name too.
		if strings.Contains(row, "/p/") {
			continue
		}
		for _, name := range names {
			if strings.Contains(row, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// A query is a view over the list, so its rows keep the list's order: an open
// project that matches stands above a stopped one, whatever the fuzzy scorer
// makes of the two names. The scorer alone puts claude-code-podman first for
// "cap", because a letter after a dash outscores a letter beside the last.
func TestAFilteredListKeepsOpenProjectsAboveStoppedOnes(t *testing.T) {
	names := []string{"claude-code-podman", "cap-data-intelligence", "claude-plugins-official"}
	rt, c, projects := named(t, names...)
	rt.Add("session:cap-data-intelligence", "kitty", revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "sh"})
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "cap")

	want := []string{"cap-data-intelligence", "claude-code-podman", "claude-plugins-official"}
	if got := namedOrder(m, names...); !slices.Equal(got, want) {
		t.Errorf("rows for 'cap' = %v, want %v", got, want)
	}
}

// Among projects in one state, the name that holds the query whole stands
// above one that only holds its letters apart.
func TestAFilteredListPutsAWholeMatchAboveAScatteredOne(t *testing.T) {
	names := []string{"claude-code-podman", "escape-data"}
	_, c, projects := named(t, names...)
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "cap")

	want := []string{"escape-data", "claude-code-podman"}
	if got := namedOrder(m, names...); !slices.Equal(got, want) {
		t.Errorf("rows for 'cap' = %v, want %v", got, want)
	}
}

// A project that needs the user stands above one that is only open, under a
// query as without one, though the open one holds the query whole.
func TestAFilteredListKeepsAProjectThatNeedsYouFirst(t *testing.T) {
	names := []string{"cap-data-intelligence", "decay-maps"}
	rt, c, projects := named(t, names...)
	c.Probes = []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention},
	}}
	rt.Add("session:cap-data-intelligence", "kitty", revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "sh"})
	rt.Add("session:decay-maps", "kitty", revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude"})
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "cap")

	want := []string{"decay-maps", "cap-data-intelligence"}
	if got := namedOrder(m, names...); !slices.Equal(got, want) {
		t.Errorf("rows for 'cap' = %v, want %v", got, want)
	}
}

// The agent list keeps its states in order under a query too: a working agent
// the query matches apart stands above an idle one that holds it whole.
func TestAFilteredAgentListKeepsItsStatesInOrder(t *testing.T) {
	m, _, _ := listedWorld(t, 140, 30,
		listed{project: "beta", status: revier.StatusIdle, on: "cap sizes"},
		listed{project: "beta", status: revier.StatusRunning, on: "decay maps"})
	m = typeInto(m, "cap")

	rows := listedRows(m)
	if len(rows) != 2 || !strings.Contains(rows[0], "decay maps") || !strings.Contains(rows[1], "cap sizes") {
		t.Errorf("rows for 'cap' = %q, want the working agent above the idle one", rows)
	}
}
