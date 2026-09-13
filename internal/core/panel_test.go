package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// tabProject has a workspace and a ticket viewer declared as a tab of it.
func tabProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Match: revier.Match{Title: "^session:revier$"},
			Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}}}},
		{Name: "tickets", Key: "ctrl-shift-t", Runtime: &revier.Realization{
			Inside: "home", Launch: []string{"taskmgr-ui"}}},
	}}
}

// tabHosts is a kitty-like runtime holding the workspace, and a window host
// that lists its OS window.
func tabHosts(t *testing.T) (*hosttest.FakeRuntime, *hosttest.Fake, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent})
	wm := hosttest.New("wm")
	osw := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	return rt, wm, osw
}

func TestATabIsOpenedInItsTargetAndRaised(t *testing.T) {
	rt, wm, osw := tabHosts(t)
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, tabProject()), "tickets", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Vars[core.PanelTargetVar] != "tickets" {
		t.Fatalf("tabs = %+v, want one, marked as the tickets target's", rt.Tabs)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("opened %d instances, want none: the workspace was there", len(rt.Opened))
	}
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != rt.Tabs[0].Panel {
		t.Errorf("panel focuses = %v, want the new tab", rt.PanelFocuses)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != osw {
		t.Errorf("window focuses = %v, want the workspace raised", wm.Focuses)
	}
	// The caller binds Result.Target to Result.Ref. The ref is the workspace,
	// so the target is too: tickets bound to it would raise the workspace
	// once inside is removed.
	if !res.Launched || res.Target != "home" {
		t.Errorf("result = %+v, want a launch landing on home's instance", res)
	}
}

// The point of an identity: the second press finds the tab and opens none.
func TestASecondPressFocusesTheTabItOpened(t *testing.T) {
	rt, wm, _ := tabHosts(t)
	c := &core.Core{Runtime: rt, Window: wm}
	p := prepared(t, tabProject())

	for range 2 {
		if _, err := c.Go(context.Background(), p, "tickets", nil); err != nil {
			t.Fatalf("Go: %v", err)
		}
		wm.SetFocus(revier.TargetRef{}) // the user looked away
	}
	if len(rt.Tabs) != 1 {
		t.Errorf("tabs = %d, want 1: a tab that is open is focused, not opened again", len(rt.Tabs))
	}
	if len(rt.PanelFocuses) != 2 {
		t.Errorf("panel focuses = %v, want the tab focused on both presses", rt.PanelFocuses)
	}
}

func TestATabOpensItsTargetFirst(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}

	if _, err := c.Go(context.Background(), prepared(t, tabProject()), "tickets", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "session:revier" {
		t.Fatalf("opened = %+v, want the workspace", rt.Opened)
	}
	if len(rt.Tabs) != 1 {
		t.Errorf("tabs = %d, want the tab opened in the new workspace", len(rt.Tabs))
	}
}

// Toggle-back: the tab is current in the focused window, so the press goes
// home, which here is the same window's own first panel.
func TestAPressOnTheCurrentTabReturnsToHome(t *testing.T) {
	rt, wm, osw := tabHosts(t)
	c := &core.Core{Runtime: rt, Window: wm}
	p := prepared(t, tabProject())
	if _, err := c.Go(context.Background(), p, "tickets", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	wm.SetFocus(osw)

	res, err := c.Go(context.Background(), p, "tickets", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Target != "home" {
		t.Errorf("target = %s, want home", res.Target)
	}
	if last := rt.PanelFocuses[len(rt.PanelFocuses)-1]; last != "1" {
		t.Errorf("last panel focus = %s, want the workspace's own panel", last)
	}
	if len(rt.Tabs) != 1 {
		t.Errorf("tabs = %d, want 1", len(rt.Tabs))
	}
}

// tablessRuntime is a runtime with no PanelOpener.
type tablessRuntime struct{ *hosttest.Fake }

func (tablessRuntime) Capabilities() revier.Capabilities { return revier.Capabilities{Layout: true} }

func TestATabOnARuntimeWithoutTabsIsAnError(t *testing.T) {
	rt := tablessRuntime{hosttest.New("tmux")}
	rt.Add("session:revier", "")
	c := &core.Core{Runtime: rt}
	p := prepared(t, tabProject())

	_, err := c.Go(context.Background(), p, "tickets", nil)
	if !errors.Is(err, core.ErrNoTabs) {
		t.Fatalf("err = %v, want ErrNoTabs", err)
	}
	for _, want := range []string{`"tickets"`, `"home"`, "tmux"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if len(rt.Opened) != 0 {
		t.Errorf("opened %d, want none: a window the user did not ask for is not the fallback", len(rt.Opened))
	}

	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if tv := report.Views[0].Targets[1]; tv.Available {
		t.Errorf("tickets = %+v, want unavailable", tv)
	}
}

// The survey reports an open tab at the instance that holds it, and the
// instance's agents once.
func TestTheSurveyReportsAnOpenTabOnce(t *testing.T) {
	rt, wm, _ := tabHosts(t)
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle},
	}}}
	p := prepared(t, tabProject())

	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if tv := report.Views[0].Targets[1]; !tv.Available || !tv.Ref.IsZero() {
		t.Errorf("tickets before = %+v, want available and not open", tv)
	}

	if _, err := c.Go(context.Background(), p, "tickets", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	report, err = c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	if v.Targets[1].Ref != v.Home {
		t.Errorf("tickets ref = %v, want the workspace %v", v.Targets[1].Ref, v.Home)
	}
	if len(v.Agents) != 2 {
		t.Errorf("agents = %d, want 2, each panel once", len(v.Agents))
	}
}
