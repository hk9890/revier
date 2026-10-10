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
