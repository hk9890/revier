package tui_test

import (
	"fmt"
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
	m := tui.New(c, projects, root, actions, time.Second, theme.Default())
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
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

func lines(m tui.Model) []string { return strings.Split(m.View(), "\n") }

// The project the human is waiting on sorts above every other, whatever its
// config order.
func TestProjectsNeedingAttentionSortFirst(t *testing.T) {
	_, _, c, projects := world(t, 4)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	first := lines(m)[1]
	if !strings.Contains(first, "project-03") || !strings.Contains(first, "attention") {
		t.Fatalf("first row = %q, want project-03 with attention", first)
	}
	if !strings.Contains(first, "needs a decision") {
		t.Errorf("first row = %q, want the activity line", first)
	}
	// Rows are two lines: the name, then the path under it.
	if path := lines(m)[2]; !strings.Contains(path, "/p/project-03") {
		t.Errorf("second line = %q, want the path of the first row", path)
	}
	if second := lines(m)[3]; !strings.Contains(second, "project-00") {
		t.Errorf("third line = %q, want config order to resume", second)
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

func TestEnterDrillsIntoTargetsAndEscReturns(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	m, _ = press(m, "enter")
	view := m.View()
	for _, want := range []string{"project-02", "home", "editor", "ctrl-shift-o"} {
		if !strings.Contains(view, want) {
			t.Errorf("target level lacks %q:\n%s", want, view)
		}
	}
	m, _ = press(m, "esc")
	if !strings.Contains(lines(m)[0], "3 projects") {
		t.Errorf("esc did not return to the project level:\n%s", m.View())
	}
}

// Enter on a target is core.Go: the same run-or-raise the CLI does, and the
// next refresh shows the result.
func TestEnterOnATargetRunsGo(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)
	m, _ = press(m, "enter") // project-01, the running one, is first
	m, _ = press(m, "down")  // editor
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
	body := strings.Join(lines(m)[1:], "\n")
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
	m, _ = press(m, "enter")
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
	m, _ = press(m, "enter")
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

	m := tui.New(c, projects, root, nil, time.Second, theme.Default())
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
	m, _ = press(m, "enter")
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
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	_, cmd := press(m, "enter")
	m.Update(cmd()) // applies the binding on the update loop
	got, _ := state.Load(root)
	if ref := got.Bound["project-00"]["editor"]; ref.IsZero() {
		t.Fatalf("bound = %+v, want the editor pinned after Enter", got.Bound)
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

	if first := lines(m)[1]; !strings.Contains(first, "project-09") {
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

	if strings.Contains(m.View(), "│") {
		t.Errorf("80 columns should not split:\n%s", m.View())
	}
	for i, line := range lines(m) {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d columns wide: %q", i, w, line)
		}
	}
}

// A target key acts on the row under the cursor, without the target level.
// The keys are the ones the desktop bindings use, so the surface and the
// keyboard agree about what ctrl+shift+o means.
func TestTargetKeyRunsAgainstTheHighlightedProject(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	_, cmd := press(m, "ctrl+shift+o") // editor, a window target
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
// targets exist depends on the project.
func TestFooterNamesTheHighlightedProjectsTargetKeys(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	footer := lines(m)[len(lines(m))-1]
	if !strings.Contains(footer, "editor") || !strings.Contains(footer, "home") {
		t.Errorf("footer = %q, want the target keys of the selected project", footer)
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

	m, cmd := press(m, "o")
	if cmd != nil {
		t.Fatal("a bare letter should not run a target")
	}
	if body := m.View(); !strings.Contains(body, "/o") {
		t.Errorf("'o' should have gone to the filter:\n%s", body)
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
			{Name: "web", Key: "ctrl-shift-i", Window: &revier.Realization{
				Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}}},
		}},
	}
	projects, err := core.Prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, cmd := press(m, "ctrl+shift+i") // "plain" is the first row
	if cmd != nil {
		t.Fatal("a target the project does not declare should run nothing")
	}
	if footer := lines(m)[len(lines(m))-1]; !strings.Contains(footer, "no web target") {
		t.Errorf("footer = %q, want it to name the missing target", footer)
	}
}
