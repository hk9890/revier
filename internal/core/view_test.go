package core_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// splitProject is a workspace home and a target beside it, both on the
// runtime, and a tab inside home.
func splitProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home", Launch: []string{"taskmgr-ui"}}},
	}}
}

// The view focuses and reports the focus, and the runtime starts and lists:
// a press reaches the view with the runtime's ref, and asks the runtime to
// focus nothing (decisions.md D176).
func TestAPressFocusesThroughTheViewWithTheRuntimesRef(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	diff := rt.Add("diff:revier", "kitty")
	view := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, View: view}

	res, err := press(t.Context(), c, prepared(t, splitProject()), "diff")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Ref != diff || len(view.Focuses) != 1 || view.Focuses[0] != diff {
		t.Errorf("ref %v, view focused %v; want %v focused through the view", res.Ref, view.Focuses, diff)
	}
	if len(rt.Focuses) != 0 {
		t.Errorf("runtime focused %v, want nothing: focus is the view's", rt.Focuses)
	}
}

// The second press goes home when the view says the target has the focus,
// whatever the runtime would say.
func TestToggleBackReadsTheViewsFocus(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	home := rt.Add("session:revier", "kitty")
	diff := rt.Add("diff:revier", "kitty")
	view := hosttest.NewRuntime("rt")
	view.SetFocus(diff)
	c := &core.Core{Runtime: rt, View: view}

	res, err := press(t.Context(), c, prepared(t, splitProject()), "diff")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Ref != home {
		t.Errorf("landed on %v, want home %v", res.Ref, home)
	}
}

// With no view the runtime's instance is reached and nothing is focused, and
// a second press does not go home: nothing reports where the user is. This
// is hc's setup with no display (decisions.md D178).
func TestWithNoViewAPressFocusesNothingAndDoesNotToggle(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	diff := rt.Add("diff:revier", "kitty")
	rt.SetFocus(diff)
	c := &core.Core{Runtime: rt}

	res, err := press(t.Context(), c, prepared(t, splitProject()), "diff")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Ref != diff || len(rt.Focuses) != 0 {
		t.Errorf("ref %v, runtime focused %v; want %v and no focus", res.Ref, rt.Focuses, diff)
	}
	if focused, ok, err := c.ProjectOfFocused(t.Context(), []core.Project{prepared(t, splitProject())}); err != nil || ok {
		t.Errorf("ProjectOfFocused = %s, %v, %v; want none with no view", focused.Name, ok, err)
	}
}

// A tab opens in the runtime and is made current by the view.
func TestATabOpensInTheRuntimeAndIsFocusedByTheView(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	home := rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool})
	view := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, View: view}

	if _, err := press(t.Context(), c, prepared(t, splitProject()), "tickets"); err != nil {
		t.Fatalf("press: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != home {
		t.Fatalf("runtime tabs = %+v, want one in %v", rt.Tabs, home)
	}
	if len(view.PanelFocuses) != 1 || view.PanelFocuses[0] != rt.Tabs[0].Panel || len(rt.PanelFocuses) != 0 {
		t.Errorf("view focused %v, runtime focused %v; want the tab's panel %s through the view", view.PanelFocuses, rt.PanelFocuses, rt.Tabs[0].Panel)
	}
}

// With no view a tab still opens, and stays where it is.
func TestWithNoViewATabOpensAndIsNotFocused(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt}

	if _, err := press(context.Background(), c, prepared(t, splitProject()), "tickets"); err != nil {
		t.Fatalf("press: %v", err)
	}
	if len(rt.Tabs) != 1 || len(rt.PanelFocuses) != 0 {
		t.Errorf("tabs %+v, panel focuses %v; want one tab and no focus", rt.Tabs, rt.PanelFocuses)
	}
}

// OSWindows is the view's to claim: a runtime whose view does not claim it is
// not raised through the window host, whatever the runtime itself says.
func TestOSWindowsIsTheViewsClaim(t *testing.T) {
	rt, wm, _, _ := osWindowHosts()
	view := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, View: view, Window: wm}

	if _, err := press(t.Context(), c, prepared(t, project()), "home"); err != nil {
		t.Fatalf("press: %v", err)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window host focused %v, want nothing: the view claims no OS windows", wm.Focuses)
	}
	view.SetCapabilities(revier.Capabilities{OSWindows: true})
	if _, err := press(t.Context(), c, prepared(t, project()), "home"); err != nil {
		t.Fatalf("press: %v", err)
	}
	if len(wm.Focuses) != 1 {
		t.Errorf("window host focused %v, want the OS window raised once the view claims it", wm.Focuses)
	}
}
