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
	"github.com/hk9890/revier/internal/session"
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
	// On tmux, focusing the workspace is what switches a terminal showing
	// another session to the tab.
	if len(rt.Focuses) != 1 || rt.Focuses[0] != rt.Tabs[0].Ref {
		t.Errorf("runtime focuses = %v, want the workspace %v", rt.Focuses, rt.Tabs[0].Ref)
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

// A workspace opened for a tab whose tab then fails is still reported, so the
// caller pins it and the next press does not open another.
func TestATabThatFailsInTheWorkspaceItOpenedReportsTheWorkspace(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.OpenTabErr = errors.New("kitty went away")
	c := &core.Core{Runtime: rt}

	res, err := c.Go(context.Background(), prepared(t, tabProject()), "tickets", nil)
	if err == nil {
		t.Fatal("Go succeeded, want the tab's failure")
	}
	if len(rt.Opened) != 1 || res.Target != "home" || res.Ref.IsZero() {
		t.Errorf("result = %+v, opened %d; want the opened workspace reported as home", res, len(rt.Opened))
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

func TestATabOnARuntimeWithoutTabsIsAnError(t *testing.T) {
	rt := hosttest.NewRuntime("tmux")
	rt.Add("session:revier", "")
	c := &core.Core{Runtime: noTabs{rt}}
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

// A tab opened for a target the project no longer declares is no tab target's
// panel, so its agent stays with the instance rather than dropping out of the
// save.
func TestAnAgentInATabNoTargetRecordsStaysWithItsInstance(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	stale := agent("2", "old-7", "")
	stale.Vars[core.PanelTargetVar] = "notes"
	rt.Add("session:revier", "kitty", agent("1", "abc-123", ""), stale)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, tabProject())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), report, "revier")
	if home := s.Projects[0].Targets[0]; len(home.Agents) != 2 || home.Agents[1].Session != "old-7" {
		t.Errorf("home = %+v, want both agents of its instance", home)
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

// An agent in a tab target is recorded under the tab, not under the instance
// that holds it, so a restore does not open it a second time as an extra agent
// tab of the workspace. It is not resumed: the tab runs its own launch argv,
// which need not be the agent, and a resume flag added to it would start a
// broken command. The save says so while the agent still runs.
func TestATabTargetsAgentIsRecordedOnceAndNotResumed(t *testing.T) {
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
	s, gaps := c.Session(context.Background(), report, "revier")
	targets := s.Projects[0].Targets
	if len(targets) != 2 || len(targets[0].Agents) != 1 || targets[0].Agents[0].Session != "abc-123" {
		t.Fatalf("targets = %+v, want home with its own agent only", targets)
	}
	if len(targets[1].Agents) != 1 || targets[1].Agents[0] != (session.Agent{Harness: "claude"}) {
		t.Fatalf("tickets = %+v, want the tab's agent by harness alone", targets[1])
	}
	if s.Conversations() != 1 || gaps.Unnamed != 0 {
		t.Errorf("conversations = %d, unnamed = %d, want the tab's agent in neither: it is never resumed", s.Conversations(), gaps.Unnamed)
	}
	if !slices.Equal(gaps.InTab, []string{"revier:tickets"}) {
		t.Errorf("gaps.InTab = %v, want the tab named at save time", gaps.InTab)
	}

	// A restore on a machine where the workspace is up and the tab is not.
	restored := hosttest.NewRuntime("kitty")
	restored.Add("session:revier", "kitty", agent("1", "abc-123", ""))
	c.Runtime = restored
	resumes := []core.Resume{{Harness: "claude", Session: "tab-9", Dir: t.TempDir()}}
	res, err := c.GoResuming(context.Background(), p, "tickets", nil, resumes)
	if err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	if len(restored.Tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(restored.Tabs))
	}
	if real := restored.Tabs[0].Real; !slices.Equal(real.Launch, []string{"taskmgr-ui"}) || real.Dir != "/p" {
		t.Errorf("tab launch = %v in %q, want the plain launch argv in the project", real.Launch, real.Dir)
	}
	if !slices.Equal(res.Agents, []core.AgentOutcome{core.AgentInTab}) {
		t.Errorf("agents = %v, want the one agent reported as in a tab target", res.Agents)
	}
	if dry := c.Resumes(p, "tickets", resumes); !slices.Equal(dry, res.Agents) {
		t.Errorf("dry run = %v, want what the restore did: %v", dry, res.Agents)
	}
}
