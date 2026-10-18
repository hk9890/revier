package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// agentWorld is demo, open with two agents, and solo, closed with none. demo
// sorts first, so the cursor starts on it.
func agentWorld(t *testing.T) (*hosttest.FakeRuntime, tui.Model) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude idle fix-remote-work"},
		revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "claude idle UX and naming"})
	var raw []revier.Project
	for _, name := range []revier.ProjectName{"demo", "solo"} {
		raw = append(raw, revier.Project{Name: name, Path: "/p/" + string(name), Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "session:" + string(name), Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + string(name) + "$"}}},
		}})
	}
	projects := core.Prepare(raw)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "claude"}}}
	return rt, resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 30).Said()
}

// Beside the list the pane starts level with the project query, and its
// border meets the top rule: the query and its rule stand in the list's
// column, so they read as the list's and not the whole screen's.
func TestThePaneStartsLevelWithTheProjectQuery(t *testing.T) {
	_, m := agentWorld(t)
	if top := lines(m)[1]; !strings.Contains(top, "┬") {
		t.Errorf("top rule = %q, want it to meet the pane's border", top)
	}
	q := query(m)
	left, right, ok := strings.Cut(q, "│")
	if !ok || !strings.Contains(left, "filter projects") || !strings.Contains(right, "demo") {
		t.Errorf("query line = %q, want the project query left of the border and the pane's first line right of it", q)
	}
	if rule := ruleLine(m); !strings.Contains(rule, "│") {
		t.Errorf("rule = %q, want it to stop at the pane's border", rule)
	}
}

// The pane's border is drawn in the rules' colour. A border takes its own
// colour in lipgloss, and without one it was the terminal's text colour,
// white beside the grey rules it meets.
func TestThePaneBorderIsTheRulesColour(t *testing.T) {
	inColour(t)

	_, m := agentWorld(t)
	view := strings.Split(drawn(m), "\n")
	colour := func(line, glyph string) string {
		before, _, ok := strings.Cut(line, glyph)
		if !ok {
			t.Fatalf("line %q has no %q", line, glyph)
		}
		return before[strings.LastIndex(before, "\x1b["):]
	}
	var rule, border string
	for _, line := range view {
		switch {
		case rule == "" && strings.Contains(line, "┬"):
			rule = colour(line, "─")
		case border == "" && strings.Contains(line, "│"):
			border = colour(line, "│")
		}
	}
	if rule == "" || rule != border {
		t.Errorf("rule drawn with %q, border with %q, want one colour", rule, border)
	}
}

// Tab walks the projects and the agents and comes back round; shift+tab walks
// the other way. Alt+t reaches the targets, and Tab from there goes back to
// the projects.
func TestTabWalksTheListAndTheAgentsAndAltTReachesTheTargets(t *testing.T) {
	_, m := agentWorld(t)
	for _, step := range []struct{ key, want string }{
		{"tab", "fix-remote-work"},
		{"tab", ""},
		{"shift+tab", "fix-remote-work"},
		{"shift+tab", ""},
		{"alt+t", "home"},
		{"tab", ""},
		{"tab", "fix-remote-work"},
		{"alt+t", "home"},
		{"shift+tab", ""},
	} {
		m, _ = press(m, step.key)
		row := paneCursor(m)
		if step.want == "" && row != "" || !strings.Contains(row, step.want) {
			t.Fatalf("after %s the pane cursor is %q, want %q:\n%s", step.key, row, step.want, drawn(m))
		}
	}
}

// A project with no agents has no Agents section, so Tab has nowhere to go;
// alt+t still reaches its targets.
func TestTabStaysOnTheListOfAProjectWithoutAgents(t *testing.T) {
	_, m := agentWorld(t)
	m, _ = press(m, "down") // solo
	m, _ = press(m, "tab")
	if row := paneCursor(m); row != "" {
		t.Fatalf("pane cursor = %q, want the cursor still on the list", row)
	}
	m, _ = press(m, "alt+t")
	if row := paneCursor(m); !strings.Contains(row, "home") {
		t.Errorf("pane cursor = %q, want the target", row)
	}
}

// Agents that exit take their section with them, and a cursor that was in it
// goes back to the list, where Tab would have taken it, rather than staying
// where nothing is drawn.
func TestTheCursorLeavesAnAgentsSectionThatIsGone(t *testing.T) {
	rt, m := agentWorld(t)
	m, _ = press(m, "tab")
	rt.Retitle("1", "zsh")
	rt.Retitle("2", "zsh")
	m = survey(m)
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q, want the list once the agents are gone:\n%s", row, drawn(m))
	}
}

// Typing in Agents filters the agents by what they are doing, and Enter
// brings the one under the cursor to the front.
func TestTypingInAgentsFiltersThemAndEnterGoesToTheAgent(t *testing.T) {
	rt, m := agentWorld(t)
	m, _ = press(m, "tab")
	m, _ = press(m, "n")
	m, _ = press(m, "a")
	if body := pane(m); strings.Contains(body, "fix-remote-work") || !strings.Contains(body, "UX and naming") {
		t.Fatalf("agent query 'na' should leave only UX and naming:\n%s", body)
	}
	if q := query(m); strings.Contains(q, "na") {
		t.Errorf("query line = %q, want the project query untouched", q)
	}
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatalf("enter on an agent ran nothing:\n%s", drawn(m))
	}
	cmd()
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != "2" {
		t.Errorf("panel focuses = %v, want the UX and naming agent", rt.PanelFocuses)
	}
}

// Esc clears the query of the section the cursor is in, and a second Esc
// takes the cursor back to the projects.
func TestEscClearsTheSectionsQueryThenGoesBack(t *testing.T) {
	_, m := agentWorld(t)
	m, _ = press(m, "tab")
	m, _ = press(m, "u")
	m, _ = press(m, "esc")
	if body := pane(m); !strings.Contains(body, "fix-remote-work") || !strings.Contains(body, "❯ filter agents") {
		t.Errorf("esc did not clear the agent query:\n%s", body)
	}
	if row := paneCursor(m); row == "" {
		t.Fatal("the first esc left the Agents section")
	}
	m, _ = press(m, "esc")
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q after the second esc, want the list", row)
	}
}

// One click on an agent row puts the cursor on it, and a second goes to the
// agent, as on a project row; a click on the agents' field puts the cursor
// there.
func TestADoubleClickOnAnAgentGoesToItAndOnItsFieldTypesThere(t *testing.T) {
	rt, m := agentWorld(t)
	x, y := paneCell(t, m, "UX and naming")
	m, cmd := clickCell(m, x, y)
	runAll(cmd)
	if len(rt.PanelFocuses) != 0 {
		t.Fatalf("panel focuses = %v after one click, want none", rt.PanelFocuses)
	}
	if row := paneCursor(m); !strings.Contains(row, "UX and naming") {
		t.Errorf("pane cursor = %q after one click, want the agent clicked", row)
	}
	m, cmd = clickCell(m, x, y)
	runAll(cmd)
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != "2" {
		t.Errorf("panel focuses = %v, want the agent clicked", rt.PanelFocuses)
	}

	m, _ = press(m, "shift+tab")
	x, y = paneCell(t, m, "filter agents")
	m, _ = clickCell(m, x, y)
	m, _ = press(m, "z")
	if body := pane(m); !strings.Contains(body, "❯ z") || strings.Contains(body, "fix-remote-work") {
		t.Errorf("typing after a click on the agents' field did not filter the agents:\n%s", body)
	}
}

// threeAgents is demo, open with three agents, on a clock the test
// advances, and the ref of its workspace, for a test that closes one of them.
func threeAgents(t *testing.T) (*hosttest.FakeRuntime, revier.TargetRef, tui.Model, *time.Time) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:demo", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude idle fix-remote-work"},
		revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "claude idle UX and naming"},
		revier.Panel{ID: "3", Kind: revier.PanelTool, Title: "claude idle release notes"})
	projects := core.Prepare([]revier.Project{{Name: "demo", Path: "/p/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:demo$"}}},
	}}})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "claude"}}}
	m, now := clocked(resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 30).Said())
	return rt, ref, m, now
}

// Two clicks on one cell of the Agents section are two choices when a refresh
// between them moved another agent into that row: it was clicked once.
func TestASecondClickOnAnAgentRowARefreshMovedGoesNowhere(t *testing.T) {
	rt, ref, m, now := threeAgents(t)

	x, y := paneCell(t, m, "UX and naming")
	m, cmd := clickCell(m, x, y)
	runAll(cmd)
	if err := rt.ClosePanel(t.Context(), ref, "1"); err != nil {
		t.Fatal(err)
	}
	m = survey(m)
	if rx, ry := paneCell(t, m, "release notes"); rx != x || ry != y {
		t.Fatalf("release notes is at %d,%d after the refresh, the click was at %d,%d: the test needs one cell", rx, ry, x, y)
	}
	*now = now.Add(100 * time.Millisecond)
	_, cmd = clickCell(m, x, y)
	runAll(cmd)
	if len(rt.PanelFocuses) != 0 {
		t.Errorf("panel focuses = %v, want none: one click on release notes", rt.PanelFocuses)
	}
}

// Clicks on two rows of the Agents section are two choices when a refresh
// between them moved the agent clicked first into the row clicked second.
func TestAClickOnTheAgentRowARefreshMovedTheLastAgentToGoesNowhere(t *testing.T) {
	rt, ref, m, now := threeAgents(t)

	x, y := paneCell(t, m, "release notes")
	ux, uy := paneCell(t, m, "UX and naming")
	m, cmd := clickCell(m, x, y)
	runAll(cmd)
	if err := rt.ClosePanel(t.Context(), ref, "1"); err != nil {
		t.Fatal(err)
	}
	m = survey(m)
	if rx, ry := paneCell(t, m, "release notes"); rx != ux || ry != uy {
		t.Fatalf("release notes is at %d,%d after the refresh, UX and naming was at %d,%d: the test needs it moved there", rx, ry, ux, uy)
	}
	*now = now.Add(100 * time.Millisecond)
	_, cmd = clickCell(m, ux, uy)
	runAll(cmd)
	if len(rt.PanelFocuses) != 0 {
		t.Errorf("panel focuses = %v, want none: one click on each of two rows", rt.PanelFocuses)
	}
}

// The agent query is about one project's rows: another project under the
// cursor starts with it empty.
func TestAnotherProjectStartsWithThePaneQueryEmpty(t *testing.T) {
	_, m := agentWorld(t)
	m, _ = press(m, "tab")
	m, _ = press(m, "z")
	m, _ = press(m, "shift+tab")
	m, _ = press(m, "down")
	m, _ = press(m, "up")
	if body := pane(m); !strings.Contains(body, "❯ filter agents") || !strings.Contains(body, "fix-remote-work") {
		t.Errorf("the agent query outlived its project:\n%s", body)
	}
}
