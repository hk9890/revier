package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// listedTabsProject is a workspace that is only the window: tickets is its
// first tab, and the agent and the shell are its second.
func listedTabsProject(active revier.TargetName) revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Match: revier.Match{Title: "^session:revier$"},
			Tabs: []revier.TargetName{"tickets", "agent"}, Active: active}},
		{Name: "tickets", Key: "ctrl-shift-t", Runtime: &revier.Realization{
			Inside: "home", Launch: []string{"taskmgr-ui"}, Dir: "/p/tickets"}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{
				{Kind: revier.PanelAgent, Command: []string{"claude"}},
				{Kind: revier.PanelShell},
			}}},
	}}
}

// marks is the target and the home mark of each panel that carries one, in
// the order the runtime lists the panels of its one instance.
func marks(t *testing.T, rt *hosttest.FakeRuntime) []string {
	t.Helper()
	instances, err := rt.Instances(context.Background())
	if err != nil || len(instances) != 1 {
		t.Fatalf("instances = %+v, %v; want one", instances, err)
	}
	var out []string
	for _, panel := range instances[0].Panels {
		if len(panel.Vars) > 0 {
			out = append(out, panel.Vars[core.PanelTargetVar]+"/"+panel.Vars[core.PanelHomeVar])
		}
	}
	return out
}

// lastPanelFocus is the panel the runtime was last asked to make current.
func lastPanelFocus(t *testing.T, rt *hosttest.FakeRuntime) revier.PanelID {
	t.Helper()
	if len(rt.PanelFocuses) == 0 {
		t.Fatal("no panel was focused")
	}
	return rt.PanelFocuses[len(rt.PanelFocuses)-1]
}

// The order of the tabs is the order they are opened in: the first entry
// opens the instance under the workspace's name, and the second is the tab
// after it. The active tab has the focus and the home mark.
func TestAWorkspaceOpensWithTheTabsItListsAndTheActiveOneCurrent(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, listedTabsProject("agent")), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("opened %d instances, want one", len(rt.Opened))
	}
	opened := rt.Opened[0]
	if opened.Name != "session:revier" || !slices.Equal(opened.Launch, []string{"taskmgr-ui"}) || len(opened.Panels) != 0 || opened.Dir != "/p/tickets" {
		t.Errorf("opened = %+v, want the workspace's name on the launch and the directory of tickets", opened)
	}
	if len(rt.Tabs) != 1 {
		t.Fatalf("tabs = %d, want the agent tab alone", len(rt.Tabs))
	}
	agent := rt.Tabs[0]
	if len(agent.Real.Panels) != 2 || agent.Real.Panels[0].Kind != revier.PanelAgent || agent.Real.Panels[1].Kind != revier.PanelShell {
		t.Errorf("second tab = %+v, want the agent and the shell", agent.Real.Panels)
	}
	if got, want := marks(t, rt), []string{"tickets/", "agent/home", "agent/home"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	if got := lastPanelFocus(t, rt); got != agent.Panel {
		t.Errorf("focused panel %s, want the agent panel %s", got, agent.Panel)
	}
	if len(rt.Focuses) != 1 || rt.Focuses[0] != res.Ref {
		t.Errorf("focuses = %v, want the new instance", rt.Focuses)
	}
	if res.Target != "home" || !slices.Equal(res.Tabs, []revier.TargetName{"tickets", "agent"}) || !res.Launched || res.Ref.IsZero() {
		t.Errorf("result = %+v, want a launch of home with tickets and agent", res)
	}
}

// With tickets active, or no tab named, the press ends on the first tab, and
// that tab carries the home mark.
func TestAWorkspaceEndsOnItsFirstTabWhenThatIsActiveOrNoneIs(t *testing.T) {
	for _, active := range []revier.TargetName{"tickets", ""} {
		t.Run("active="+string(active), func(t *testing.T) {
			rt := hosttest.NewRuntime("kitty")
			c := &core.Core{Runtime: rt}

			res, err := press(context.Background(), c, prepared(t, listedTabsProject(active)), "home")
			if err != nil {
				t.Fatalf("Go: %v", err)
			}
			if got, want := marks(t, rt), []string{"tickets/home", "agent/", "agent/"}; !slices.Equal(got, want) {
				t.Errorf("marks = %q, want %q", got, want)
			}
			if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
				t.Errorf("focused panel %s, want the tickets panel %s", got, want)
			}
		})
	}
}

// A press on the key of a listed tab with the workspace closed opens the
// workspace, which brings the tab: the press stays on it and opens no other.
// The second press goes home, which is the active tab.
func TestAPressOnAListedTabOfAClosedWorkspaceEndsOnThatTab(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, listedTabsProject("agent"))

	res, err := press(context.Background(), c, p, "tickets")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got, want := marks(t, rt), []string{"tickets/", "agent/home", "agent/home"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Errorf("opened %d instances and %d tabs, want the workspace and its agent tab", len(rt.Opened), len(rt.Tabs))
	}
	if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the tickets panel %s", got, want)
	}
	if res.Target != "home" || res.Tab != "tickets" || !res.Launched {
		t.Errorf("result = %+v, want a launch landing on the tickets tab of home", res)
	}

	rt.SetFocus(res.Ref)
	if _, err := press(context.Background(), c, p, "tickets"); err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if got, want := lastPanelFocus(t, rt), rt.Tabs[0].Panel; got != want {
		t.Errorf("focused panel after the second press %s, want the agent panel %s", got, want)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Errorf("opened %d instances and %d tabs after the second press, want nothing new", len(rt.Opened), len(rt.Tabs))
	}
}

// An entry that is refused in this project is skipped: the next one opens the
// instance. An active tab that was skipped leaves the first opened tab active.
func TestAListedTabThatIsRefusedIsSkipped(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, listedTabsProject("tickets"))
	p.Refuse(1, errors.New("tickets is refused"))

	res, err := press(context.Background(), c, p, "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Opened[0].Panels) != 2 || rt.Opened[0].Name != "session:revier" || len(rt.Tabs) != 0 {
		t.Fatalf("opened = %+v, tabs = %+v; want the agent panels as the instance and no other tab", rt.Opened, rt.Tabs)
	}
	if got, want := marks(t, rt), []string{"agent/home", "agent/home"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the agent panel %s", got, want)
	}
	if !slices.Equal(res.Tabs, []revier.TargetName{"agent"}) {
		t.Errorf("tabs = %v, want agent alone", res.Tabs)
	}
}

// failingTabs is a runtime whose OpenTab fails at one call and counts all of
// them.
type failingTabs struct {
	*hosttest.FakeRuntime
	failAt, calls int
}

func (f *failingTabs) OpenTab(ctx context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	f.calls++
	if f.calls == f.failAt {
		return "", errors.New("kitty went away")
	}
	return f.FakeRuntime.OpenTab(ctx, ref, r, vars)
}

// A tab that fails stops the sequence. The workspace is left open with the
// tabs it has, and reported with the failure, so the next press raises it and
// opens no second one.
func TestAListedTabThatDoesNotOpenStopsTheRestAndTheWorkspaceIsKept(t *testing.T) {
	rt := &failingTabs{FakeRuntime: hosttest.NewRuntime("kitty"), failAt: 1}
	c := &core.Core{Runtime: rt}
	project := listedTabsProject("agent")
	project.Targets[0].Runtime.Tabs = append(project.Targets[0].Runtime.Tabs, "logs")
	project.Targets = append(project.Targets, revier.Target{Name: "logs", Runtime: &revier.Realization{
		Inside: "home", Launch: []string{"tail", "-f", "log"}}})
	p := prepared(t, project)

	res, err := pressResuming(context.Background(), c, p, "home", []core.Resume{{Harness: "claude", Session: "abc"}})
	if err == nil {
		t.Fatal("Go succeeded, want the failure of the agent tab")
	}
	for _, want := range []string{"kitty went away", "tab agent", "home"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	if rt.calls != 1 || len(rt.Tabs) != 0 {
		t.Errorf("OpenTab ran %d times and opened %d tabs, want it to stop at the first", rt.calls, len(rt.Tabs))
	}
	if res.Target != "home" || !slices.Equal(res.Tabs, []revier.TargetName{"tickets"}) || res.Ref.IsZero() || !res.Launched {
		t.Errorf("result = %+v, want the opened workspace reported with tickets in it", res)
	}
	if len(rt.Closed) != 0 || len(rt.ClosedPanels) != 0 {
		t.Errorf("closed %v and panels %v, want the workspace kept", rt.Closed, rt.ClosedPanels)
	}
	if want := []core.AgentOutcome{core.AgentNotAdded}; !slices.Equal(res.Agents, want) {
		t.Errorf("agents = %v, want %v: no agent started", res.Agents, want)
	}

	if _, err := press(context.Background(), c, p, "home"); err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if len(rt.Opened) != 1 || rt.calls != 1 {
		t.Errorf("opened %d instances and %d tabs, want the kept one raised as it is", len(rt.Opened), rt.calls)
	}
}

// A runtime with no tabs cannot open what the target lists, and says so at
// the key.
func TestARuntimeWithoutTabsRefusesATargetThatListsTabs(t *testing.T) {
	rt := hosttest.NewRuntime("plain")
	c := &core.Core{Runtime: bareRuntime{rt}}

	_, err := press(context.Background(), c, prepared(t, listedTabsProject("agent")), "home")
	if !errors.Is(err, core.ErrNoTabs) {
		t.Fatalf("err = %v, want ErrNoTabs", err)
	}
	for _, want := range []string{"home", "plain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	if len(rt.Opened) != 0 {
		t.Errorf("opened = %+v, want nothing", rt.Opened)
	}
}

// tabs and active apply when the instance is created. One that is open is
// raised as it stands.
func TestAnOpenWorkspaceIsNotChangedByTheTabsItsTargetLists(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	ref := rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, listedTabsProject("agent")), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 0 || len(rt.Tabs) != 0 || len(rt.PanelFocuses) != 0 {
		t.Errorf("opened %d, tabs %d, panel focuses %v; want the instance raised and nothing else", len(rt.Opened), len(rt.Tabs), rt.PanelFocuses)
	}
	if res.Ref != ref || res.Launched || len(res.Tabs) != 0 {
		t.Errorf("result = %+v, want a raise of %v", res, ref)
	}
}

// unmarkedOpen is a runtime whose Open does not set the marks it is given, as
// a runtime whose mark call fails does.
type unmarkedOpen struct{ *hosttest.FakeRuntime }

func (u unmarkedOpen) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	r.Vars = nil
	return u.FakeRuntime.Open(ctx, r)
}

// A runtime sets the mark of the tab that creates the instance as best
// effort. The press still ends on that tab when it is the active one or the
// one whose key was pressed, and opens no second copy of it.
func TestATabThatCreatedTheWorkspaceIsFoundWithoutItsMark(t *testing.T) {
	for _, key := range []revier.TargetName{"home", "tickets"} {
		t.Run(string(key), func(t *testing.T) {
			rt := hosttest.NewRuntime("kitty")
			c := &core.Core{Runtime: unmarkedOpen{rt}}
			active := revier.TargetName("tickets")
			if key == "tickets" {
				active = "agent"
			}

			res, err := press(context.Background(), c, prepared(t, listedTabsProject(active)), key)
			if err != nil {
				t.Fatalf("Go: %v", err)
			}
			if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
				t.Errorf("opened %d instances and %d tabs, want the workspace and its agent tab", len(rt.Opened), len(rt.Tabs))
			}
			if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
				t.Errorf("focused panel %s, want the tickets panel %s", got, want)
			}
		})
	}
}

// firstUnmarked is a runtime that lists the panels of Open as one tab whose
// first panel lost its mark, as kitty does when set-user-vars fails on the
// first window and the launch of the second carried its vars.
type firstUnmarked struct{ *hosttest.FakeRuntime }

func (f firstUnmarked) Instances(ctx context.Context) ([]revier.Instance, error) {
	instances, err := f.FakeRuntime.Instances(ctx)
	for i := range instances {
		for n := range instances[i].Panels {
			panel := &instances[i].Panels[n]
			if panel.Tab == "" {
				panel.Tab = "created"
			}
			if n == 0 {
				panel.Vars = nil
			}
		}
	}
	return instances, err
}

// The tab that created the instance is taken from its first panel when only a
// later panel of it carries the mark: the press ends on the agent, not on the
// shell beside it.
func TestATabThatCreatedTheWorkspaceStartsAtItsFirstPanelWhenThatLostItsMark(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: firstUnmarked{rt}}
	project := listedTabsProject("agent")
	project.Targets[0].Runtime.Tabs = []revier.TargetName{"agent", "tickets"}

	res, err := press(context.Background(), c, prepared(t, project), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the agent panel %s", got, want)
	}
}

// A listed tab comes back with its workspace, so a save records no step for
// it. A tab the target does not list keeps its step.
func TestASaveRecordsNoStepForAListedTab(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	project := listedTabsProject("agent")
	project.Targets = append(project.Targets, revier.Target{Name: "logs", Runtime: &revier.Realization{
		Inside: "home", Launch: []string{"tail", "-f", "log"}}})
	p := prepared(t, project)
	for _, name := range []revier.TargetName{"home", "logs"} {
		if _, err := press(context.Background(), c, p, name); err != nil {
			t.Fatalf("Go %s: %v", name, err)
		}
	}

	r, err := c.Survey(context.Background(), []core.Project{p})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), r, "revier")
	if len(s.Projects) != 1 {
		t.Fatalf("session = %+v, want one project", s.Projects)
	}
	var steps []revier.TargetName
	for _, step := range s.Projects[0].Targets {
		steps = append(steps, step.Name)
	}
	if want := []revier.TargetName{"home", "logs"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %v, want %v", steps, want)
	}
}

// A target with panels and no tabs opens as it did before a target could list
// its tabs: the panels are the instance, and its first panel has the home
// mark.
func TestAWorkspaceThatListsNoTabsOpensItsOwnPanels(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, tabProject()), "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 1 || len(rt.Opened[0].Panels) != 1 || len(rt.Tabs) != 0 || len(rt.PanelFocuses) != 0 {
		t.Fatalf("opened = %+v, tabs = %+v, panel focuses = %v; want the declared panels alone", rt.Opened, rt.Tabs, rt.PanelFocuses)
	}
	if got, want := marks(t, rt), []string{"/home"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	if len(res.Tabs) != 0 {
		t.Errorf("tabs = %v, want none", res.Tabs)
	}
}

// markedPanels is the panels of the instance that carry the mark of the named
// tab target, in listing order, and the tabs that hold them.
func markedPanels(t *testing.T, rt *hosttest.FakeRuntime, name revier.TargetName) (panels []revier.PanelID, tabs []string) {
	t.Helper()
	instances, err := rt.Instances(context.Background())
	if err != nil || len(instances) != 1 {
		t.Fatalf("instances = %+v, %v; want one", instances, err)
	}
	for _, panel := range instances[0].Panels {
		if panel.Vars[core.PanelTargetVar] != string(name) {
			continue
		}
		panels = append(panels, panel.ID)
		if !slices.Contains(tabs, panel.Tab) {
			tabs = append(tabs, panel.Tab)
		}
	}
	return panels, tabs
}

// The first panel of a tab that holds panels is the agent, and the tab
// outlives it. The key still finds the tab by the panel that is left, and
// opens no second one (decisions.md D64).
func TestATabIsFoundAfterItsFirstPanelHasEnded(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, listedTabsProject("tickets"))
	res, err := press(context.Background(), c, p, "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	opened, _ := markedPanels(t, rt, "agent")
	if len(opened) != 2 {
		t.Fatalf("panels of the agent tab = %v, want the agent and the shell", opened)
	}
	if err := rt.ClosePanel(context.Background(), res.Ref, opened[0]); err != nil {
		t.Fatalf("ClosePanel: %v", err)
	}

	if _, err := press(context.Background(), c, p, "agent"); err != nil {
		t.Fatalf("Go agent: %v", err)
	}
	left, tabs := markedPanels(t, rt, "agent")
	if len(rt.Tabs) != 1 || len(tabs) != 1 || !slices.Equal(left, opened[1:]) {
		t.Errorf("OpenTab ran %d times, and the agent target has panels %v in tabs %v; want the one tab with its shell", len(rt.Tabs), left, tabs)
	}
	if got := lastPanelFocus(t, rt); got != opened[1] {
		t.Errorf("focused panel %s, want the shell that is left, %s", got, opened[1])
	}
}

// The second press goes home from any panel of the tab, not only from its
// first.
func TestAPressOnATabGoesHomeFromItsSecondPanel(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, listedTabsProject("tickets"))
	res, err := press(context.Background(), c, p, "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	opened, _ := markedPanels(t, rt, "agent")
	if len(opened) != 2 {
		t.Fatalf("panels of the agent tab = %v, want the agent and the shell", opened)
	}
	if err := rt.FocusPanel(context.Background(), res.Ref, opened[1]); err != nil {
		t.Fatalf("FocusPanel: %v", err)
	}
	rt.SetFocus(res.Ref)

	back, err := press(context.Background(), c, p, "agent")
	if err != nil {
		t.Fatalf("Go agent: %v", err)
	}
	if back.Target != "home" || back.Tab != "" {
		t.Errorf("result = %+v, want the press to land on home", back)
	}
	if got, want := lastPanelFocus(t, rt), rt.FirstPanel(res.Ref); got != want {
		t.Errorf("focused panel %s, want the tickets panel %s, which has the home mark", got, want)
	}
}

// openListedTabs opens the workspace that lists its tabs, with agent the
// active one, on a runtime with a probe that can resume.
func openListedTabs(t *testing.T) (*core.Core, *hosttest.FakeRuntime, core.Project, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, listedTabsProject("agent"))
	res, err := press(context.Background(), c, p, "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	return c, rt, p, res.Ref
}

// The agent panel and the shell panel of a workspace that lists its tabs are
// the ones of its tabs (decisions.md D127). `revier agent new` opens a copy of
// them as a tab of the workspace, whether the command names the workspace or
// the tab that holds the panels, and returns the new agent's panel.
func TestAgentNewOpensAnAgentTabInAWorkspaceThatListsItsTabs(t *testing.T) {
	c, rt, p, ref := openListedTabs(t)
	dir := t.TempDir()

	if target, err := c.AgentTarget(p); err != nil || target != "home" {
		t.Fatalf("AgentTarget = %q, %v; want home", target, err)
	}
	for _, name := range []revier.TargetName{"home", "agent"} {
		w, err := c.AgentWorkspace(context.Background(), p, name)
		if err != nil {
			t.Fatalf("AgentWorkspace %s: %v", name, err)
		}
		if w.Target != "home" || w.Ref != ref {
			t.Fatalf("workspace of %s = %s in %v, want home in %v", name, w.Target, w.Ref, ref)
		}
		panel, outcome, err := c.AddAgent(context.Background(), w, core.Resume{Session: "abc-123", Dir: dir})
		if err != nil || outcome != core.AgentResumed {
			t.Fatalf("AddAgent in %s = %v, %v; want resumed", name, outcome, err)
		}
		tab := rt.Tabs[len(rt.Tabs)-1]
		if tab.Ref != ref || tab.Vars != nil || panel != tab.Panel {
			t.Errorf("tab = %+v and panel %s, want an unmarked tab in %v and its first panel", tab, panel, ref)
		}
		samePanels(t, "tab", tab.Real.Panels, []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "abc-123"}, Dir: dir},
			{Kind: revier.PanelShell, Dir: dir},
		})
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 3 {
		t.Errorf("opened %d instances and %d tabs, want the workspace, its agent tab and the two added", len(rt.Opened), len(rt.Tabs))
	}
}

// `revier shell new` takes the shell panel from the same tab.
func TestShellNewOpensTheShellOfAListedTab(t *testing.T) {
	c, rt, p, _ := openListedTabs(t)
	w, err := c.AgentWorkspace(context.Background(), p, "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	if err := c.NewShell(context.Background(), w, ""); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	samePanels(t, "tab", rt.Tabs[len(rt.Tabs)-1].Real.Panels, []revier.PanelSpec{{Kind: revier.PanelShell, Dir: "/p"}})
}

// A panel served to a link is read from the same place, under the name of the
// workspace that lists the tab.
func TestServeTakesThePanelsOfAListedTab(t *testing.T) {
	c := &core.Core{}
	p := prepared(t, listedTabsProject("agent"))

	agent, err := c.ServeAgent(p, core.Resume{})
	if err != nil || agent.Workspace != "session:revier" || !slices.Equal(agent.Argv, []string{"claude"}) {
		t.Errorf("ServeAgent = %+v, %v; want claude in session:revier", agent, err)
	}
	shell, err := c.ServeShell(p, "")
	if err != nil || shell.Workspace != "session:revier" || shell.Dir != "/p" {
		t.Errorf("ServeShell = %+v, %v; want a shell of session:revier in /p", shell, err)
	}
}

// savedListedTabs is the session a save records for the workspace that lists
// its tabs, open with tickets, the agent tab on the conversation main, and an
// agent tab `revier agent new` added on the conversation by-hand.
func savedListedTabs(t *testing.T, dir string) (session.Session, core.SessionGaps) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	inTab := func(panel revier.Panel, tab string, marks map[string]string) revier.Panel {
		panel.Tab = tab
		if panel.Vars == nil {
			panel.Vars = map[string]string{}
		}
		for k, v := range marks {
			panel.Vars[k] = v
		}
		return panel
	}
	agentTab := map[string]string{core.PanelTargetVar: "agent", core.PanelHomeVar: "home"}
	rt.Add("session:revier", "kitty",
		inTab(revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "taskmgr-ui"}, "t1", map[string]string{core.PanelTargetVar: "tickets"}),
		inTab(agent("2", "main", dir), "t2", agentTab),
		inTab(revier.Panel{ID: "3", Kind: revier.PanelShell, Title: "zsh"}, "t2", agentTab),
		inTab(agent("4", "by-hand", dir), "t3", nil),
		inTab(revier.Panel{ID: "5", Kind: revier.PanelShell, Title: "zsh"}, "t3", nil),
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	return c.Session(context.Background(), survey(t, c, []core.Project{prepared(t, listedTabsProject("agent"))}, nil), "revier")
}

// restoredInto restores s over a project on a fresh runtime, and checks that
// the dry run said what the restore then did.
func restoredInto(t *testing.T, project revier.Project, s session.Session) (*hosttest.FakeRuntime, core.Restored) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}, Ledger: &ledger{}}
	projects := []core.Project{prepared(t, project)}
	report := survey(t, c, projects, nil)
	preview := c.RestorePreview(s, report, projects)
	out, back := c.Restore(context.Background(), s, report, projects)
	if back != nil {
		t.Fatalf("Restore: %v", back)
	}
	for i := range out {
		if out[i].Err != nil {
			t.Fatalf("step %s: %v", out[i].Target, out[i].Err)
		}
		if !slices.Equal(preview[i].Agents, out[i].Agents) {
			t.Errorf("dry run of %s = %v, the restore did %v", out[i].Target, preview[i].Agents, out[i].Agents)
		}
	}
	return rt, out
}

// The agent in a panel of a listed tab is recorded under the workspace, with
// its conversation, in the shape a workspace with its own panels is recorded
// in. A restore lays it over the agent panel of that tab, and opens the agent
// past it as an agent tab.
func TestSaveThenRestoreResumesTheAgentOfAListedTab(t *testing.T) {
	dir := t.TempDir()
	s, gaps := savedListedTabs(t, dir)

	if len(s.Projects) != 1 || len(s.Projects[0].Targets) != 1 || s.Projects[0].Targets[0].Name != "home" {
		t.Fatalf("session = %+v, want the one step home", s.Projects)
	}
	want := []session.Agent{{Harness: "claude", Session: "main", Dir: dir}, {Harness: "claude", Session: "by-hand", Dir: dir}}
	if got := s.Projects[0].Targets[0].Agents; !slices.Equal(got, want) {
		t.Errorf("agents = %+v, want %+v", got, want)
	}
	if len(gaps.InTab) != 0 || gaps.Unnamed != 0 {
		t.Errorf("gaps = %+v, want none", gaps)
	}

	rt, out := restoredInto(t, listedTabsProject("agent"), s)
	if wantAgents := []core.AgentOutcome{core.AgentResumed, core.AgentResumed}; len(out) != 1 || !slices.Equal(out[0].Agents, wantAgents) {
		t.Fatalf("restore = %+v, want home with both agents resumed", out)
	}
	if len(rt.Opened) != 1 || !slices.Equal(rt.Opened[0].Launch, []string{"taskmgr-ui"}) || len(rt.Tabs) != 2 {
		t.Fatalf("opened = %+v, tabs = %+v; want tickets, the agent tab and one added tab", rt.Opened, rt.Tabs)
	}
	if rt.Tabs[0].Vars[core.PanelTargetVar] != "agent" || rt.Tabs[1].Vars != nil {
		t.Errorf("tab marks = %v and %v, want the agent tab and an added one", rt.Tabs[0].Vars, rt.Tabs[1].Vars)
	}
	samePanels(t, "agent tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "main"}, Dir: dir},
		{Kind: revier.PanelShell, Dir: "/p"},
	})
	samePanels(t, "added tab", rt.Tabs[1].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"claude", "--resume", "by-hand"}, Dir: dir},
		{Kind: revier.PanelShell, Dir: dir},
	})
	if got := lastPanelFocus(t, rt); got != rt.Tabs[0].Panel {
		t.Errorf("focused panel %s, want the agent panel %s of the active tab", got, rt.Tabs[0].Panel)
	}
}

// A file saved while the panels were the workspace's own restores into the
// project after its panels moved into a listed tab: the conversation goes to
// the agent panel of that tab, and the step of the tab that is now listed
// opens no second copy of it.
func TestASessionOfAWorkspaceWithItsOwnPanelsRestoresIntoItsListedTabs(t *testing.T) {
	dir := t.TempDir()
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home", Agents: []session.Agent{{Harness: "claude", Session: "main", Dir: dir}}},
		{Name: "tickets"},
	}}}}

	rt, out := restoredInto(t, listedTabsProject("agent"), s)
	if len(out) != 2 || !slices.Equal(out[0].Agents, []core.AgentOutcome{core.AgentResumed}) {
		t.Fatalf("restore = %+v, want home with its agent resumed", out)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Fatalf("opened %d instances and %d tabs, want the workspace and its agent tab", len(rt.Opened), len(rt.Tabs))
	}
	if got, want := rt.Tabs[0].Real.Panels[0].Command, []string{"claude", "--resume", "main"}; !slices.Equal(got, want) {
		t.Errorf("agent command = %v, want %v", got, want)
	}
}

// The reverse: a file saved from listed tabs restores into a workspace that
// holds its own panels.
func TestASessionOfListedTabsRestoresIntoAWorkspaceWithItsOwnPanels(t *testing.T) {
	dir := t.TempDir()
	s, _ := savedListedTabs(t, dir)

	rt, out := restoredInto(t, tabProject(), s)
	if len(out) != 1 || !slices.Equal(out[0].Agents, []core.AgentOutcome{core.AgentResumed, core.AgentResumed}) {
		t.Fatalf("restore = %+v, want home with both agents resumed", out)
	}
	if got, want := rt.Opened[0].Panels[0].Command, []string{"claude", "--resume", "main"}; !slices.Equal(got, want) {
		t.Errorf("agent command = %v, want %v", got, want)
	}
	if len(rt.Tabs) != 1 {
		t.Errorf("tabs = %+v, want the one agent past the layout", rt.Tabs)
	}
}

// An agent laid over a tab that opened keeps its outcome when a later tab
// fails: it started.
func TestAnAgentOfAnOpenedTabIsReportedWhenALaterTabFails(t *testing.T) {
	rt := &failingTabs{FakeRuntime: hosttest.NewRuntime("kitty"), failAt: 1}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	project := listedTabsProject("agent")
	project.Targets[0].Runtime.Tabs = []revier.TargetName{"agent", "tickets"}

	res, err := pressResuming(context.Background(), c, prepared(t, project), "home", []core.Resume{
		{Harness: "claude", Session: "main"}, {Harness: "claude", Session: "by-hand"},
	})
	if err == nil {
		t.Fatal("Go succeeded, want the failure of the tickets tab")
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentNotAdded}; !slices.Equal(res.Agents, want) {
		t.Errorf("agents = %v, want %v", res.Agents, want)
	}
}

// A shutdown of the agents alone keeps the shell the workspace declares
// beside its agent. In a workspace that lists its tabs that is the shell of
// the tab that carries the home mark.
func TestShutdownAgentsKeepsTheShellOfTheTabWithTheHomeMark(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "claude"}}}
	projects := []core.Project{prepared(t, listedTabsProject("agent"))}
	if _, err := press(context.Background(), c, projects[0], "home"); err != nil {
		t.Fatalf("Go: %v", err)
	}
	panels, _ := markedPanels(t, rt, "agent")

	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAgents)
	if len(plan) != 1 || !slices.Equal(plan[0].Panels, panels[:1]) {
		t.Fatalf("plan = %+v, want the agent panel %s alone", plan, panels[0])
	}
	if _, err := c.Shutdown(context.Background(), plan, 0, reading(projects)); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !slices.Equal(rt.ClosedPanels, panels[:1]) {
		t.Errorf("panels closed = %v, want the agent panel %s", rt.ClosedPanels, panels[0])
	}
	if left, _ := markedPanels(t, rt, "agent"); !slices.Equal(left, panels[1:]) {
		t.Errorf("panels of the agent tab after = %v, want its shell %v", left, panels[1:])
	}
	if got := marks(t, rt); len(got) != 2 {
		t.Errorf("marks after = %q, want tickets and the shell", got)
	}
}

// closeTab closes every panel of the named tab, as its programs ending does.
func closeTab(t *testing.T, rt *hosttest.FakeRuntime, ref revier.TargetRef, name revier.TargetName) {
	t.Helper()
	panels, _ := markedPanels(t, rt, name)
	for _, panel := range panels {
		if err := rt.ClosePanel(context.Background(), ref, panel); err != nil {
			t.Fatalf("ClosePanel: %v", err)
		}
	}
}

// The active tab that was closed whole took the home mark with it. Its key
// opens it with that mark again, so a return home from another tab still
// lands in it: every tab of the workspace carries a target mark, and no other
// panel names the place.
func TestAnActiveTabOpenedAgainByItsKeyHasTheHomeMark(t *testing.T) {
	c, rt, p, ref := openListedTabs(t)
	closeTab(t, rt, ref, "agent")

	if _, err := press(context.Background(), c, p, "agent"); err != nil {
		t.Fatalf("Go agent: %v", err)
	}
	if got, want := marks(t, rt), []string{"tickets/", "agent/home", "agent/home"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
	reopened := rt.Tabs[len(rt.Tabs)-1].Panel

	rt.SetFocus(ref)
	if _, err := press(context.Background(), c, p, "tickets"); err != nil {
		t.Fatalf("Go tickets: %v", err)
	}
	if got, want := lastPanelFocus(t, rt), rt.FirstPanel(ref); got != want {
		t.Fatalf("focused panel %s, want the tickets panel %s", got, want)
	}
	if _, err := press(context.Background(), c, p, "tickets"); err != nil {
		t.Fatalf("second Go tickets: %v", err)
	}
	if got := lastPanelFocus(t, rt); got != reopened {
		t.Errorf("focused panel after the second press %s, want the agent panel %s of the reopened tab", got, reopened)
	}
}

// A tab its key opens beside a panel that has the home mark gets its own mark
// alone: one tab of an instance is home.
func TestATabOpenedByItsKeyBesideTheHomeMarkHasItsOwnMarkAlone(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, listedTabsProject("tickets"))
	res, err := press(context.Background(), c, p, "home")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	closeTab(t, rt, res.Ref, "agent")

	if _, err := press(context.Background(), c, p, "agent"); err != nil {
		t.Fatalf("Go agent: %v", err)
	}
	if got, want := marks(t, rt), []string{"tickets/home", "agent/", "agent/"}; !slices.Equal(got, want) {
		t.Errorf("marks = %q, want %q", got, want)
	}
}

// The dry run of a restore refuses what the launch refuses: on a runtime with
// no tabs a target that lists tabs starts no agent, so it reports none.
func TestTheDryRunReportsNoAgentForATargetTheRuntimeCannotOpen(t *testing.T) {
	c := &core.Core{Runtime: bareRuntime{hosttest.NewRuntime("plain")}, Probes: []revier.AgentProbe{resumable()}}

	got := c.Resumes(prepared(t, listedTabsProject("agent")), "home", []core.Resume{{Harness: "claude", Session: "abc"}})
	if len(got) != 0 {
		t.Errorf("Resumes = %v, want no outcome", got)
	}
}

// marked is the panel in a tab of the instance, under the mark of the tab
// target it was opened for.
func marked(panel revier.Panel, tab string, target revier.TargetName) revier.Panel {
	panel.Tab = tab
	if panel.Vars == nil {
		panel.Vars = map[string]string{}
	}
	panel.Vars[core.PanelTargetVar] = string(target)
	return panel
}

// savedWith is the session a save records for the project with the panels in
// its one workspace.
func savedWith(t *testing.T, project revier.Project, panels ...revier.Panel) (session.Session, core.SessionGaps) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:revier", "kitty", panels...)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	s, gaps := c.Session(context.Background(), survey(t, c, []core.Project{prepared(t, project)}, nil), "revier")
	if len(s.Projects) != 1 {
		t.Fatalf("session = %+v, want one project", s.Projects)
	}
	return s, gaps
}

// A listed tab that runs a launch is no part of the workspace's layout. An
// agent that runs as its program is not recorded under the workspace, where a
// restore would lay its conversation over the agent panel of another tab: the
// save names it as an agent in a tab (decisions.md D68).
func TestTheAgentOfAListedTabThatRunsALaunchIsNotRecordedUnderTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	s, gaps := savedWith(t, listedTabsProject("agent"),
		marked(agent("1", "in-tickets", dir), "t1", "tickets"),
		marked(agent("2", "main", dir), "t2", "agent"),
		marked(revier.Panel{ID: "3", Kind: revier.PanelShell, Title: "zsh"}, "t2", "agent"),
	)

	steps := s.Projects[0].Targets
	if len(steps) != 1 || steps[0].Name != "home" {
		t.Fatalf("steps = %+v, want home alone", steps)
	}
	if want := []session.Agent{{Harness: "claude", Session: "main", Dir: dir}}; !slices.Equal(steps[0].Agents, want) {
		t.Errorf("agents of home = %+v, want %+v", steps[0].Agents, want)
	}
	if want := []string{"revier:tickets"}; !slices.Equal(gaps.InTab, want) {
		t.Errorf("agents in a tab = %v, want %v", gaps.InTab, want)
	}
}

// A tab that holds panels and that its workspace does not list is not a part
// of the layout either: it keeps its step, so a restore opens it again, and
// its agent is recorded under it by harness alone (decisions.md D68).
func TestATabWithPanelsThatIsNotListedKeepsItsStep(t *testing.T) {
	dir := t.TempDir()
	project := listedTabsProject("agent")
	project.Targets = append(project.Targets, revier.Target{Name: "review", Runtime: &revier.Realization{
		Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude"}},
			{Kind: revier.PanelShell},
		}}})
	s, gaps := savedWith(t, project,
		marked(revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "taskmgr-ui"}, "t1", "tickets"),
		marked(agent("2", "in-review", dir), "t2", "review"),
		marked(revier.Panel{ID: "3", Kind: revier.PanelShell, Title: "zsh"}, "t2", "review"),
	)

	steps := s.Projects[0].Targets
	if len(steps) != 2 || steps[0].Name != "home" || steps[1].Name != "review" {
		t.Fatalf("steps = %+v, want home and review", steps)
	}
	if len(steps[0].Agents) != 0 {
		t.Errorf("agents of home = %+v, want none", steps[0].Agents)
	}
	if want := []session.Agent{{Harness: "claude"}}; !slices.Equal(steps[1].Agents, want) {
		t.Errorf("agents of review = %+v, want %+v", steps[1].Agents, want)
	}
	if want := []string{"revier:review"}; !slices.Equal(gaps.InTab, want) {
		t.Errorf("agents in a tab = %v, want %v", gaps.InTab, want)
	}
}

// twoAgentTabsProject is a workspace whose two listed tabs each declare an
// agent panel, on commands that differ. The file declares them in the other
// order.
func twoAgentTabsProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Match: revier.Match{Title: "^session:revier$"},
			Tabs: []revier.TargetName{"agent", "review"}}},
		{Name: "review", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude", "--model", "review"}}}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}}}},
	}}
}

// The first panel of a kind is the one of the first listed tab that declares
// it, in the order of `tabs` and not of the file (decisions.md D127): an agent
// tab and a served agent copy that one.
func TestTheAgentPanelOfAWorkspaceIsTheOneOfItsFirstListedTab(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, twoAgentTabsProject())
	if _, err := press(context.Background(), c, p, "home"); err != nil {
		t.Fatalf("Go: %v", err)
	}
	w, err := c.AgentWorkspace(context.Background(), p, "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}

	if _, _, err := c.AddAgent(context.Background(), w, core.Resume{}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if got, want := rt.Tabs[len(rt.Tabs)-1].Real.Panels[0].Command, []string{"claude"}; !slices.Equal(got, want) {
		t.Errorf("agent tab runs %v, want %v, the agent of the first listed tab", got, want)
	}
	served, err := c.ServeAgent(p, core.Resume{})
	if err != nil || !slices.Equal(served.Argv, []string{"claude"}) {
		t.Errorf("ServeAgent = %+v, %v; want the agent of the first listed tab", served, err)
	}
}

// A restore lays the recorded agents over the agent panels of the listed
// tabs in the order of the tabs: the first conversation is in the first tab.
func TestARestoreLaysTheAgentsOverTheListedTabsInTheirOrder(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	res, err := pressResuming(context.Background(), c, prepared(t, twoAgentTabsProject()), "home", []core.Resume{
		{Harness: "claude", Session: "one"}, {Harness: "claude", Session: "two"},
	})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentResumed}; !slices.Equal(res.Agents, want) {
		t.Errorf("agents = %v, want %v", res.Agents, want)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 {
		t.Fatalf("opened %d instances and %d tabs, want the agent tab and the review tab", len(rt.Opened), len(rt.Tabs))
	}
	if got, want := rt.Opened[0].Panels[0].Command, []string{"claude", "--resume", "one"}; !slices.Equal(got, want) {
		t.Errorf("first tab runs %v, want %v", got, want)
	}
	if got, want := rt.Tabs[0].Real.Panels[0].Command, []string{"claude", "--model", "review", "--resume", "two"}; !slices.Equal(got, want) {
		t.Errorf("second tab runs %v, want %v", got, want)
	}
}
