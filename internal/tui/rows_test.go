package tui_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// Running projects sort above stopped ones, under the ones that need the
// human, and each group keeps config order.
func TestRunningProjectsSortAboveStoppedOnes(t *testing.T) {
	rt, _, c, projects := world(t, 4) // project-03 needs the human
	rt.Add("session:project-01", "kitty")
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	r := rows(m)
	for i, want := range []string{"project-03", "project-01", "project-00", "project-02"} {
		if line := r[2*i]; !strings.Contains(line, want) {
			t.Errorf("row %d = %q, want %s", i, line, want)
		}
	}
}

// Clearing the query, by Esc or by deleting its last letter, puts the cursor
// back on the project it was on when the query began (decisions.md D44).
func TestClearingTheFilterRestoresTheCursor(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	before := selectedRow(t, m) // project-01: project-11 leads, then config order

	for _, r := range "project-07" {
		m, _ = press(m, string(r))
	}
	if row := selectedRow(t, m); !strings.Contains(row, "project-07") {
		t.Fatalf("selected %q while typing, want the best match", row)
	}
	m, _ = press(m, "esc")
	if rule := ruleLine(m); !strings.Contains(rule, " 12/12 ") {
		t.Fatalf("rule = %q, want the filter cleared", rule)
	}
	if after := selectedRow(t, m); after != before {
		t.Errorf("selected %q after esc, want %q, where the cursor was before the query", after, before)
	}

	m, _ = press(m, "0")
	m, _ = press(m, "backspace")
	if after := selectedRow(t, m); after != before {
		t.Errorf("selected %q after deleting the query, want %q", after, before)
	}
}

// Enter ends the search it acted on: the surface opens again with an empty
// query, and the cursor stays on the project the search found rather than
// going back as Esc puts it.
func TestEnterClearsTheQueryAndKeepsTheSelection(t *testing.T) {
	_, _, c, projects := world(t, 12)
	for i := range projects {
		projects[i].Path = t.TempDir() // Enter opens only a directory that is there
	}
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	for _, r := range "project-07" {
		m, _ = press(m, string(r))
	}
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a project returned no command")
	}
	if rule := ruleLine(m); !strings.Contains(rule, " 12/12 ") {
		t.Errorf("rule = %q, want the query cleared", rule)
	}
	if q := query(m); strings.Contains(q, "project-07") {
		t.Errorf("query = %q, want the field empty", q)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "project-07") {
		t.Errorf("selected %q after enter, want project-07, the project the search found", row)
	}
}

// Enter in the pane ends the target search too, and the cursor stays on the
// target it ran, with a query typed or without one.
func TestEnterInThePaneKeepsTheCursorOnTheTargetItRan(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	m, _ = press(m, "tab")
	m, _ = press(m, "down") // editor, the second target
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q after enter, want the editor it ran", row)
	}

	m, _ = press(m, "e")
	m, _ = press(m, "d")
	m, cmd = press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a matched target returned no command")
	}
	if body := pane(m); !strings.Contains(body, "home") || !strings.Contains(body, "filter targets") {
		t.Errorf("enter did not end the target query:\n%s", body)
	}
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q after enter, want the editor the query found", row)
	}
}

// A double click opens a row as Enter does, so it ends the search as Enter
// does.
func TestADoubleClickClearsTheQuery(t *testing.T) {
	_, _, c, projects := world(t, 3)
	for i := range projects {
		projects[i].Path = t.TempDir() // Enter opens only a directory that is there
	}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	m, _ = press(m, "0")
	m, _ = press(m, "0")
	m = clickAt(m, 5, 5) // project-00, the only match
	next, cmd := m.Update(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(tui.Model)
	if cmd == nil {
		t.Fatal("a double click on a row returned no command")
	}
	if rule := ruleLine(m); !strings.Contains(rule, " 3/3 ") {
		t.Errorf("rule = %q, want the query cleared", rule)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "project-00") {
		t.Errorf("selected %q after the double click, want project-00", row)
	}
}

// A target key opens what it names, so it ends the search as Enter does.
func TestATargetKeyClearsTheQuery(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "00")
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlO}) // ctrl+shift+o: editor
	if cmd == nil {
		t.Fatal("ctrl+shift+o ran nothing")
	}
	assertSearchEnded(t, m, "00", " 3/3 ", "project-00")
}

// A click on a pane target runs it, so it ends the search, and the pane
// cursor stays on the row clicked.
func TestAClickOnAPaneTargetClearsTheQuery(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := typeInto(resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20), "00")
	x, y := paneCell(t, m, "editor")
	m, cmd := clickCell(m, x, y)
	if cmd == nil {
		t.Fatalf("the click ran nothing:\n%s", m.View())
	}
	assertSearchEnded(t, m, "00", " 3/3 ", "project-00")
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q, want it on the row clicked", row)
	}
}

// An action runs a command for the project, so it ends the search too.
func TestAnActionClearsTheQuery(t *testing.T) {
	_, _, c, projects := world(t, 3)
	actions := []config.Action{{Key: "ctrl-y", Name: "sync", Run: []string{"true"}}}
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), actions), "00")
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if cmd == nil {
		t.Fatal("ctrl+y ran no action")
	}
	assertSearchEnded(t, m, "00", " 3/3 ", "project-00")
}

// Enter that has nothing to run leaves the search as it is: a project with
// no home target moves the cursor to its targets, and a project whose
// directory is missing with nothing to clone from is refused.
func TestEnterThatRunsNothingKeepsTheQuery(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	homeless := core.Prepare([]revier.Project{{Name: "homeless", Path: "/p/homeless", Targets: []revier.Target{
		{Name: "editor", Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
	}}})
	m := typeInto(resize(refreshed(t, c, homeless, stateWith(t, nil), nil), 120, 20), "home")
	m, cmd := press(m, "enter")
	if cmd != nil {
		t.Fatal("enter on a project without home ran something")
	}
	if q := query(m); !strings.Contains(q, "home") {
		t.Errorf("query = %q after enter on a project without home, want it kept", q)
	}

	_, _, c, projects := world(t, 3) // /p/project-NN is not on this machine
	m = typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "00")
	m, cmd = press(m, "enter")
	if cmd != nil {
		t.Fatal("enter on a missing checkout with nothing to clone from ran something")
	}
	if f := footer(m); !strings.Contains(f, "does not exist") {
		t.Errorf("footer = %q, want the refusal said", f)
	}
	if q := query(m); !strings.Contains(q, "00") {
		t.Errorf("query = %q after a refused enter, want it kept", q)
	}
}

// A launch that fails after the search ended says why in the footer, where
// the user is left looking: no window came up to take the focus away.
func TestAFailedLaunchSaysSoInTheFooter(t *testing.T) {
	rt, _, c, projects := world(t, 3)
	for i := range projects {
		projects[i].Path = t.TempDir() // Enter opens only a directory that is there
	}
	rt.OpenErr = errors.New("kitty is not running")
	m := typeInto(refreshed(t, c, projects, stateWith(t, nil), nil), "00")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a project returned no command")
	}
	next, _ := m.Update(cmd())
	m = next.(tui.Model)
	if f := footer(m); !strings.Contains(f, "kitty is not running") {
		t.Errorf("footer = %q, want the launch failure said", f)
	}
	assertSearchEnded(t, m, "00", " 3/3 ", "project-00")
}

// assertSearchEnded checks that the typed query is gone, every project is listed
// again, and the cursor stayed on the project the search found.
func assertSearchEnded(t *testing.T, m tui.Model, typed, count, selected string) {
	t.Helper()
	if rule := ruleLine(m); !strings.Contains(rule, count) {
		t.Errorf("rule = %q, want the query cleared", rule)
	}
	// The pane may sit on the same line, right of its border.
	if q, _, _ := strings.Cut(query(m), "│"); strings.Contains(q, typed) {
		t.Errorf("query = %q, want the field empty", q)
	}
	if row := selectedRow(t, m); !strings.Contains(row, selected) {
		t.Errorf("selected %q, want %s, the project the search found", row, selected)
	}
}

// A project whose directory is not here shows the missing-folder glyph, and
// its row says nothing else about it: the table's right column is agent state
// only, and whether it can be cloned is the pane's.
func TestAMissingCheckoutShowsTheMissingFolder(t *testing.T) {
	gone := map[string]string{"cloneable": "/nowhere/a", "stuck": "/nowhere/b"}
	projects, _ := onDisk(t, []string{"cloneable", "present", "stuck"}, gone,
		map[string]string{"cloneable": "/srv/git/a.git"})
	m := resize(refreshed(t, &core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, stateWith(t, nil), nil), 80, 20)
	g := theme.Default().Glyphs

	r := rows(m)
	for i, missing := range []bool{true, false, true} {
		if got := strings.Contains(r[2*i], g.NoFolder); got != missing {
			t.Errorf("row %d = %q: missing-folder glyph = %v, want %v", i, r[2*i], got, missing)
		}
		if path := r[2*i+1]; strings.Contains(path, "not cloned") || strings.Contains(path, "not on this machine") {
			t.Errorf("row %d = %q, want no note", i, path)
		}
	}
}

// On a narrow row the path of a missing checkout is whole: the project column
// gives way last.
func TestAMissingCheckoutKeepsItsPathOnANarrowRow(t *testing.T) {
	long := "/nowhere/" + strings.Repeat("deeply-nested/", 2) + "cloneable"
	projects, _ := onDisk(t, []string{"cloneable"}, map[string]string{"cloneable": long},
		map[string]string{"cloneable": "/srv/git/a.git"})
	// Room for the path, its indent and a gap.
	m := resize(refreshed(t, &core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, stateWith(t, nil), nil), 58, 20)

	path := rows(m)[1]
	if !strings.Contains(path, long) || strings.Contains(path, "not") {
		t.Errorf("path line = %q, want the path whole and no note", path)
	}
}

// A narrow row keeps the count of its agents, and never the activity: the
// state is what the row is there to show, and the activity is the pane's.
func TestANarrowRowKeepsTheCountAndNoActivity(t *testing.T) {
	c, projects := longWorld(t, "/p/long")
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 60, 20)

	first := rows(m)[0]
	if !strings.Contains(first, theme.Default().Glyphs.Working+" 1") || strings.Contains(first, "Reading") {
		t.Errorf("row = %q, want the count kept and no activity", first)
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
	}
}

// A long path is cut in the middle, so both the root it lives under and the
// checkout it is stay on the row.
func TestALongPathKeepsItsStartAndItsEnd(t *testing.T) {
	path := "/p/" + strings.Repeat("deeply-nested/", 10) + "checkout"
	c, projects := longWorld(t, path)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20)

	second := rows(m)[1]
	if !strings.Contains(second, "/p/deeply") || !strings.Contains(second, "…") || !strings.Contains(second, "/checkout") {
		t.Errorf("path line = %q, want its start, an ellipsis, and its end", second)
	}
}

// Each column means one thing: the mark says the project is open, and the
// agents are counted by state on the right. The pane says the state in words.
func TestTheMarkSaysOpenAndTheAgentSaysItNeedsYou(t *testing.T) {
	_, _, c, projects := world(t, 2) // project-01 is open and its agent needs you
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)
	g := theme.Default().Glyphs

	first, _, _ := strings.Cut(rows(m)[0], "│")
	// The folder column, when the set has one, sits between the mark and the
	// name.
	left, _, found := strings.Cut(strings.TrimLeft(first, " "+g.Cursor), "project-01")
	if !found || !strings.HasPrefix(left, g.Running+" ") {
		t.Errorf("row = %q, want the open mark in front of the name", first)
	}
	if !strings.Contains(first, g.NeedsYou+" 1") || strings.Contains(first, "needs you") {
		t.Errorf("row = %q, want the agent counted by its glyph, without words", first)
	}
	if body := pane(m); !strings.Contains(body, g.NeedsYou+" needs you") {
		t.Errorf("pane = %q, want the agent's glyph and words", body)
	}
}

// The pane appears only once the list keeps room for its rows beside it: at
// ninety-six columns the list takes the whole width.
func TestThePaneWaitsUntilTheListHasItsRoom(t *testing.T) {
	_, _, c, projects := world(t, 3)
	for _, tc := range []struct {
		width int
		pane  bool
	}{{96, false}, {120, true}} {
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), tc.width, 20)
		split := false
		for _, line := range lines(m) {
			if strings.Contains(line, "│") {
				split = true
			}
		}
		if split != tc.pane {
			t.Errorf("at %d columns pane = %v, want %v:\n%s", tc.width, split, tc.pane, m.View())
		}
	}
}

// On a wide terminal the frame takes the width, and the rows are a grid: the
// counts line up in one column.
func TestAWideTerminalFillsTheWidthWithAGrid(t *testing.T) {
	rt, _, c, projects := world(t, 3) // project-02 needs you
	rt.Add("session:project-00", "kitty", revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude"})
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 380, 20)
	g := theme.Default().Glyphs

	widest := 0
	for _, line := range strings.Split(m.View(), "\n") {
		w := lipgloss.Width(strings.TrimRight(line, " "))
		if w > 380 {
			t.Errorf("line is %d columns wide: %q", w, line)
		}
		if w > widest {
			widest = w
		}
	}
	if widest < 370 {
		t.Errorf("the surface is %d wide, want the whole terminal but the margin:\n%s", widest, m.View())
	}
	r := rows(m)
	first, second := r[0], r[2]
	// Cells, not bytes: the bar and the glyphs are several bytes each.
	col := func(row, s string) int {
		i := strings.Index(row, s)
		if i < 0 {
			return -1
		}
		return lipgloss.Width(row[:i])
	}
	at := func(row string) int { return col(row, g.NeedsYou+" 1") }
	if at(first) < 0 || at(first) != at(second) {
		t.Errorf("counts at %d and %d, want them in one column:\n%s\n%s", at(first), at(second), first, second)
	}
}

// The list stops at its table's width, and the pane takes the rest: at
// three hundred and eighty columns the list is eighty.
func TestTheListStopsAtItsTableWidth(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 380, 40)
	_, mc := margins(m)
	// The border sits after the margin and the list.
	if border := paneBorder(t, m); border != mc+80 {
		t.Errorf("pane border at column %d, want the list capped at 80 columns", border)
	}
}

// The agent column gives way in steps as the list narrows, and the project
// column only after it: the count goes first, then the glyph, and only then
// is the name cut.
func TestTheAgentColumnGivesWayBeforeTheProjectColumn(t *testing.T) {
	g := theme.Default().Glyphs
	for _, tc := range []struct {
		width               int
		count, glyph, whole bool
	}{
		{41, true, true, true},    // room for everything
		{40, false, true, true},   // the count goes
		{38, false, false, true},  // the glyph goes, the name is whole
		{35, false, false, false}, // only now is the name cut
	} {
		_, _, c, projects := longNamedWorld(t)
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), tc.width, 20)
		first := rows(m)[0]
		if got := strings.Contains(first, g.NeedsYou+" 1"); got != tc.count {
			t.Errorf("%d columns: count shown = %v, want %v: %q", tc.width, got, tc.count, first)
		}
		if got := strings.Contains(first, g.NeedsYou); got != tc.glyph {
			t.Errorf("%d columns: glyph shown = %v, want %v: %q", tc.width, got, tc.glyph, first)
		}
		if got := strings.Contains(first, longName); got != tc.whole {
			t.Errorf("%d columns: whole name shown = %v, want %v: %q", tc.width, got, tc.whole, first)
		}
	}
}

const longName = "a-project-name-of-thirty-chars"

// longNamedWorld is one running project with a thirty-character name whose
// agent needs the human.
func longNamedWorld(t *testing.T) (*hosttest.FakeRuntime, *hosttest.Fake, *core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention, Activity: "needs a decision"},
	}}}
	projects := core.Prepare([]revier.Project{{Name: longName, Path: "/p/x", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:x", Launch: []string{"x"}, Match: revier.Match{Title: "^session:x$"}}},
	}}})
	rt.Add("session:x", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	return rt, nil, c, projects
}

// The snapshot takes the rows the pane has left, and no more: on a tall
// terminal it lists past the twenty rows it once stopped at, and on a short
// one it ends in an ellipsis inside the pane.
func TestTheSnapshotFillsThePaneHeight(t *testing.T) {
	dir := t.TempDir()
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, _, c, projects := world(t, 1)
	projects[0].Path = dir

	tall := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 80)
	if body := pane(tall); !strings.Contains(body, "f39") || strings.Contains(body, "...") {
		t.Errorf("tall pane stops short of its rows:\n%s", body)
	}
	// Nineteen rows of pane, from the query line down; the facts and the
	// target query take ten, the heading two.
	short := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 24)
	body := pane(short)
	if !strings.Contains(body, "f00") || strings.Contains(body, "f39") || !strings.HasSuffix(strings.TrimSpace(body), "...") {
		t.Errorf("short pane does not end in an ellipsis inside its rows:\n%s", body)
	}
	if n := strings.Count(strings.TrimSpace(body), "\n") + 1; n > 19 {
		t.Errorf("short pane is %d rows, want at most 19", n)
	}
}

// A wide pane puts the snapshot beside the facts, level with the name, so
// both fill the height.
func TestAWidePaneLaysTheSnapshotBesideTheFacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, projects := longWorld(t, dir)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 260, 30)

	top := strings.Split(pane(m), "\n")[0]
	if !strings.HasPrefix(top, "long") || !strings.Contains(top, "Project Snapshot") {
		t.Errorf("pane top = %q, want the name and the snapshot's heading on one line", top)
	}
}

// A click on a row selects it, with or without the margin around the frame.
// A click on the pane, or above the rows, moves nothing.
func TestAClickSelectsTheRowUnderIt(t *testing.T) {
	for _, tc := range []struct {
		w, h, top int // top is the terminal row the first project row is on
	}{{120, 20, 5}, {140, 30, 6}} {
		_, _, c, projects := world(t, 4) // project-03, then 00, 01, 02
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), tc.w, tc.h)
		_, mc := margins(m)
		x := mc + 5

		m = clickAt(m, x, tc.top+2*2)
		if row := selectedRow(t, m); !strings.Contains(row, "project-01") {
			t.Errorf("%dx%d: selected %q after a click on the third row, want project-01", tc.w, tc.h, row)
		}
		m = clickAt(m, paneBorder(t, m)+3, tc.top)
		m = clickAt(m, x, tc.top-3)
		if row := selectedRow(t, m); !strings.Contains(row, "project-01") {
			t.Errorf("%dx%d: selected %q after clicks on the pane and the header, want project-01 still", tc.w, tc.h, row)
		}
	}
}

// Two clicks on one row open it, as Enter does; two clicks on different rows
// are two choices.
func TestADoubleClickOpensTheRow(t *testing.T) {
	rt, _, c, projects := world(t, 3) // project-02, then 00, 01
	for i := range projects {
		projects[i].Path = t.TempDir() // Enter opens only a directory that is there
	}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m = clickAt(m, 5, 5) // project-02
	m = clickAt(m, 5, 7) // project-00: a second choice, not a double click
	next, cmd := m.Update(tea.MouseMsg{X: 5, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(tui.Model)
	if cmd == nil {
		t.Fatal("a double click on a row returned no command")
	}
	cmd()
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "session:project-00" {
		t.Fatalf("runtime Opened = %v, want project-00's home", rt.Opened)
	}
	if !strings.Contains(ruleLine(m), " 3/3 ") {
		t.Errorf("a double click must not leave the list:\n%s", m.View())
	}
}

// clickAt is one press of the left button on a terminal cell.
func clickAt(m tui.Model, x, y int) tui.Model {
	m, _ = clickCell(m, x, y)
	return m
}

// margins reads the frame's margin off the rendered surface: the rows above
// its top border and the columns left of it.
func margins(m tui.Model) (rows, cols int) {
	for i, line := range strings.Split(m.View(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Every line of the surface starts with a gutter space of its own,
		// which is not margin.
		return i, len(line) - len(strings.TrimLeft(line, " ")) - 1
	}
	return 0, 0
}

// A target with nothing up says "stopped", where it said "-".
func TestAStoppedTargetSaysStopped(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)

	if body := pane(m); !strings.Contains(body, "stopped") {
		t.Errorf("pane = %q, want the editor target stopped", body)
	}
}

// A running row does not name its open targets, whatever is open: the
// table's right column is agent state only, and the targets are the pane's.
func TestARunningRowDoesNotNameItsOpenTargets(t *testing.T) {
	rt, wm := hosttest.NewRuntime("rt"), hosttest.New("wm")
	projects := core.Prepare([]revier.Project{{Name: "alpha", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:alpha", Launch: []string{"x"}, Match: revier.Match{Title: "^session:alpha$"}}},
		{Name: "editor", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code-alpha$"}}},
	}}})
	rt.Add("session:alpha", "kitty")
	wm.Add("editor", "code-alpha")
	m := resize(refreshed(t, &core.Core{Runtime: rt, Window: wm}, projects, stateWith(t, nil), nil), 96, 20)
	if path := rows(m)[1]; strings.Contains(path, "home") || strings.Contains(path, "editor") {
		t.Errorf("path line = %q, want no targets named", path)
	}
	body := pane(resize(m, 140, 30))
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "editor") && strings.Contains(line, "running") {
			return
		}
	}
	t.Errorf("pane = %q, want the editor running, so the row had a target to leave out", body)
}
