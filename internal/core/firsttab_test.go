package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// firstTabProject is a workspace of an agent and a shell, with tickets as the
// first tab of it.
func firstTabProject() revier.Project {
	p := tabProject()
	p.Targets[0].Runtime.Panels = append(p.Targets[0].Runtime.Panels, revier.PanelSpec{Kind: revier.PanelShell})
	p.Targets[1].Runtime.First = true
	return p
}

// The order of the tabs is the order they are opened in: the first tab opens
// the instance, and the workspace's own panels are the tab after it.
func TestAWorkspaceOpensWithItsFirstTabAndItsOwnPanelsAfterIt(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, firstTabProject()), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("opened %d instances, want one", len(rt.Opened))
	}
	opened := rt.Opened[0]
	if opened.Name != "session:revier" || !slices.Equal(opened.Launch, []string{"taskmgr-ui"}) || len(opened.Panels) != 0 {
		t.Errorf("opened = %+v, want the workspace's name on the tab's launch", opened)
	}
	if opened.Vars[core.PanelTargetVar] != "tickets" || opened.Vars[core.PanelHomeVar] != "" {
		t.Errorf("first panel vars = %v, want the tickets mark alone", opened.Vars)
	}
	if len(rt.Tabs) != 1 {
		t.Fatalf("tabs = %d, want the workspace's own panels in one", len(rt.Tabs))
	}
	own := rt.Tabs[0]
	if len(own.Real.Panels) != 2 || own.Real.Panels[0].Kind != revier.PanelAgent || own.Real.Panels[1].Kind != revier.PanelShell {
		t.Errorf("second tab = %+v, want the agent and the shell", own.Real.Panels)
	}
	if own.Vars[core.PanelHomeVar] != "home" || own.Vars[core.PanelTargetVar] != "" {
		t.Errorf("own panel vars = %v, want the home mark alone", own.Vars)
	}
	if len(rt.PanelFocuses) == 0 || rt.PanelFocuses[len(rt.PanelFocuses)-1] != own.Panel {
		t.Errorf("panel focuses = %v, want the agent panel %s last", rt.PanelFocuses, own.Panel)
	}
	if res.Target != "home" || res.FirstTab != "tickets" || !res.Launched || res.Ref.IsZero() {
		t.Errorf("result = %+v, want a launch of home with tickets first", res)
	}
}

// The tab is in the workspace already, so its key finds it there.
func TestAPressOnAFirstTabOpensNoSecondOne(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, firstTabProject())

	if _, err := press(context.Background(), c, p, "home"); err != nil {
		t.Fatalf("Go home: %v", err)
	}
	res, err := press(context.Background(), c, p, "tickets")
	if err != nil {
		t.Fatalf("Go tickets: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Errorf("opened %d instances and %d tabs, want the one workspace and its own tab", len(rt.Opened), len(rt.Tabs))
	}
	if got, want := rt.PanelFocuses[len(rt.PanelFocuses)-1], rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the first tab's %s", got, want)
	}
	if res.Tab != "tickets" || res.Launched {
		t.Errorf("result = %+v, want the tickets tab reached and nothing launched", res)
	}
}

// A press on the tab's key with the workspace closed opens the workspace,
// which brings the tab: the press focuses it and opens no other.
func TestAPressOnAFirstTabOfAClosedWorkspaceOpensTheWorkspace(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, firstTabProject()), "tickets")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 || rt.Tabs[0].Vars[core.PanelHomeVar] != "home" {
		t.Fatalf("opened %d instances, tabs %+v; want the workspace and its own tab alone", len(rt.Opened), rt.Tabs)
	}
	if got, want := rt.PanelFocuses[len(rt.PanelFocuses)-1], rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the first tab's %s", got, want)
	}
	if res.Target != "home" || res.Tab != "tickets" || !res.Launched {
		t.Errorf("result = %+v, want a launch landing on the tickets tab of home", res)
	}
}

// The second press on the tab goes home, which is the workspace's own panel
// in the second tab and not the first panel of the instance.
func TestAReturnHomeFromAFirstTabLandsInTheWorkspacesOwnPanel(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, firstTabProject())

	for range 2 {
		if _, err := press(context.Background(), c, p, "tickets"); err != nil {
			t.Fatalf("Go: %v", err)
		}
	}
	if got, want := rt.PanelFocuses[len(rt.PanelFocuses)-1], rt.Tabs[0].Panel; got != want {
		t.Errorf("focused panel %s, want the agent panel %s", got, want)
	}
}

// The workspace is left open with its first tab alone, and reported with the
// failure, so the next press raises it and opens no second one.
func TestAWorkspaceWhoseOwnPanelsDoNotOpenIsKeptAndReported(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.OpenTabErr = errors.New("kitty went away")
	c := &core.Core{Runtime: rt}
	p := prepared(t, firstTabProject())

	res, err := pressResuming(context.Background(), c, p, "home", []core.Resume{{Harness: "claude", Session: "abc"}})
	if err == nil {
		t.Fatal("Go succeeded, want the failure of the workspace's own panels")
	}
	for _, want := range []string{"kitty went away", "home", "first tab tickets"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	if res.Target != "home" || res.FirstTab != "tickets" || res.Ref.IsZero() || !res.Launched {
		t.Errorf("result = %+v, want the opened workspace reported", res)
	}
	if !slices.Equal(res.Agents, []core.AgentOutcome{core.AgentNotAdded}) {
		t.Errorf("agents = %v, want the recorded agent named as not added", res.Agents)
	}
	if len(rt.Closed) != 0 || len(rt.ClosedPanels) != 0 {
		t.Errorf("closed %v and panels %v, want the workspace kept", rt.Closed, rt.ClosedPanels)
	}

	rt.OpenTabErr = nil
	if _, err := press(context.Background(), c, p, "home"); err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Errorf("opened %d instances, want the kept one raised", len(rt.Opened))
	}
}

// A runtime with no tabs opens the workspace as it is declared.
func TestARuntimeWithoutTabsOpensTheWorkspaceWithoutItsFirstTab(t *testing.T) {
	rt := hosttest.NewRuntime("plain")
	c := &core.Core{Runtime: bareRuntime{rt}}

	res, err := press(context.Background(), c, prepared(t, firstTabProject()), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Opened[0].Panels) != 2 || rt.Opened[0].Vars[core.PanelHomeVar] != "home" {
		t.Errorf("opened = %+v, want the declared panels under the home mark", rt.Opened)
	}
	if res.FirstTab != "" {
		t.Errorf("first tab = %q, want none", res.FirstTab)
	}
}

// A first tab comes back with its workspace, so a save records no step for it.
func TestASaveRecordsNoStepForAFirstTab(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, firstTabProject())
	if _, err := press(context.Background(), c, p, "home"); err != nil {
		t.Fatalf("Go: %v", err)
	}

	r, err := c.Survey(context.Background(), []core.Project{p})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), r, "revier")
	if len(s.Projects) != 1 || len(s.Projects[0].Targets) != 1 || s.Projects[0].Targets[0].Name != "home" {
		t.Errorf("session = %+v, want home alone", s.Projects)
	}
}
