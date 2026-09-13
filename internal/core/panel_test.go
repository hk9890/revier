package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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

// A tab declared before the target it is inside is recorded after it. A
// restore walks the file in order, and reaching the tab first would open the
// workspace for it with none of its agents resumed.
func TestASessionRecordsATabAfterItsInstance(t *testing.T) {
	proj := tabProject()
	proj.Targets[0], proj.Targets[1] = proj.Targets[1], proj.Targets[0]
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:revier", "kitty",
		agent("1", "abc-123", ""),
		revier.Panel{ID: "2", Kind: revier.PanelTool, Vars: map[string]string{core.PanelTargetVar: "tickets"}},
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, proj)}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), report, "revier")
	targets := s.Projects[0].Targets
	if len(targets) != 2 || targets[0].Name != "home" || targets[1].Name != "tickets" {
		t.Fatalf("targets = %+v, want home, then the tickets tab", targets)
	}
	if len(targets[0].Agents) != 1 || targets[0].Agents[0].Session != "abc-123" || len(targets[1].Agents) != 0 {
		t.Errorf("targets = %+v, want the one agent under home", targets)
	}
}

// A tab named in an agent address is its own panel, not the instance's.
func TestAnAgentAddressedByATabIsTheTabsPanel(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:revier", "kitty",
		agentPanel("1", "idle"),
		revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "busy", Command: []string{"agent"},
			Vars: map[string]string{core.PanelTargetVar: "tickets"}},
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{titleProbe{}}}

	a, err := c.Agent(context.Background(), prepared(t, tabProject()), "tickets", nil)
	if err != nil {
		t.Fatalf("Agent(tickets): %v", err)
	}
	if a.Panel.ID != "2" || a.State.Status != revier.StatusRunning {
		t.Errorf("agent = %+v, want the tab's panel 2", a)
	}
}

// A tab has no match of its own, and must not be read as one that matches
// everything. A stray window is still a stray, and a focused window no target
// declares still belongs to no project.
func TestATabMatchesNoInstanceOnItsOwn(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	stray := rt.Add("htop", "kitty")
	rt.SetFocus(stray)
	c := &core.Core{Runtime: rt}
	p := prepared(t, tabProject())

	if _, found, err := c.ProjectOfFocused(context.Background(), []core.Project{p}); err != nil || found {
		t.Errorf("ProjectOfFocused = %v, %v; want no project for a window no target declares", found, err)
	}
	now := time.Now()
	after, _ := rt.Instances(context.Background())
	if _, ok := c.Claim(nil, after, core.Launch{Project: p, At: now}, now, []core.Project{p}); !ok {
		t.Error("the stray was not claimed: a tab was taken to declare it")
	}
}

// An agent in a tab is recorded under the tab, not under the instance that
// holds it, and restoring the tab starts it on its conversation in the tab.
// Recorded under the instance, the restore opened it twice: once as an extra
// agent of the workspace, and once as the tab.
func TestATabsAgentIsRecordedAndRestoredOnce(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	tabAgent := agent("2", "tab-9", "")
	tabAgent.Vars[core.PanelTargetVar] = "tickets"
	rt.Add("session:revier", "kitty", agent("1", "abc-123", ""), tabAgent)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, tabProject())

	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), report, "revier")
	targets := s.Projects[0].Targets
	if len(targets) != 2 || len(targets[0].Agents) != 1 || targets[0].Agents[0].Session != "abc-123" {
		t.Fatalf("targets = %+v, want home with its own agent only", targets)
	}
	if len(targets[1].Agents) != 1 || targets[1].Agents[0].Session != "tab-9" {
		t.Fatalf("tickets = %+v, want the tab's agent", targets[1])
	}

	// A restore on a machine where the workspace is up and the tab is not.
	restored := hosttest.NewRuntime("kitty")
	restored.Add("session:revier", "kitty", agent("1", "abc-123", ""))
	c.Runtime = restored
	dir := t.TempDir()
	res, err := c.GoResuming(context.Background(), p, "tickets", nil, []core.Resume{{Harness: "claude", Session: "tab-9", Dir: dir}})
	if err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	if len(restored.Tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(restored.Tabs))
	}
	real := restored.Tabs[0].Real
	if want := []string{"taskmgr-ui", "--resume", "tab-9"}; !slices.Equal(real.Launch, want) || real.Dir != dir {
		t.Errorf("tab launch = %v in %q, want %v in %q", real.Launch, real.Dir, want, dir)
	}
	if len(res.Agents) != 1 || res.Agents[0] != core.AgentResumed {
		t.Errorf("agents = %v, want one resumed", res.Agents)
	}
	dry := c.Resumes(p, "tickets", []core.Resume{{Harness: "claude", Session: "tab-9"}, {Harness: "claude"}})
	if !slices.Equal(dry, []core.AgentOutcome{core.AgentResumed, core.AgentDropped}) {
		t.Errorf("dry run = %v, want the first resumed and the second dropped", dry)
	}
}
