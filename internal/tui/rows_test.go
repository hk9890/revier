package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// Clearing the filter leaves the cursor on the project it found.
func TestClearingTheFilterKeepsTheProjectFound(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	for _, r := range "project-07" {
		m, _ = press(m, string(r))
	}
	m, _ = press(m, "esc")

	if rule := lines(m)[2]; !strings.Contains(rule, " 12/12 ") {
		t.Fatalf("rule = %q, want the filter cleared", rule)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "project-07") {
		t.Errorf("selected %q after clearing the filter, want project-07", row)
	}
}

// A project whose directory is not here says so on its row, and says whether
// Enter can bring it back. One whose directory is here says nothing.
func TestAMissingCheckoutSaysWhetherEnterClonesIt(t *testing.T) {
	gone := map[string]string{"cloneable": "/nowhere/a", "stuck": "/nowhere/b"}
	projects, _ := onDisk(t, []string{"cloneable", "present", "stuck"}, gone,
		map[string]string{"cloneable": "/srv/git/a.git"})
	m := resize(refreshed(t, &core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, stateWith(t, nil), nil), 80, 20)

	r := rows(m)
	for i, want := range []string{"not cloned · enter clones", "", "not on this machine"} {
		path := r[2*i+1]
		if want == "" {
			if strings.Contains(path, "not cloned") || strings.Contains(path, "not on this machine") {
				t.Errorf("row %d = %q, want no tag for a checkout that is here", i, path)
			}
			continue
		}
		if !strings.Contains(path, want) {
			t.Errorf("row %d = %q, want %q", i, path, want)
		}
	}
}

// On a row too narrow for the whole path and the whole tag, a missing
// checkout keeps a short tag and the path is cut: nothing else on the row
// says the directory is not here.
func TestAMissingCheckoutKeepsItsTagOnANarrowRow(t *testing.T) {
	long := "/nowhere/" + strings.Repeat("deeply-nested/", 4) + "cloneable"
	projects, _ := onDisk(t, []string{"cloneable"}, map[string]string{"cloneable": long},
		map[string]string{"cloneable": "/srv/git/a.git"})
	m := resize(refreshed(t, &core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, stateWith(t, nil), nil), 60, 20)

	path := rows(m)[1]
	if !strings.Contains(path, "not cloned") || !strings.Contains(path, "…") {
		t.Errorf("path line = %q, want the path cut and the tag kept", path)
	}
}

// A narrow row cuts the activity and keeps the state: the state is what the
// row is there to show.
func TestANarrowRowKeepsTheStateAndCutsTheActivity(t *testing.T) {
	c, projects := longWorld(t, "/p/long")
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 60, 20)

	first := rows(m)[0]
	if !strings.Contains(first, "working") || !strings.Contains(first, "…") {
		t.Errorf("row = %q, want the state kept and the activity cut with an ellipsis", first)
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
// agent's state is a glyph and words on the right, in the row and the pane.
func TestTheMarkSaysOpenAndTheAgentSaysItNeedsYou(t *testing.T) {
	_, _, c, projects := world(t, 2) // project-01 is open and its agent needs you
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)
	g := theme.Default().Glyphs

	first, _, _ := strings.Cut(rows(m)[0], "│")
	if !strings.HasPrefix(strings.TrimLeft(first, " "+g.Cursor), g.Running+" project-01") {
		t.Errorf("row = %q, want the open mark in front of the name", first)
	}
	if !strings.Contains(first, g.NeedsYou+" needs you") {
		t.Errorf("row = %q, want the agent's glyph and words", first)
	}
	if body := pane(m); !strings.Contains(body, g.NeedsYou+" needs you") {
		t.Errorf("pane = %q, want the agent's glyph and words", body)
	}
}

// The header's count agrees in number with what it counts.
func TestTheHeaderSaysOneProjectNeedsYou(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	if head := lines(m)[0]; !strings.Contains(head, "1 needs you") {
		t.Errorf("header = %q, want \"1 needs you\"", head)
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

// On a wide terminal the frame stops growing and sits in the middle, as the
// popup did at half the screen: the state stays within reading distance of
// the name, and the pane beside the row it describes.
func TestAWideTerminalCentresTheFrame(t *testing.T) {
	_, _, c, projects := world(t, 3)
	// Twenty rows: a short terminal keeps no row for a margin, and the frame
	// is still centred across the width it does not need.
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 380, 20)

	view := strings.Split(m.View(), "\n")
	var top string
	for _, line := range view {
		if w := lipgloss.Width(line); w > 380 {
			t.Errorf("line is %d columns wide: %q", w, line)
		}
		if strings.Contains(line, "╭") {
			top = line
		}
	}
	lead := len(top) - len(strings.TrimLeft(top, " "))
	frame := lipgloss.Width(strings.TrimSpace(top))
	if lead < 90 || frame > 194 {
		t.Errorf("frame is %d wide after %d columns of margin, want it capped and centred:\n%s", frame, lead, top)
	}
	first, _, split := strings.Cut(rows(m)[0], "│")
	first = strings.TrimPrefix(first, " ") // the frame's padding column
	if !split || lipgloss.Width(first) > 100 || !strings.Contains(first, "needs you") {
		t.Errorf("row = %q, want the state on a list no wider than 100 columns, with the pane beside it", first)
	}
}

// A click on a row selects it, with or without the margin around the frame.
// A click on the pane, or above the rows, moves nothing.
func TestAClickSelectsTheRowUnderIt(t *testing.T) {
	for _, tc := range []struct {
		w, h, top int // top is the terminal row the first project row is on
	}{{120, 20, 4}, {140, 30, 5}} {
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

	m = clickAt(m, 5, 4) // project-02
	m = clickAt(m, 5, 6) // project-00: a second choice, not a double click
	next, cmd := m.Update(tea.MouseMsg{X: 5, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(tui.Model)
	if cmd == nil {
		t.Fatal("a double click on a row returned no command")
	}
	cmd()
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "session:project-00" {
		t.Fatalf("runtime Opened = %v, want project-00's home", rt.Opened)
	}
	if !strings.Contains(lines(m)[0], "3 projects") {
		t.Errorf("a double click must not leave the project level:\n%s", m.View())
	}
}

// clickAt is one press of the left button on a terminal cell.
func clickAt(m tui.Model, x, y int) tui.Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(tui.Model)
}

// margins reads the frame's margin off the rendered surface: the rows above
// its top border and the columns left of it.
func margins(m tui.Model) (rows, cols int) {
	for i, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "╭") {
			return i, len(line) - len(strings.TrimLeft(line, " "))
		}
	}
	return 0, 0
}

// A target with nothing up says "stopped", in the pane and at the target
// level, where it said "-".
func TestAStoppedTargetSaysStopped(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)

	if body := pane(m); !strings.Contains(body, "stopped") {
		t.Errorf("pane = %q, want the editor target stopped", body)
	}
	m, _ = press(m, "tab")
	var editor string
	for _, line := range lines(m) {
		if l, _, _ := strings.Cut(line, "│"); strings.Contains(l, "editor") {
			editor = l
		}
	}
	if !strings.Contains(editor, "stopped") {
		t.Errorf("target row = %q, want it stopped", editor)
	}
}

// The open targets show on a running row when there is more open than the
// home the green mark already stands for.
func TestARunningRowNamesItsOpenTargetsBeyondHome(t *testing.T) {
	rt, wm := hosttest.NewRuntime("rt"), hosttest.New("wm")
	projects, err := core.Prepare([]revier.Project{{Name: "alpha", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:alpha", Launch: []string{"x"}, Match: revier.Match{Title: "^session:alpha$"}}},
		{Name: "editor", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code-alpha$"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	rt.Add("session:alpha", "kitty")
	// Wide enough for the temporary directory's name and the tag beside it.
	m := resize(refreshed(t, &core.Core{Runtime: rt, Window: wm}, projects, stateWith(t, nil), nil), 96, 20)
	if path := rows(m)[1]; strings.Contains(path, "home") {
		t.Errorf("path line = %q, want nothing when only home is open", path)
	}

	wm.Add("editor", "code-alpha")
	m = survey(m)
	if path := rows(m)[1]; !strings.Contains(path, "home · editor") {
		t.Errorf("path line = %q, want the open targets named", path)
	}
}
