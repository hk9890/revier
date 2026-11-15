package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// viewedProject is a workspace home of the tabs tickets and agent, with agent
// active, as the user's config.toml describes one: a tickets tab running a
// tool, and an agent tab of an agent and a shell.
func viewedProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Match: revier.Match{Title: "^session:revier$"},
			Tabs: []revier.TargetName{"tickets", "agent"}, Active: "agent"}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home", Launch: []string{"taskmgr-ui"}}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude"}}, {Kind: revier.PanelShell},
		}}},
	}}
}

// viewed is a runtime that draws nothing, as hc, and a view that draws it in
// windows of its own, as kitty over hc (decisions.md D180).
func viewed() (*core.Core, *hosttest.FakeRuntime, *hosttest.FakeView) {
	rt := hosttest.NewRuntime("hc")
	view := hosttest.NewView("kitty")
	return &core.Core{Runtime: rt, View: view}, rt, view
}

// panelIDs is the ids of the instance's panels, in the runtime's order.
func panelIDs(t *testing.T, rt *hosttest.FakeRuntime, ref revier.TargetRef) []revier.PanelID {
	t.Helper()
	instances, err := rt.Instances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range instances {
		if in.Ref == ref {
			var ids []revier.PanelID
			for _, p := range in.Panels {
				ids = append(ids, p.ID)
			}
			return ids
		}
	}
	t.Fatalf("no instance %v", ref)
	return nil
}

// The first press starts every tab in the runtime, then has the view open one
// window over all of them, and makes the active tab current there.
func TestAPressOpensOneViewWindowOverEveryTab(t *testing.T) {
	c, rt, view := viewed()

	res, err := press(t.Context(), c, prepared(t, viewedProject()), "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Fatalf("runtime opened %d, tabs %d; want the instance and the agent tab", len(rt.Opened), len(rt.Tabs))
	}
	if !slices.Equal(view.Opened, []revier.TargetRef{res.Ref}) {
		t.Errorf("view opened %v, want one window for %v", view.Opened, res.Ref)
	}
	shown, _ := view.Showing(res.Ref)
	if !slices.Equal(shown, panelIDs(t, rt, res.Ref)) {
		t.Errorf("view shows %v, want every panel %v", shown, panelIDs(t, rt, res.Ref))
	}
	if cur, _ := view.FocusedPanel(t.Context(), res.Ref); cur != rt.Tabs[0].Panel {
		t.Errorf("current panel %s, want the agent tab's %s", cur, rt.Tabs[0].Panel)
	}
	if focused, _ := view.Focused(t.Context()); focused != res.Ref {
		t.Errorf("view focused %v, want %v", focused, res.Ref)
	}
}

// A view closed by hand ends no program: the next press finds the instance
// running, opens a window over its tabs, and starts nothing (decisions.md
// D181).
func TestAPressOnAnInstanceNoViewShowsOpensAViewAndStartsNothing(t *testing.T) {
	c, rt, view := viewed()
	p := prepared(t, viewedProject())
	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	view.CloseWindow(res.Ref)

	again, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("second press: %v", err)
	}
	if again.Ref != res.Ref || len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Errorf("ref %v, runtime opened %d, tabs %d; want %v again and nothing started", again.Ref, len(rt.Opened), len(rt.Tabs), res.Ref)
	}
	if len(view.Opened) != 2 {
		t.Errorf("view opened %v, want a second window", view.Opened)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, panelIDs(t, rt, res.Ref)) {
		t.Errorf("view shows %v, want every live panel", shown)
	}
	if focused, _ := view.Focused(t.Context()); focused != res.Ref {
		t.Errorf("view focused %v, want %v", focused, res.Ref)
	}
}

// A restore that finds a workspace running, as after kitty closed, has the
// view show it again over its live panels and starts nothing; a second
// restore opens no second window.
func TestARestoreShowsARunningWorkspaceNoViewShows(t *testing.T) {
	c, rt, view := viewed()
	projects := []core.Project{prepared(t, viewedProject())}
	res, err := press(t.Context(), c, projects[0], "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	view.CloseWindow(res.Ref)
	c.Ledger = &ledger{}
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "home"}}}}}

	for range 2 {
		out, back := c.Restore(t.Context(), s, survey(t, c, projects, nil), projects, core.RestoreOpts{})
		if back != nil || len(out) != 1 || out[0].Action != core.RestoreRunning {
			t.Fatalf("restore = %+v, %v; want home running", out, back)
		}
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 || len(view.Opened) != 2 {
		t.Errorf("runtime opened %d, tabs %d, view windows %v; want nothing started and one window again", len(rt.Opened), len(rt.Tabs), view.Opened)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, panelIDs(t, rt, res.Ref)) {
		t.Errorf("view shows %v, want every live panel", shown)
	}
}

// A press on a tab whose instance no view shows opens the view too, and
// lands on that tab.
func TestAPressOnATabOfAnUnshownInstanceShowsItAndFocusesTheTab(t *testing.T) {
	c, rt, view := viewed()
	p := prepared(t, viewedProject())
	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	view.CloseWindow(res.Ref)

	if _, err := press(t.Context(), c, p, "tickets"); err != nil {
		t.Fatalf("press tickets: %v", err)
	}
	if len(view.Opened) != 2 || len(rt.Tabs) != 1 {
		t.Errorf("view opened %v, runtime tabs %d; want the view again and no tab started", view.Opened, len(rt.Tabs))
	}
	if cur, _ := view.FocusedPanel(t.Context(), res.Ref); cur != panelIDs(t, rt, res.Ref)[0] {
		t.Errorf("current panel %s, want the tickets tab's", cur)
	}
}

// A second press on a tab the view has current goes home: the view reports
// the focus, as kitty does over hc.
func TestASecondPressOnAViewedTabGoesHome(t *testing.T) {
	c, rt, view := viewed()
	p := prepared(t, viewedProject())
	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	tickets := panelIDs(t, rt, res.Ref)[0]
	if _, err := press(t.Context(), c, p, "tickets"); err != nil {
		t.Fatalf("press tickets: %v", err)
	}
	if cur, _ := view.FocusedPanel(t.Context(), res.Ref); cur != tickets {
		t.Fatalf("current panel %s, want tickets' %s", cur, tickets)
	}
	if _, err := press(t.Context(), c, p, "tickets"); err != nil {
		t.Fatalf("second press tickets: %v", err)
	}
	if cur, _ := view.FocusedPanel(t.Context(), res.Ref); cur != rt.Tabs[0].Panel {
		t.Errorf("current panel %s, want home's own, the agent tab's %s", cur, rt.Tabs[0].Panel)
	}
}

// A tab a key opens in a workspace the view shows is drawn in its window and
// made current; one a script opens with no view window opens no window,
// since a script's tab is gone to by nobody (decisions.md D118).
func TestAnAgentTabIsDrawnWhereTheViewShowsTheWorkspaceAlone(t *testing.T) {
	c, rt, view := viewed()
	p := prepared(t, viewedProject())
	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if _, err := newAgent(t, c, p, "home", core.Resume{}); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	added := rt.Tabs[len(rt.Tabs)-1].Panel
	if shown, _ := view.Showing(res.Ref); !slices.Contains(shown, added) || len(view.Opened) != 1 {
		t.Errorf("view shows %v in %d windows, want the new tab's %s in the one window", shown, len(view.Opened), added)
	}
	if cur, _ := view.FocusedPanel(t.Context(), res.Ref); cur != added {
		t.Errorf("current panel %s, want the new agent's %s", cur, added)
	}

	view.CloseWindow(res.Ref)
	w, err := c.AgentWorkspace(t.Context(), p, "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	if _, _, err := c.AddAgent(t.Context(), w, core.Resume{}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if _, ok := view.Showing(res.Ref); ok || len(view.Opened) != 1 {
		t.Errorf("view opened %v, want no window for a script's tab", view.Opened)
	}

	if _, err := newAgent(t, c, p, "home", core.Resume{}); err != nil {
		t.Fatalf("NewAgent with no window: %v", err)
	}
	if shown, _ := view.Showing(res.Ref); !slices.Equal(shown, panelIDs(t, rt, res.Ref)) || len(view.Opened) != 2 {
		t.Errorf("view shows %v in %d windows, want a window again over every panel", shown, len(view.Opened))
	}
}

// A key passes the view's window id, which the view maps to the runtime's
// panel behind it: the tab opens in that panel's workspace, in the panel's
// directory for a shell. A window of the view's that shows no panel gets a
// plain tab of the view's own beside it, which no instance holds
// (decisions.md D183).
func TestAKeyInAViewWindowReachesTheRuntimePanelBehindIt(t *testing.T) {
	c, rt, view := viewed()
	dir := t.TempDir()
	proj := viewedProject()
	proj.Path = dir
	p := prepared(t, proj)
	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	shell := panelIDs(t, rt, res.Ref)[2]
	rt.Replace(revier.Panel{ID: shell, Kind: revier.PanelShell, Dir: dir, Tab: "tab1"})
	view.Keys = map[revier.PanelID]revier.PanelID{"7": shell}
	projects := []core.Project{p}

	tabs := len(rt.Tabs)
	if _, err := c.PlaceTab(t.Context(), projects, core.TabRequest{Kind: revier.PanelShell, Panel: "7"}); err != nil {
		t.Fatalf("PlaceTab: %v", err)
	}
	if len(rt.Tabs) != tabs+1 || rt.Tabs[tabs].Ref != res.Ref || rt.Tabs[tabs].Real.Panels[0].Dir != dir {
		t.Errorf("tabs = %+v, want a shell tab in %v started in %s", rt.Tabs[tabs:], res.Ref, dir)
	}

	if _, err := c.PlaceTab(t.Context(), projects, core.TabRequest{Kind: revier.PanelShell, Panel: "8", Dir: dir, Outside: core.OutsideKey{Outside: core.OutsideTab}}); err != nil {
		t.Fatalf("PlaceTab outside: %v", err)
	}
	if len(rt.Tabs) != tabs+1 {
		t.Errorf("runtime tabs = %d, want no tab of the runtime's outside a workspace", len(rt.Tabs))
	}
	if len(view.Beside) != 1 || view.Beside[0].Panel != "8" || view.Beside[0].Spec.Kind != revier.PanelShell || view.Beside[0].Spec.Dir != dir {
		t.Errorf("beside = %+v, want a plain shell beside window 8 in %s", view.Beside, dir)
	}
}

// osWindowView is FakeView whose windows are OS windows the window host wm
// lists by the instance's title, as the kitty view's are under GNOME.
type osWindowView struct {
	*hosttest.FakeView
	wm      *hosttest.Fake
	windows map[string]revier.TargetRef
}

func (v *osWindowView) Capabilities() revier.Capabilities {
	return revier.Capabilities{OSWindows: true}
}

func (v *osWindowView) Show(ctx context.Context, inst revier.Instance, open bool) (bool, error) {
	opened, err := v.FakeView.Show(ctx, inst, open)
	if opened {
		v.windows[inst.Ref.ID] = v.wm.AddInstance(revier.Instance{Title: inst.Title, Class: "kitty"})
	}
	return opened, err
}

func (v *osWindowView) closeWindow(ref revier.TargetRef) {
	v.CloseWindow(ref)
	v.wm.Remove(v.windows[ref.ID])
	delete(v.windows, ref.ID)
}

// A view window that opens again over a live workspace is a window revier
// starts, so it goes where the home target places a launch's, whether a
// press on home, a press on a tab or a restore opened it (decisions.md D24,
// D186).
func TestAViewWindowOpenedAgainGoesWhereALaunchGoes(t *testing.T) {
	proj := viewedProject()
	proj.Targets[0].Runtime.Place = "right top 75% 100%"
	want := []string{"right", "top", "75%", "100%"}
	for _, again := range []string{"home", "agent", "restore"} {
		t.Run(again, func(t *testing.T) {
			rt, wm := hosttest.NewRuntime("hc"), hosttest.New("wm")
			view := &osWindowView{FakeView: hosttest.NewView("kitty"), wm: wm, windows: map[string]revier.TargetRef{}}
			c := &core.Core{Runtime: rt, View: view, Window: wm}
			projects := []core.Project{prepared(t, proj)}
			res, err := press(t.Context(), c, projects[0], "home")
			if err != nil {
				t.Fatalf("press: %v", err)
			}
			if got := wm.Placements[view.windows[res.Ref.ID].ID]; !slices.Equal(got, want) {
				t.Fatalf("launch placed %v, want %v", got, want)
			}
			view.closeWindow(res.Ref)

			if again == "restore" {
				c.Ledger = &ledger{}
				s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "home"}}}}}
				if _, back := c.Restore(t.Context(), s, survey(t, c, projects, nil), projects, core.RestoreOpts{}); back != nil {
					t.Fatalf("restore: %v", back)
				}
			} else if _, err := press(t.Context(), c, projects[0], revier.TargetName(again)); err != nil {
				t.Fatalf("press %s: %v", again, err)
			}
			if got := wm.Placements[view.windows[res.Ref.ID].ID]; !slices.Equal(got, want) {
				t.Errorf("window opened again placed %v, want %v", got, want)
			}
		})
	}
}
