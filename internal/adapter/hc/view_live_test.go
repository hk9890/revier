//go:build live

// Layer L4: the core against a real hc core of the test's own and the fake
// view that draws it in windows of its own, as kitty does over hc. A real kitty
// view needs a display, which no test may reach; this proves what hc gives the
// view: tabs, their order, and terminals that outlive it.
package hc_test

import (
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// workspace is a project as the user's config.toml declares one: home of
// the tabs tickets and agent, agent active, an agent tab of an agent and a
// shell, and notes, a tab inside home that home does not list.
func workspace(t *testing.T) core.Project {
	t.Helper()
	sleep := []string{"sh", "-c", "exec sleep 60"}
	return core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Match: revier.Match{Title: "^session:demo$"},
			Tabs: []revier.TargetName{"tickets", "agent"}, Active: "agent"}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home", Launch: sleep}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: sleep}, {Kind: revier.PanelShell},
		}}},
		{Name: "notes", Runtime: &revier.Realization{Inside: "home", Launch: sleep}},
	}})
}

func ids(panels []revier.Panel) []revier.PanelID {
	var out []revier.PanelID
	for _, p := range panels {
		out = append(out, p.ID)
	}
	return out
}

func tabCount(panels []revier.Panel) int {
	tabs := map[string]bool{}
	for _, p := range panels {
		tabs[p.Tab] = true
	}
	return len(tabs)
}

// The workspace opens on hc as tabs in its order, the view draws them all
// and makes the active tab current, a tab inside home opens after them, a
// second press on it goes home, and two agent tabs stay two.
func TestTheCoreOpensTabsOnHcAndTheViewDrawsThem(t *testing.T) {
	h, c := world(t), ctx(t)
	view := hosttest.NewView("kitty")
	cr := &core.Core{Runtime: h, View: view}
	p := workspace(t)

	res, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("press home: %v", err)
	}
	panels := listing(t, h, res.Ref)
	if len(panels) != 3 || tabCount(panels) != 2 || panels[0].Vars[core.PanelTargetVar] != "tickets" || panels[1].Vars[core.PanelTargetVar] != "agent" {
		t.Fatalf("panels = %+v; want tickets, then the agent tab of two", panels)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, ids(panels)) || len(view.Opened) != 1 {
		t.Errorf("view shows %v in %d windows, want every panel %v in one", shown, len(view.Opened), ids(panels))
	}
	if cur, _ := view.FocusedPanel(c, res.Ref); cur != panels[1].ID {
		t.Errorf("current panel %s, want the active agent tab's %s", cur, panels[1].ID)
	}

	if _, err := press(c, cr, p, "notes"); err != nil {
		t.Fatalf("press notes: %v", err)
	}
	panels = listing(t, h, res.Ref)
	notes := panels[len(panels)-1]
	if len(panels) != 4 || tabCount(panels) != 3 || notes.Vars[core.PanelTargetVar] != "notes" {
		t.Fatalf("panels = %+v; want notes as a last tab", panels)
	}
	if cur, _ := view.FocusedPanel(c, res.Ref); cur != notes.ID {
		t.Errorf("current panel %s, want notes' %s", cur, notes.ID)
	}
	if _, err := press(c, cr, p, "notes"); err != nil {
		t.Fatalf("second press notes: %v", err)
	}
	if cur, _ := view.FocusedPanel(c, res.Ref); cur != panels[1].ID {
		t.Errorf("after the second press the current panel is %s, want home's own %s", cur, panels[1].ID)
	}

	w, err := cr.AgentWorkspace(c, p, "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	for range 2 {
		if _, err := cr.NewAgent(c, w, core.Resume{}); err != nil {
			t.Fatalf("NewAgent: %v", err)
		}
	}
	panels = listing(t, h, res.Ref)
	if len(panels) != 8 || tabCount(panels) != 5 {
		t.Fatalf("panels = %+v; want two agent tabs of two more", panels)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, ids(panels)) || len(view.Opened) != 1 {
		t.Errorf("view shows %v in %d windows, want every panel in the one window", shown, len(view.Opened))
	}
	if cur, _ := view.FocusedPanel(c, res.Ref); cur != panels[6].ID {
		t.Errorf("current panel %s, want the last agent's %s", cur, panels[6].ID)
	}
}

// A view closed by hand leaves every terminal running: the next press, and a
// restore, open the view over the same terminals and start none. A shutdown
// then ends them through hc.
func TestAClosedViewEndsNoTerminalAndComesBackOverThem(t *testing.T) {
	h, c := world(t), ctx(t)
	view := hosttest.NewView("kitty")
	cr := &core.Core{Runtime: h, View: view}
	p := workspace(t)
	projects := []core.Project{p}

	res, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("press home: %v", err)
	}
	before := ids(listing(t, h, res.Ref))
	view.CloseWindow(res.Ref)

	if _, err := press(c, cr, p, "home"); err != nil {
		t.Fatalf("press home again: %v", err)
	}
	if got := ids(listing(t, h, res.Ref)); !slices.Equal(got, before) {
		t.Errorf("terminals %v, want the same %v: the press starts nothing", got, before)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, before) || len(view.Opened) != 2 {
		t.Errorf("view shows %v in %d windows, want a second window over %v", shown, len(view.Opened), before)
	}

	view.CloseWindow(res.Ref)
	report, err := cr.Survey(c, projects)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s := session.Session{Projects: []session.Project{{Name: "demo", Targets: []session.Target{{Name: "home"}}}}}
	out, back := cr.Restore(c, s, report, projects, core.RestoreOpts{})
	if back != nil || len(out) != 1 || out[0].Action != core.RestoreRunning {
		t.Fatalf("restore = %+v, %v; want home running", out, back)
	}
	if got := ids(listing(t, h, res.Ref)); !slices.Equal(got, before) || len(view.Opened) != 3 {
		t.Errorf("terminals %v, view windows %d; want the same terminals and the view again", got, len(view.Opened))
	}

	report, err = cr.Survey(c, projects)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	plan := cr.ShutdownPlan(report, "", core.ShutdownAll)
	if _, err := cr.Shutdown(c, plan, 5*time.Second, core.ShutdownOpts{Projects: projects, Force: true}); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got, err := h.Instances(c); err != nil || len(got) != 0 {
		t.Errorf("after the shutdown = %+v, %v; want no terminal", got, err)
	}
}
