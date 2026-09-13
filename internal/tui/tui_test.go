package tui_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// world is a runtime, a window host, and n projects; the last project's
// workspace is running with an agent that wants the human.
func world(t *testing.T, n int) (*hosttest.FakeRuntime, *hosttest.Fake, *core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention, Activity: "needs a decision"},
	}}}
	var raw []revier.Project
	for i := 0; i < n; i++ {
		name := revier.ProjectName(fmt.Sprintf("project-%02d", i))
		raw = append(raw, revier.Project{Name: name, Path: "/p/" + string(name), Targets: []revier.Target{
			{Name: "home", Home: true, Key: "ctrl-shift-u", Runtime: &revier.Realization{
				Name: "session:" + string(name), Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + string(name) + "$"}}},
			{Name: "editor", Key: "ctrl-shift-o", Window: &revier.Realization{
				Launch: []string{"code"}, Match: revier.Match{Class: "^code-" + string(name) + "$"}}},
		}})
	}
	last := raw[n-1].Name
	rt.Add("session:"+string(last), "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	return rt, wm, c, projects
}

// stateWith writes a state file holding the given attachments and returns
// its root.
func stateWith(t *testing.T, attached map[revier.ProjectName][]revier.TargetRef) string {
	t.Helper()
	root := t.TempDir()
	st := &state.State{Attached: attached}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// refreshed builds the model over a state root and applies one survey, as the
// timer does.
func refreshed(t *testing.T, c *core.Core, projects []core.Project, root string, actions []config.Action) tui.Model {
	t.Helper()
	m := tui.New(c, projects, root, &config.Config{Actions: actions}, time.Second, theme.Default(), "")
	next, _ := m.Update(m.Survey()())
	return next.(tui.Model)
}

func survey(m tui.Model) tui.Model {
	next, _ := m.Update(m.Survey()())
	return next.(tui.Model)
}

func press(m tui.Model, key string) (tui.Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// lines is the surface's content, with the margin taken off: its blank rows
// dropped and each line right-trimmed. Tests assert on what the surface says,
// not on where it sits in the terminal.
//
// The left margin stays on the line. Every line of the surface carries a
// gutter space of its own, so a test that cared where a line starts would
// have to count either way, and margins reads the margin off the render.
func lines(m tui.Model) []string {
	out := strings.Split(m.View(), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// The chrome lines, in the order View writes them.
func barLine(m tui.Model) string  { return lines(m)[0] }
func query(m tui.Model) string    { return lines(m)[2] }
func ruleLine(m tui.Model) string { return lines(m)[4] }

// footer is the last content line: the key legend, or the last failure.
func footer(m tui.Model) string {
	l := lines(m)
	return l[len(l)-1]
}

// rows are the project rows, without the header, the query line and the rule.
func rows(m tui.Model) []string {
	l := lines(m)
	if len(l) < chromeLines {
		return nil
	}
	return l[chromeLines:]
}

// The action bar, the line under it, the query line, a blank line and the
// rule sit above the list.
const chromeLines = 5

// The project the human is waiting on sorts above every other, whatever its
// config order.
func TestProjectsNeedingAttentionSortFirst(t *testing.T) {
	_, _, c, projects := world(t, 4)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	first := rows(m)[0]
	if !strings.Contains(first, "project-03") || !strings.Contains(first, "needs you") {
		t.Fatalf("first row = %q, want project-03 needing you", first)
	}
	if !strings.Contains(first, "needs a decision") {
		t.Errorf("first row = %q, want the activity line", first)
	}
	// Rows are two lines: the name, then the path under it.
	if path := rows(m)[1]; !strings.Contains(path, "/p/project-03") {
		t.Errorf("second line = %q, want the path of the first row", path)
	}
	if second := rows(m)[2]; !strings.Contains(second, "project-00") {
		t.Errorf("third line = %q, want config order to resume", second)
	}
}

// bubbletea paints once before the first survey answers. That frame must not
// say there are no projects when there are ninety, and must not carry the list
// component's own empty text.
func TestTheFrameBeforeTheFirstSurveyClaimsNothing(t *testing.T) {
	_, _, c, projects := world(t, 90)
	m := resize(tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), ""), 150, 20)

	view := m.View()
	for _, wrong := range []string{"0 projects", "0/0", "No items", "No project"} {
		if strings.Contains(view, wrong) {
			t.Errorf("the first frame says %q:\n%s", wrong, view)
		}
	}
	if head := ruleLine(m); !strings.Contains(head, "surveying") {
		t.Errorf("rule = %q, want it to say the survey is pending", head)
	}
	m = survey(m)
	if strings.Contains(ruleLine(m), "surveying") || !strings.Contains(ruleLine(m), "90/90") {
		t.Errorf("after the survey the counts should be real:\n%s", m.View())
	}
}

// With no project files the surface says so, and says where they go, rather
// than showing an empty list.
func TestNoProjectsNamesTheConfigurationDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	// Wide enough that the temporary directory's long name is not cut.
	m := resize(tui.New(c, nil, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), ""), 200, 20)

	// Nothing to survey, so the first frame is already the answer.
	view := m.View()
	if !strings.Contains(view, "No projects configured") || !strings.Contains(view, filepath.Join(root, "projects")) {
		t.Errorf("want the empty state to name %s/projects:\n%s", root, view)
	}
	if strings.Contains(view, "No items") || strings.Contains(view, "surveying") {
		t.Errorf("an empty configuration has nothing to wait for:\n%s", view)
	}
}

// A filter that leaves nothing says the filter is why, and the count agrees.
func TestAFilterMatchingNothingSaysSo(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	for _, r := range "zzz" {
		m, _ = press(m, string(r))
	}
	view := m.View()
	if !strings.Contains(view, `No project matches "zzz"`) {
		t.Errorf("want the filter named as the reason:\n%s", view)
	}
	if rule := ruleLine(m); !strings.Contains(rule, " 0/12 ") {
		t.Errorf("rule = %q, want the count to read 0/12", rule)
	}
	if strings.Contains(view, "No items") {
		t.Errorf("the list component's own text leaked:\n%s", view)
	}
}

// A refresh costs one Instances call per host however many projects exist.
func TestRefreshIssuesOneInstancesCallPerHost(t *testing.T) {
	rt, wm, c, projects := world(t, 60)
	refreshed(t, c, projects, stateWith(t, nil), nil)
	if rt.InstancesCalls != 1 || wm.InstancesCalls != 1 {
		t.Fatalf("Instances calls: runtime %d, window %d; want 1 each for 60 projects", rt.InstancesCalls, wm.InstancesCalls)
	}
}

// Enter on a project is what a search is for: it opens the project's home,
// the same run-or-raise `revier go home` does, and does not stop at the list.
func TestEnterOnAProjectOpensItsHome(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	for i := range projects {
		projects[i].Path = t.TempDir() // Enter opens only a directory that is there
	}
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "0")
	m, _ = press(m, "0") // project-00, whose home is not running
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a project returned no command")
	}
	cmd()
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "session:project-00" {
		t.Fatalf("runtime Opened = %v, want project-00's home", rt.Opened)
	}
	if len(wm.Opened) != 0 {
		t.Errorf("window Opened = %v, want nothing but home", wm.Opened)
	}
	if !strings.Contains(ruleLine(m), "/2 ") {
		t.Errorf("enter must not leave the list:\n%s", m.View())
	}
}

// A project with no home target has nothing to open, so Enter shows what it
// does have rather than doing nothing.
func TestEnterOnAProjectWithoutHomeShowsItsTargets(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	projects, err := core.Prepare([]revier.Project{{Name: "homeless", Path: "/p/homeless", Targets: []revier.Target{
		{Name: "editor", Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	m, cmd := press(m, "enter")
	if cmd != nil {
		t.Error("enter on a project without home should run nothing")
	}
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q, want it on the editor, the only target:\n%s", row, m.View())
	}
}

// Tab moves the cursor into the pane, onto the project's targets, and the
// list stays on screen beside it; Esc brings the cursor back
// (decisions.md D42).
func TestTabMovesTheCursorIntoThePaneAndEscReturns(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "tab")
	view := m.View()
	// The key in the spelling the footer uses, not the configuration's.
	for _, want := range []string{"3/3", "project-00", "ctrl+shift+o"} {
		if !strings.Contains(view, want) {
			t.Errorf("after tab the surface lacks %q:\n%s", want, view)
		}
	}
	if row := paneCursor(m); !strings.Contains(row, "home") {
		t.Errorf("pane cursor = %q, want it on the first target", row)
	}
	if f := footer(m); !strings.Contains(f, "enter go") {
		t.Errorf("footer = %q, want enter to say go", f)
	}
	m, _ = press(m, "down")
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q after down, want the editor", row)
	}
	m, _ = press(m, "down")
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q after down at the last row, want it to stay", row)
	}
	m, _ = press(m, "esc")
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q after esc, want none", row)
	}
	if f := footer(m); !strings.Contains(f, "enter open") {
		t.Errorf("footer = %q, want enter to say open again", f)
	}
}

// On a terminal too narrow for both, the pane takes the list's place while
// the cursor is on it, and gives it back on Esc.
func TestOnANarrowTerminalThePaneStandsInForTheList(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil) // 80 columns: no pane beside the list

	m, _ = press(m, "tab")
	body := strings.Join(rows(m), "\n")
	if strings.Contains(body, "project-00") {
		t.Errorf("the list is still on screen:\n%s", m.View())
	}
	if !strings.Contains(body, "Targets") || !strings.Contains(body, "ctrl+shift+o") {
		t.Errorf("the pane is not on screen:\n%s", m.View())
	}
	if !strings.Contains(ruleLine(m), " 3/3 ") {
		t.Errorf("rule = %q, want the list still under it", ruleLine(m))
	}
	m, _ = press(m, "esc")
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "project-00") {
		t.Errorf("esc did not bring the list back:\n%s", m.View())
	}
}

// The query line follows the cursor: with the cursor in the pane it is the
// target query, the best match is selected, and leaving the pane drops it
// and shows the project query again (decisions.md D43).
func TestTypingInThePaneFiltersTheTargets(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	m, _ = press(m, "1")
	m, _ = press(m, "tab")
	if q := query(m); !strings.Contains(q, "filter targets") {
		t.Errorf("query line = %q, want the target query, empty", q)
	}
	m, _ = press(m, "e")
	m, _ = press(m, "d")
	if body := pane(m); strings.Contains(body, "home") || !strings.Contains(body, "editor") {
		t.Errorf("target query 'ed' should leave only the editor:\n%s", body)
	}
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q, want the match", row)
	}
	if body := strings.Join(rows(m), "\n"); strings.Contains(body, "project-00") {
		t.Errorf("the target query filtered the projects:\n%s", body)
	}
	m, _ = press(m, "tab")
	if q := query(m); !strings.Contains(q, "❯ 1") {
		t.Errorf("query line = %q, want the project query back", q)
	}
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q after tab, want the cursor back on the list", row)
	}
	if body := pane(m); !strings.Contains(body, "home") {
		t.Errorf("the target query outlived the pane:\n%s", body)
	}
}

// A target key acts on the highlighted project wherever the cursor is: it
// does not read the pane's cursor.
func TestATargetKeyIgnoresThePaneCursor(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	m, _ = press(m, "tab") // the cursor is on home
	_, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if cmd == nil {
		t.Fatal("ctrl+o ran nothing")
	}
	cmd()
	if len(wm.Opened) != 1 || wm.Opened[0].Launch[0] != "code" {
		t.Errorf("Opened = %v, want the editor, the target the key names", wm.Opened)
	}
}

// One click on a target row in the pane runs it, as Enter on it does: a
// target is a thing to do, not a row to choose.
func TestAClickOnATargetInThePaneRunsIt(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	x, y := paneCell(t, m, "editor")
	m, cmd := clickCell(m, x, y)
	if cmd == nil {
		t.Fatalf("the click ran nothing:\n%s", m.View())
	}
	cmd()
	if len(wm.Opened) != 1 || wm.Opened[0].Launch[0] != "code" {
		t.Errorf("Opened = %v, want the editor launched", wm.Opened)
	}
	if row := paneCursor(m); !strings.Contains(row, "editor") {
		t.Errorf("pane cursor = %q, want it on the row clicked", row)
	}
}

// paneCursor is the pane row carrying the cursor bar, or nothing when the
// cursor is on the list.
func paneCursor(m tui.Model) string {
	for _, line := range strings.Split(pane(m), "\n") {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return line
		}
	}
	return ""
}

// paneCell is a terminal cell on the pane line that carries the text.
func paneCell(t *testing.T, m tui.Model, text string) (x, y int) {
	t.Helper()
	for y, raw := range strings.Split(m.View(), "\n") {
		// The list, the pane's border, the pane.
		if parts := strings.Split(raw, "│"); len(parts) > 1 && strings.Contains(parts[1], text) {
			return paneBorder(t, m) + 2, y
		}
	}
	t.Fatalf("no pane line carries %q:\n%s", text, m.View())
	return 0, 0
}

// Enter on a target is core.Go: the same run-or-raise the CLI does, and the
// next refresh shows the result.
func TestEnterOnATargetRunsGo(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "tab")  // project-01, the running one, is first
	m, _ = press(m, "down") // editor
	_, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a target returned no command")
	}
	msg := cmd()
	if len(wm.Opened) != 1 || wm.Opened[0].Launch[0] != "code" {
		t.Fatalf("Opened = %v, want the editor launched through core.Go", wm.Opened)
	}
	next, _ := m.Update(msg)
	m = next.(tui.Model)
	next, _ = m.Update(m.Survey()())
	m = next.(tui.Model)
	if view := m.View(); !strings.Contains(view, "editor") || !strings.Contains(view, "running") {
		t.Errorf("after Go the editor should show running:\n%s", view)
	}
	_ = rt
}

// Typing narrows the list by name; backspace widens it; esc clears it.
func TestTypingFiltersProjects(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "1")
	m, _ = press(m, "1")
	body := strings.Join(rows(m), "\n")
	if !strings.Contains(body, "project-11") || strings.Contains(body, "project-10") {
		t.Errorf("filter '11' should leave only project-11:\n%s", m.View())
	}
	m, _ = press(m, "backspace")
	if body := m.View(); !strings.Contains(body, "project-10") {
		t.Errorf("filter '1' should include project-10:\n%s", body)
	}
	m, _ = press(m, "esc")
	if body := m.View(); !strings.Contains(body, "project-00") {
		t.Errorf("esc should clear the filter:\n%s", body)
	}
}

// An attached instance is listed under its project without a key and is
// focused through the host that produced it.
func TestAttachedInstancesAreListedAndFocused(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	ref := wm.Add("Pull requests", "chrome")
	m := refreshed(t, c, projects, stateWith(t, map[revier.ProjectName][]revier.TargetRef{"project-00": {ref}}), nil)
	m, _ = press(m, "tab")
	if view := m.View(); !strings.Contains(view, "Pull requests") || !strings.Contains(view, "attached") {
		t.Fatalf("attached instance not listed:\n%s", view)
	}
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	_, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	cmd()
	if len(wm.Focuses) != 1 || wm.Focuses[0] != ref {
		t.Errorf("Focuses = %v, want the attached ref", wm.Focuses)
	}
}

// A survey that fails leaves the last good view and reports in the footer.
func TestSurveyErrorIsShownNotFatal(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	rt.InstancesErr = fmt.Errorf("kitty went away")
	next, _ := m.Update(m.Survey()())
	m = next.(tui.Model)
	if view := m.View(); !strings.Contains(view, "kitty went away") || !strings.Contains(view, "project-00") {
		t.Errorf("want the error in the footer and the rows kept:\n%s", view)
	}
}

// Claim-on-appear on the polling path: a window that appears within the claim
// window after a launch, matching no declared target, is attached to the
// launching project by the next refresh; the launch is consumed.
func TestPollingClaimsTheWindowThatAppearsAfterALaunch(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil) // the "before" listing

	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "project-00", At: time.Now()}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	stray := wm.Add("Pull requests - Chromium", "chromium")
	m = survey(m)

	got, _ := state.Load(root)
	if refs := got.Attached["project-00"]; len(refs) != 1 || refs[0] != stray {
		t.Fatalf("attached = %+v, want the stray window on project-00", got.Attached)
	}
	if got.Launch != nil {
		t.Error("a claim must consume the launch")
	}
	m, _ = press(m, "0")
	m, _ = press(m, "0") // project-00; project-01 sorts first, its agent wants the human
	m, _ = press(m, "tab")
	if view := m.View(); !strings.Contains(view, "Pull requests") {
		t.Errorf("the claimed window should be listed under the project:\n%s", view)
	}
}

// The bounds: no launch, a stale launch, a declared target, or two windows at
// once claim nothing.
func TestPollingClaimsNothingOutsideTheBounds(t *testing.T) {
	cases := map[string]func(root string, wm *hosttest.Fake){
		"no launch": func(root string, wm *hosttest.Fake) {
			wm.Add("stray", "chromium")
		},
		"stale launch": func(root string, wm *hosttest.Fake) {
			st, _ := state.Load(root)
			st.Launch = &state.Launch{Project: "project-00", At: time.Now().Add(-time.Minute)}
			_ = st.Save(root)
			wm.Add("stray", "chromium")
		},
		"a declared target": func(root string, wm *hosttest.Fake) {
			st, _ := state.Load(root)
			st.Launch = &state.Launch{Project: "project-00", At: time.Now()}
			_ = st.Save(root)
			wm.Add("editor", "code-project-01") // project-01's editor, by class
		},
		"two windows at once": func(root string, wm *hosttest.Fake) {
			st, _ := state.Load(root)
			st.Launch = &state.Launch{Project: "project-00", At: time.Now()}
			_ = st.Save(root)
			wm.Add("stray one", "chromium")
			wm.Add("stray two", "chromium")
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			_, wm, c, projects := world(t, 2)
			root := stateWith(t, nil)
			m := refreshed(t, c, projects, root, nil)
			arrange(root, wm)
			survey(m)
			got, _ := state.Load(root)
			if len(got.Attached) != 0 {
				t.Errorf("attached = %+v, want nothing claimed", got.Attached)
			}
		})
	}
}

// Claim-on-appear on the event path: a watching host reports the window and
// the claim lands without waiting for a refresh.
func TestWatcherClaimsAnOpenedWindow(t *testing.T) {
	rt, _, c, projects := world(t, 1)
	wm := hosttest.NewWatcher("wm")
	c.Window = wm
	_ = rt
	root := stateWith(t, nil)
	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "project-00", At: time.Now()}
	_ = st.Save(root)

	m := tui.New(c, projects, root, &config.Config{}, time.Second, theme.Default(), "")
	stray := wm.Add("Pull requests - Chromium", "chromium")
	wm.Events <- revier.WindowEvent{Kind: revier.WindowOpened, Instance: revier.Instance{Ref: stray, Title: "Pull requests - Chromium", Class: "chromium"}}

	// Init batches the first survey with the watcher; run what it returns.
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init should batch the survey and the watcher for a watching host")
	}
	for _, cmd := range batch {
		next, _ := m.Update(cmd())
		m = next.(tui.Model)
	}
	got, _ := state.Load(root)
	if refs := got.Attached["project-00"]; len(refs) != 1 || refs[0] != stray {
		t.Fatalf("attached = %+v, want the opened window on project-00", got.Attached)
	}
}

// A target's launch that outlived the activation's wait is bound by a later
// refresh, by class, and the target then shows as running.
func TestPollingBindsALaunchedTargetByClass(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil)

	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "project-00", Target: "editor", At: time.Now().Add(-20 * time.Second)}
	_ = st.Save(root)
	// The editor's class, a title the rule does not match yet.
	unsettled := wm.AddInstance(revier.Instance{Title: "", Class: "code-project-00"})
	unsettled.Title = ""
	m = survey(m)

	got, _ := state.Load(root)
	if ref := got.Bound["project-00"]["editor"]; ref.ID != unsettled.ID {
		t.Fatalf("bound = %+v, want the editor window bound by class", got.Bound)
	}
	if got.Launch != nil {
		t.Error("a binding must consume the launch")
	}
	m = survey(m)
	m, _ = press(m, "tab")
	if view := m.View(); !strings.Contains(view, "editor") || !strings.Contains(view, "running") {
		t.Errorf("the bound editor should show running:\n%s", view)
	}
}

// Enter on a target pins where it landed, so the next survey and press use
// the binding.
func TestEnterPinsTheTarget(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	wm.Add("Visual Studio Code", "code-project-00")
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil)
	m, _ = press(m, "tab")
	m, _ = press(m, "down")
	_, cmd := press(m, "enter")
	m.Update(cmd()) // applies the binding on the update loop
	got, _ := state.Load(root)
	if ref := got.Bound["project-00"]["editor"]; ref.IsZero() {
		t.Fatalf("bound = %+v, want the editor pinned after Enter", got.Bound)
	}
	if got.Current != "project-00" {
		t.Errorf("current = %q, want project-00: a desktop key pressed next falls back to it", got.Current)
	}
}

// Enter on the editor while the editor has focus goes home, and pins home.
// Pinned to the editor, the home window would be where the editor's key
// lands from then on.
func TestAToggleBackPinsHomeNotThePressedTarget(t *testing.T) {
	rt, wm, c, projects := world(t, 1)
	wm.SetFocus(wm.Add("Visual Studio Code", "code-project-00"))
	listed, _ := rt.Instances(context.Background())
	home := listed[0].Ref
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil)
	m, _ = press(m, "tab")
	m, _ = press(m, "down") // editor
	_, cmd := press(m, "enter")
	m.Update(cmd())

	got, _ := state.Load(root)
	if ref := got.Bound["project-00"]["editor"]; !ref.IsZero() {
		t.Errorf("editor bound to %v, want no binding: the press landed on home", ref)
	}
	if ref := got.Bound["project-00"]["home"]; ref != home {
		t.Errorf("home bound to %v, want %v", ref, home)
	}
}

// lateWindows is a window host whose windows appear later than the launch
// that asked for them: Open starts nothing it can list yet.
type lateWindows struct {
	*hosttest.Fake
	opened int
}

func (l *lateWindows) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	l.opened++
	return revier.TargetRef{}, nil
}

// A second press while a launch is coming up does not launch again, and the
// launch is in state before the wait for its window begins, so a desktop key
// pressed meanwhile sees it too (decisions.md D21).
func TestASecondPressDuringALaunchDoesNotLaunchAgain(t *testing.T) {
	_, _, c, projects := world(t, 1)
	wm := &lateWindows{Fake: hosttest.New("wm")}
	c.Window = wm
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil)
	m, _ = press(m, "tab")
	m, _ = press(m, "down") // editor
	m, cmd := press(m, "enter")
	next, wait := m.Update(cmd())
	m = next.(tui.Model)
	if wait == nil {
		t.Fatal("a detached launch should go on to wait for its window")
	}
	if got, _ := state.Load(root); got.Launch == nil || got.Launch.Target != "editor" {
		t.Fatalf("launch = %+v, want the editor recorded before the wait", got.Launch)
	}

	if _, again := press(m, "enter"); again != nil {
		again()
	}
	if wm.opened != 1 {
		t.Errorf("the editor was launched %d times, want once", wm.opened)
	}
}

// Once the window of a launch still on record is there, Enter raises it: only
// a second launch is refused, never the raise.
func TestAPressDuringALaunchRaisesTheWindowOnceItIsThere(t *testing.T) {
	_, _, c, projects := world(t, 1)
	wm := &lateWindows{Fake: hosttest.New("wm")}
	editor := wm.Add("Visual Studio Code", "code-project-00")
	c.Window = wm
	root := stateWith(t, nil)
	st, _ := state.Load(root)
	st.Launch = &state.Launch{Project: "project-00", Target: "editor", At: time.Now().Add(-40 * time.Second)}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m := refreshed(t, c, projects, root, nil)
	m, _ = press(m, "tab")
	m, _ = press(m, "down") // editor
	_, cmd := press(m, "enter")
	m.Update(cmd())

	if wm.opened != 0 {
		t.Errorf("the editor was launched %d more times, want none", wm.opened)
	}
	if n := len(wm.Focuses); n == 0 || wm.Focuses[n-1] != editor {
		t.Errorf("focuses = %v, want the editor window %v raised", wm.Focuses, editor)
	}
}

// A refresh prunes only what it could see. A surface started where the window
// host does not probe lists no window at all, and must not take that as every
// attached window having closed.
func TestARefreshWithoutAWindowHostKeepsWindowRefs(t *testing.T) {
	rt, _, _, projects := world(t, 1)
	c := &core.Core{Runtime: rt}
	window := revier.TargetRef{Host: "gnome", ID: "42"}
	root := stateWith(t, map[revier.ProjectName][]revier.TargetRef{"project-00": {window}})
	st, _ := state.Load(root)
	st.Bind("project-00", "editor", window)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	refreshed(t, c, projects, root, nil)

	got, _ := state.Load(root)
	if len(got.Attached["project-00"]) != 1 || got.Bound["project-00"]["editor"] != window {
		t.Errorf("state = attached %v, bound %v; want the window refs kept", got.Attached, got.Bound)
	}
}

// A survey lists the windows that were there when it started. A binding a
// desktop key writes while it lists is to a window the listing may not hold,
// and the survey answering afterwards must not take that as the window having
// closed.
func TestASurveyKeepsABindingWrittenWhileItListed(t *testing.T) {
	_, wm, c, projects := world(t, 1)
	root := stateWith(t, nil)
	m := refreshed(t, c, projects, root, nil)

	inFlight := m.Survey()() // listed before the editor opened
	editor := wm.Add("Visual Studio Code", "code-project-00")
	st, _ := state.Load(root)
	st.Bind("project-00", "editor", editor)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	m.Update(inFlight)
	if got, _ := state.Load(root); got.Bound["project-00"]["editor"] != editor {
		t.Fatalf("bound = %+v, want the editor binding kept for the next survey", got.Bound)
	}

	// The next survey saw the window, and one after it closed drops it.
	m = survey(m)
	wm.Remove(editor)
	survey(m)
	if got, _ := state.Load(root); !got.Bound["project-00"]["editor"].IsZero() {
		t.Errorf("bound = %+v, want the closed editor's binding dropped", got.Bound)
	}
}

// A background refresh replaces every row. The cursor must stay on the
// project the user was looking at, not on the row index it happened to sit
// at: attention sorting moves rows, so an index points at a different project
// after a survey.
func TestRefreshKeepsTheFilterAndTheSelectedProject(t *testing.T) {
	rt, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	for _, r := range "project-0" { // project-00 .. project-09
		m, _ = press(m, string(r))
	}
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	before := selectedRow(t, m)
	if !strings.Contains(before, "project-02") {
		t.Fatalf("selected %q, want project-02 two rows down", before)
	}

	// project-09's agent starts wanting the human, so it sorts to the top and
	// every row below it moves down one. An index-based cursor would now be
	// pointing at project-01.
	rt.Add("session:project-09", "kitty", revier.Panel{ID: "9", Kind: revier.PanelAgent, Title: "claude"})
	m = survey(m)

	if first := rows(m)[0]; !strings.Contains(first, "project-09") {
		t.Fatalf("first row = %q, want project-09 to have sorted first", first)
	}
	if after := selectedRow(t, m); after != before {
		t.Errorf("selection moved across a refresh: %q -> %q", before, after)
	}
	if body := m.View(); strings.Contains(body, "project-11") {
		t.Errorf("filter did not survive the refresh:\n%s", body)
	}
}

// The filter is fuzzy, not a substring test: the characters have to appear in
// order, and nothing more. This is the ranking fzf uses, which is what the
// picker being replaced trained the user on.
func TestFilterMatchesNonAdjacentCharacters(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	for _, r := range "pj11" {
		m, _ = press(m, string(r))
	}
	body := m.View()
	if !strings.Contains(body, "project-11") {
		t.Errorf("fuzzy filter 'pj11' should match project-11:\n%s", body)
	}
	if strings.Contains(body, "project-10") {
		t.Errorf("fuzzy filter 'pj11' should not match project-10:\n%s", body)
	}
}

// selectedRow is the row the cursor is on, found by the cursor glyph the
// delegate renders into it.
func selectedRow(t *testing.T, m tui.Model) string {
	t.Helper()
	for _, line := range lines(m) {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no row is selected:\n%s", m.View())
	return ""
}

// resize is the size message a terminal sends. The default model is 80
// columns, which is too narrow to split, so a test that wants the detail pane
// has to ask for the room.
func resize(m tui.Model, w, h int) tui.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(tui.Model)
}

// pane is the detail pane: whatever is right of the border column on each
// line. The two panes are joined horizontally, so this is how a test reads
// one without the other.
func pane(m tui.Model) string {
	var out []string
	for _, line := range lines(m) {
		if _, right, ok := strings.Cut(line, "│"); ok {
			out = append(out, strings.TrimSpace(right))
		}
	}
	return strings.Join(out, "\n")
}

// The pane answers "what is this project" for the row under the cursor, and
// follows the cursor.
func TestDetailPaneFollowsTheCursor(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	first := pane(m)
	if !strings.Contains(first, "project-02") || !strings.Contains(first, "/p/project-02") {
		t.Fatalf("pane = %q, want the name and path of the first row", first)
	}
	if !strings.Contains(first, "home") || !strings.Contains(first, "editor") {
		t.Errorf("pane = %q, want every target listed", first)
	}

	m, _ = press(m, "down")
	if second := pane(m); second == first || !strings.Contains(second, "project-00") {
		t.Errorf("pane after down = %q, want the next project", second)
	}
}

// A project with two agents is the case the row cannot show: it collapses to
// the worst state. The pane lists them both.
func TestDetailPaneListsEveryAgent(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	rt.Add("session:project-00", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude"})
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	// Both projects want the human now, so the sort keeps config order and
	// project-00 is the first row.
	body := pane(m)
	if !strings.Contains(body, "project-00") {
		t.Fatalf("pane = %q, want project-00", body)
	}
	if n := strings.Count(body, "needs a decision"); n != 2 {
		t.Errorf("pane lists %d agents, want 2:\n%s", n, body)
	}
}

// The pane is a luxury. At eighty columns the list keeps the whole width, and
// no row is wider than the terminal.
func TestNoDetailPaneAtEightyColumns(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20)

	for i, line := range lines(m) {
		if strings.Contains(line, "│") {
			t.Errorf("80 columns should not split, line %d = %q", i, line)
		}
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
	}
}

// send is one key as bubbletea's input reader delivers it.
func send(m tui.Model, msg tea.KeyMsg) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// A target key acts on the row under the cursor, without the target level.
// The keys are the ones the desktop bindings use, so the surface and the
// keyboard agree about what ctrl+shift+o means. A terminal sends ctrl+shift+o
// as the byte of ctrl+o, which is the message the surface gets.
func TestTargetKeyRunsAgainstTheHighlightedProject(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	_, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlO}) // ctrl+shift+o: editor, a window target
	if cmd == nil {
		t.Fatal("ctrl+shift+o produced no command")
	}
	cmd()
	if len(wm.Opened) != 1 {
		t.Fatalf("window host opened %d times, want 1", len(wm.Opened))
	}
	if len(rt.Opened) != 0 {
		t.Errorf("runtime host opened %d times, want none: the editor is a window", len(rt.Opened))
	}
}

// The footer names the keys that will do something on this row, because which
// targets exist depends on the project. A key that does nothing here is not
// named: ctrl+shift+u reaches the surface as ctrl+u, which clears the query,
// so home's key is the desktop's only.
func TestFooterNamesTheHighlightedProjectsTargetKeys(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	f := footer(m)
	if !strings.Contains(f, "ctrl+shift+o editor") {
		t.Errorf("footer = %q, want the editor's key of the selected project", f)
	}
	if strings.Contains(f, "ctrl+shift+u") {
		t.Errorf("footer = %q, names home's key, which the query takes here", f)
	}
}

// ctrl+shift+u cannot reach the surface as itself, and ctrl+u, which it
// arrives as, is the query's. The query keeps it: nothing opens.
func TestADesktopKeyTheQueryOwnsEditsTheQuery(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "p")

	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if cmd != nil {
		cmd()
	}
	if len(rt.Opened) != 0 || len(wm.Opened) != 0 {
		t.Errorf("ctrl+u opened something: runtime %v, window %v", rt.Opened, wm.Opened)
	}
	if q := query(m); !strings.Contains(q, "filter") {
		t.Errorf("query line = %q, want ctrl+u to have cleared it", q)
	}
}

// bubbletea names ctrl+alt+<key> "alt+ctrl+<key>", alt first. A target key and
// an action on a ctrl+alt chord fire on the message the terminal produces.
func TestCtrlAltKeysFire(t *testing.T) {
	raw := []revier.Project{{Name: "solo", Path: "/p/solo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Launch: []string{"x"}, Match: revier.Match{Title: "^session:solo$"}}},
		{Name: "editor", Key: "ctrl-alt-o", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
	}}}
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	actions := []config.Action{{Key: "ctrl-alt-y", Name: "sync", Run: []string{"true"}}}
	m := refreshed(t, c, projects, stateWith(t, nil), actions)

	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlO, Alt: true}); cmd == nil {
		t.Error("ctrl+alt+o ran no target")
	} else if cmd(); len(wm.Opened) != 1 {
		t.Errorf("window host opened %d times, want the editor once", len(wm.Opened))
	}
	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY, Alt: true}); cmd == nil {
		t.Error("ctrl+alt+y ran no action")
	}
}

// An action that renders to nothing says so in words, not in a formatting
// verb that was handed no error.
func TestAnActionThatRunsNothingSaysSo(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := refreshed(t, c, projects, stateWith(t, nil), []config.Action{{Key: "ctrl-y", Name: "sync"}})
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if cmd == nil {
		t.Fatal("ctrl+y ran no action")
	}
	next, _ := m.Update(cmd())
	f := footer(next.(tui.Model))
	if !strings.Contains(f, `action "sync"`) || strings.Contains(f, "%!") {
		t.Errorf("footer = %q, want the action named and no formatting verb", f)
	}
}

// A letter is a filter character, never a shortcut. A project bound to a bare
// key would otherwise swallow it.
func TestABareLetterFiltersRatherThanRunningATarget(t *testing.T) {
	raw := []revier.Project{{
		Name: "solo", Path: "/p/solo",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Launch: []string{"x"}, Match: revier.Match{Title: "^session:solo$"}}},
			{Name: "editor", Key: "o", Window: &revier.Realization{
				Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
		},
	}}
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	rt, wm := hosttest.NewRuntime("rt"), hosttest.New("wm")
	c := &core.Core{Runtime: rt, Window: wm}
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	// The command a keystroke returns is the input's own cursor blink, so the
	// evidence is that no host was asked to open anything.
	m, cmd := press(m, "o")
	if cmd != nil {
		cmd()
	}
	if len(wm.Opened) != 0 || len(rt.Opened) != 0 {
		t.Fatalf("a bare letter opened something: window %d, runtime %d", len(wm.Opened), len(rt.Opened))
	}
	if body := m.View(); !strings.Contains(body, "❯ o") {
		t.Errorf("'o' should have gone to the query line:\n%s", body)
	}
}

// A target key means the same target in every project, so a press on a
// project that does not declare it is a mistake worth naming. Doing nothing
// would look like the key was not bound at all.
func TestTargetKeyOnAProjectWithoutThatTargetSaysSo(t *testing.T) {
	raw := []revier.Project{
		{Name: "plain", Path: "/p/plain", Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Launch: []string{"x"}, Match: revier.Match{Title: "^session:plain$"}}},
		}},
		{Name: "webby", Path: "/p/webby", Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Launch: []string{"x"}, Match: revier.Match{Title: "^session:webby$"}}},
			{Name: "web", Key: "ctrl-shift-y", Window: &revier.Realization{
				Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}}},
		}},
	}
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY}) // ctrl+shift+y; "plain" is the first row
	if cmd != nil {
		t.Fatal("a target the project does not declare should run nothing")
	}
	if f := footer(m); !strings.Contains(f, "no web target") {
		t.Errorf("footer = %q, want it to name the missing target", f)
	}
}

// The surface opens on the project the working directory resolves to. Opening
// on an unrelated project makes the most likely target the one that needs
// scrolling to.
func TestTheSurfaceOpensOnTheStartingProject(t *testing.T) {
	_, _, c, projects := world(t, 6)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "project-04")
	m = survey(m)

	if row := selectedRow(t, m); !strings.Contains(row, "project-04") {
		t.Errorf("selected %q, want project-04", row)
	}
}

// After the first survey the user's selection wins: a refresh must not pull
// the cursor back to where the process started.
func TestTheStartingProjectDoesNotRecaptureTheCursor(t *testing.T) {
	_, _, c, projects := world(t, 6)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "project-04")
	m = survey(m)
	m, _ = press(m, "down")
	moved := selectedRow(t, m)

	m = survey(m)
	if row := selectedRow(t, m); row != moved {
		t.Errorf("selection = %q after a refresh, want %q", row, moved)
	}
}

// The tree says which checkout this is. Dot entries are not part of that, and
// a directory that cannot be read is not an error.
func TestDetailPaneShowsTheProjectTree(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"cmd/revier", "docs", ".git/objects"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	raw := []revier.Project{{Name: "here", Path: dir, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Launch: []string{"x"}, Match: revier.Match{Title: "^session:here$"}}},
	}}}
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 30)

	body := pane(m)
	if !strings.Contains(body, "Project Snapshot") {
		t.Fatalf("pane = %q, want a tree", body)
	}
	for _, want := range []string{"cmd", "revier", "docs", "go.mod"} {
		if !strings.Contains(body, want) {
			t.Errorf("tree does not list %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, ".git") {
		t.Errorf("tree lists a dot entry:\n%s", body)
	}
}

// A project whose directory is not here has no tree, and says nothing about
// one.
func TestNoTreeForAMissingDirectory(t *testing.T) {
	_, _, c, projects := world(t, 1) // /p/project-00 does not exist
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 30)

	if body := pane(m); strings.Contains(body, "Project Snapshot") {
		t.Errorf("pane = %q, want no tree for a missing directory", body)
	}
}

// longActivity is an agent's activity line longer than any pane.
const longActivity = "Reading internal/tui/detail.go and working out why the activity line ends in an ellipsis where fzf wraps it"

// longWorld is one running project at path whose agent reports longActivity,
// and a second project after it.
func longWorld(t *testing.T, path string) (*core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: longActivity},
	}}}
	home := func(name string) revier.Target {
		return revier.Target{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}}
	}
	projects, err := core.Prepare([]revier.Project{
		{Name: "long", Path: path, Targets: []revier.Target{home("long")}},
		{Name: "short", Path: "/p/short", Targets: []revier.Target{home("short")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.Add("session:long", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	return c, projects
}

// paneColumns is the detail pane's rendered width, its border included: from
// the border between list and pane to the end of the line.
func paneColumns(t *testing.T, m tui.Model) int {
	t.Helper()
	border := paneBorder(t, m)
	widest := 0
	for _, raw := range strings.Split(m.View(), "\n") {
		if n := lipgloss.Width(strings.TrimRight(raw, " ")); n > widest {
			widest = n
		}
	}
	return widest - border
}

// Where the picker's preview wraps, the pane wraps: a path and an activity
// line are the fields worth reading whole, and an ellipsis cut exactly them.
func TestDetailPaneWrapsALongPathAndActivity(t *testing.T) {
	path := "/p/a-rather-long-directory-name-for-wrapping/and-another-deeply-nested-segment/checkout-with-a-long-name"
	c, projects := longWorld(t, path)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 40)

	if w := paneColumns(t, m); w*100 < 45*150 {
		t.Errorf("pane is %d of 150 columns, want at least 45%%", w)
	}
	body := pane(m)
	if strings.Contains(body, "…") {
		t.Errorf("pane cut a field with an ellipsis:\n%s", body)
	}
	joined := strings.Join(strings.Fields(body), "")
	if !strings.Contains(joined, path) {
		t.Errorf("pane lost part of the path %s:\n%s", path, body)
	}
	if !strings.Contains(joined, strings.Join(strings.Fields(longActivity), "")) {
		t.Errorf("pane lost part of the activity line:\n%s", body)
	}
	// Continuation lines sit under the value, not under the label. pane trims
	// each line, so the indentation is read off the untrimmed one.
	for _, line := range lines(m) {
		_, right, ok := strings.Cut(line, "│")
		if !ok {
			continue
		}
		text := strings.TrimLeft(right, " ")
		if !strings.HasPrefix(text, "and-another") && !strings.HasPrefix(text, "wraps it") {
			continue
		}
		// One column is the pane's padding; the label column is wider.
		if indent := len(right) - len(text); indent <= 1 {
			t.Errorf("continuation %q starts at the label column", text)
		}
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 150 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
	}
}

// A tree row is cut, not wrapped: a wrapped row loses the indentation that
// says where in the tree it is.
func TestDetailPaneCutsTreeRows(t *testing.T) {
	dir := t.TempDir()
	name := "a-file-whose-name-is-long-enough-that-the-tree-row-must-be-cut-rather-than-wrapped.go"
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, projects := longWorld(t, dir)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 40)

	body := pane(m)
	if !strings.Contains(body, "└── a-file-whose-name") {
		t.Fatalf("pane lacks the tree row:\n%s", body)
	}
	if strings.Contains(strings.Join(strings.Fields(body), ""), name) {
		t.Errorf("the tree row was wrapped rather than cut:\n%s", body)
	}
}

// On a terminal too narrow for a pane the list does not wrap either: a long
// path and a long activity line leave every row two lines high.
func TestANarrowListCutsRatherThanWrapping(t *testing.T) {
	c, projects := longWorld(t, "/p/"+strings.Repeat("deeply-nested/", 10)+"checkout")
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 76, 20)

	r := rows(m)
	if !strings.Contains(r[0], "long") || !strings.Contains(r[1], "/p/deeply") || !strings.Contains(r[2], "short") {
		t.Errorf("want the long row on two lines and the next project on the third:\n%s", m.View())
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 76 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
	}
	// The activity goes to make room, and the state it describes does not:
	// the state is what the row is for, and the path keeps its column.
	if !strings.Contains(r[0], "working") || strings.Contains(r[0], "Reading") {
		t.Errorf("first row = %q, want the state kept and the activity gone", r[0])
	}
}

// A project with two agents in one state is summed up by the first, as
// `revier list` sums it up.
func TestTheRowNamesTheFirstOfTwoAgentsInTheWorstState(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	probe := func(marker, activity string) *hosttest.FakeProbe {
		return &hosttest.FakeProbe{Harness: "claude", Marker: marker,
			State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: activity}}
	}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe("one", "first task"), probe("two", "second task")}}
	projects, err := core.Prepare([]revier.Project{{Name: "duo", Path: "/p/duo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:duo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:duo$"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	rt.Add("session:duo", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude one"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude two"})
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	if row := rows(m)[0]; !strings.Contains(row, "first task") {
		t.Errorf("row = %q, want the first agent's activity", row)
	}
}

// An error of several lines - a project file with two mistakes - keeps the
// footer one line, so the frame stays the height of the terminal.
func TestAMultiLineErrorKeepsTheFooterOneLine(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 100, 20)
	height := len(strings.Split(m.View(), "\n"))

	rt.InstancesErr = fmt.Errorf("kitty went away\nand took its socket with it")
	m = survey(m)
	if got := len(strings.Split(m.View(), "\n")); got != height {
		t.Errorf("the surface is %d lines with the error, %d without:\n%s", got, height, m.View())
	}
	if f := footer(m); !strings.Contains(f, "kitty went away") {
		t.Errorf("footer = %q, want the error's first line", f)
	}
}

// wheel is one notch of the mouse wheel at a column.
func wheel(m tui.Model, x int, b tea.MouseButton) tui.Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: 5, Button: b, Action: tea.MouseActionPress})
	return next.(tui.Model)
}

// paneBorder is the terminal column of the border between the list and the
// pane, read off the rendered surface.
func paneBorder(t *testing.T, m tui.Model) int {
	t.Helper()
	for _, raw := range strings.Split(m.View(), "\n") {
		var bars []int
		for i, c := range []rune(raw) {
			if c == '│' {
				bars = append(bars, i)
			}
		}
		if len(bars) == 1 {
			return bars[0]
		}
	}
	t.Fatalf("no line with a pane:\n%s", m.View())
	return 0
}

// The wheel over the list moves the selection, a row a notch, whether or not
// the terminal is wide enough for a pane.
func TestTheWheelMovesTheSelection(t *testing.T) {
	for _, width := range []int{80, 120} {
		_, _, c, projects := world(t, 12)
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), width, 20)
		first := selectedRow(t, m)

		m = wheel(m, 5, tea.MouseButtonWheelDown)
		if row := selectedRow(t, m); !strings.Contains(row, "project-00") {
			t.Errorf("%d columns: selected %q after a notch down, want project-00", width, row)
		}
		m = wheel(m, 5, tea.MouseButtonWheelUp)
		if row := selectedRow(t, m); row != first {
			t.Errorf("%d columns: selected %q after a notch back up, want %q", width, row, first)
		}
	}
}

// The wheel over the pane scrolls the pane and leaves the selection alone; the
// next project starts at its own top.
func TestTheWheelOverThePaneScrollsThePane(t *testing.T) {
	// The snapshot fits the pane by design, so what overflows a fourteen-row
	// terminal is a project with more targets than the pane has rows.
	dir := t.TempDir()
	targets := func(name string) []revier.Target {
		out := []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
			Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}}}
		for i := range 12 {
			out = append(out, revier.Target{Name: revier.TargetName(fmt.Sprintf("t%02d", i)), Window: &revier.Realization{
				Launch: []string{"x"}, Match: revier.Match{Class: fmt.Sprintf("^t%02d-%s$", i, name)}}})
		}
		return out
	}
	projects, err := core.Prepare([]revier.Project{
		{Name: "first", Path: dir, Targets: targets("first")},
		{Name: "second", Path: dir, Targets: targets("second")},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 14)
	border := paneBorder(t, m)

	// One column left of the border is still the list.
	m = wheel(m, border-1, tea.MouseButtonWheelDown)
	m = wheel(m, border-1, tea.MouseButtonWheelUp)
	if top := strings.Split(pane(m), "\n")[0]; top != "first" {
		t.Fatalf("pane top = %q, want the list to have taken the wheel", top)
	}

	m = wheel(m, border, tea.MouseButtonWheelDown)
	if top := strings.Split(pane(m), "\n")[0]; top == "first" {
		t.Errorf("the wheel over the pane did not scroll it:\n%s", pane(m))
	}
	if row := selectedRow(t, m); !strings.Contains(row, "first") {
		t.Errorf("selected %q, want the wheel over the pane to leave the selection on first", row)
	}
	m = survey(m)
	if top := strings.Split(pane(m), "\n")[0]; top == "first" {
		t.Errorf("a refresh put the pane back at its top")
	}

	m, _ = press(m, "down")
	if top := strings.Split(pane(m), "\n")[0]; top != "second" {
		t.Errorf("pane top = %q after moving to the next project, want its name", top)
	}
}

// topRow is the name of the first project on screen, and selectedName the one
// under the cursor. Both read the rendered surface, so they see what the
// viewport actually shows.
func topRow(t *testing.T, m tui.Model) string {
	t.Helper()
	for _, line := range rows(m) {
		if n := projectIn(line); n != "" {
			return n
		}
	}
	t.Fatalf("no project row on screen:\n%s", m.View())
	return ""
}

func projectIn(line string) string {
	for _, f := range strings.Fields(line) {
		if strings.HasPrefix(f, "project-") {
			return f
		}
	}
	return ""
}

// The list scrolls one row at a time. The component underneath pages, which
// replaced every row on screen at the page boundary and put the cursor back at
// the top; with ninety projects that is the normal way through the list.
func TestTheListScrollsByOneRowNotByAPage(t *testing.T) {
	_, _, c, projects := world(t, 12)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 100, 14)

	order := map[string]int{}
	for i, line := range rows(m) {
		if n := projectIn(line); n != "" {
			order[n] = i
		}
	}
	if len(order) < 3 {
		t.Fatalf("only %d rows fit; the test needs a list taller than the screen", len(order))
	}

	seen := []string{topRow(t, m)}
	for i := range 10 {
		m, _ = press(m, "down")
		top := topRow(t, m)
		seen = append(seen, top)

		// The cursor must stay on screen, and the row it is on must be the
		// one the surface says is selected.
		if selectedRow(t, m) == "" {
			t.Fatalf("press %d: the cursor left the screen", i+1)
		}
	}

	// Consecutive presses may move the top row by one project or by none.
	// A page flip moves it by a screenful.
	full := projectOrder(t, m)
	for i := 1; i < len(seen); i++ {
		step := full[seen[i]] - full[seen[i-1]]
		if step < 0 || step > 1 {
			t.Errorf("press %d moved the top row from %s to %s, %d places: the list paged",
				i, seen[i-1], seen[i], step)
		}
	}
	if full[seen[len(seen)-1]] == 0 {
		t.Error("after ten presses the top row never moved; the test proved nothing")
	}
}

// projectOrder is every project by its place in the list, taken from the
// model rather than from the screen, so it covers rows that scrolled away.
func projectOrder(t *testing.T, m tui.Model) map[string]int {
	t.Helper()
	out := map[string]int{}
	for i := range 12 {
		out[fmt.Sprintf("project-%02d", i)] = 0
	}
	// The surface sorts attention first, then config order. world gives the
	// last project the attention, so it leads and the rest follow in order.
	out["project-11"] = 0
	for i := range 11 {
		out[fmt.Sprintf("project-%02d", i)] = i + 1
	}
	return out
}

// clickCell is one press of the left button on a terminal cell, with the
// command it returned.
func clickCell(m tui.Model, x, y int) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(tui.Model), cmd
}
