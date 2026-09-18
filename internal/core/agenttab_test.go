// Layer L2: opening an agent tab in an open workspace, and finding the
// workspace from a panel, are decisions the core makes from what a host
// reports.
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

// openWorkspace is agentProject with its home open on a fresh runtime, holding
// the given live panels.
func openWorkspace(t *testing.T, panels ...revier.Panel) (*core.Core, *hosttest.FakeRuntime, core.Project, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	ref := rt.Add("session:revier", "", panels...)
	return c, rt, prepared(t, agentProject()), ref
}

// newAgent is `revier agent new -p <project>:<target>`: the workspace found,
// then the tab opened in it.
func newAgent(t *testing.T, c *core.Core, p core.Project, target revier.TargetName, r core.Resume) (core.AgentOutcome, error) {
	t.Helper()
	w, err := c.AgentWorkspace(context.Background(), p, target, nil)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	return c.NewAgent(context.Background(), w, r)
}

// The key's tab: the project's agent panel on the conversation asked for, and
// the project's shell, both in the directory asked for, opened in the open
// workspace, with the new agent made current.
func TestNewAgentOpensAnAgentTabInTheOpenWorkspace(t *testing.T) {
	c, rt, p, ref := openWorkspace(t)
	dir := t.TempDir()

	outcome, err := newAgent(t, c, p, "home", core.Resume{Session: "abc-123", Dir: dir})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentResumed {
		t.Errorf("outcome = %v, want resumed", outcome)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref || rt.Tabs[0].Vars != nil {
		t.Fatalf("Tabs = %+v, want one tab without a target var in %+v", rt.Tabs, ref)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus", "--resume", "abc-123"}, Dir: dir},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: dir},
	})
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{rt.Tabs[0].Panel}) {
		t.Errorf("panel focuses = %v, want the new agent %s", rt.PanelFocuses, rt.Tabs[0].Panel)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("Opened = %+v, want nothing opened: the tab goes into the open workspace", rt.Opened)
	}
}

// With no conversation and no directory, the agent starts empty where the
// workspace starts.
func TestNewAgentStartsEmptyInTheProject(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)

	outcome, err := newAgent(t, c, p, "home", core.Resume{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentEmpty {
		t.Errorf("outcome = %v, want empty", outcome)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus"}, Dir: "/home/hans/dev/github/revier"},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: "/home/hans/dev/github/revier"},
	})
}

// --resume names a conversation and not its harness. It goes to the harness
// the agent panel runs: a panel no resumable probe claims starts empty, not on
// another harness's resume flag.
func TestNewAgentResumesOnlyTheHarnessTheAgentPanelRuns(t *testing.T) {
	proj := agentProject()
	proj.Targets[0].Runtime.Panels[1] = revier.PanelSpec{Kind: revier.PanelAgent, Title: "opencode", Command: []string{"opencode"}}
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable(), &hosttest.FakeProbe{Harness: "opencode", Marker: "opencode"}}}
	rt.Add("session:revier", "")

	outcome, err := newAgent(t, c, prepared(t, proj), "home", core.Resume{Session: "abc-123"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentUnresumable {
		t.Errorf("outcome = %v, want unresumable", outcome)
	}
	if got := rt.Tabs[0].Real.Panels[0].Command; !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("agent command = %q, want opencode without a resume flag", got)
	}
}

// A workspace that is not open has no instance to open a tab in, and a target
// with no agent panel has no tab to give. Neither opens anything.
func TestNewAgentRefusesWhatItCannotOpenIn(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())

	if _, err := c.AgentWorkspace(context.Background(), p, "home", nil); !errors.Is(err, core.ErrNotOpen) {
		t.Errorf("closed workspace: err = %v, want ErrNotOpen", err)
	}
	rt.Add("notes:revier", "")
	outcome, err := newAgent(t, c, p, "notes", core.Resume{})
	if !errors.Is(err, core.ErrNoAgent) || outcome != core.AgentNotAdded {
		t.Errorf("target with no agent panel: %v, %v, want ErrNoAgent and not added", outcome, err)
	}
	if len(rt.Tabs) != 0 || len(rt.Opened) != 0 {
		t.Errorf("Tabs = %+v, Opened = %+v, want nothing", rt.Tabs, rt.Opened)
	}
}

// A runtime without tabs is named, not worked around by opening a second
// workspace.
func TestNewAgentNeedsARuntimeWithTabs(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: noTabs{rt}, Probes: []revier.AgentProbe{resumable()}}
	rt.Add("session:revier", "")

	_, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{})
	if !errors.Is(err, core.ErrNoTabsToOpen) || !strings.Contains(err.Error(), "rt") {
		t.Errorf("err = %v, want ErrNoTabsToOpen naming the runtime", err)
	}
}

// The shell key's tab: the target's declared shell alone, in the directory
// asked for, made current in the open workspace.
func TestNewShellOpensTheDeclaredShellInTheOpenWorkspace(t *testing.T) {
	c, rt, p, ref := openWorkspace(t)
	w, err := c.AgentWorkspace(context.Background(), p, "home", nil)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	dir := t.TempDir()

	if err := c.NewShell(context.Background(), w, dir); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref {
		t.Fatalf("Tabs = %+v, want one tab in %+v", rt.Tabs, ref)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: dir}})
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{rt.Tabs[0].Panel}) {
		t.Errorf("panel focuses = %v, want the new shell %s", rt.PanelFocuses, rt.Tabs[0].Panel)
	}
}

// A target that declares no shell still gets one: the runtime's own, in the
// target's directory.
func TestNewShellFallsBackToTheRuntimesShell(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	rt.Add("notes:revier", "")
	p := prepared(t, agentProject())
	w, err := c.AgentWorkspace(context.Background(), p, "notes", nil)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}

	if err := c.NewShell(context.Background(), w, ""); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	if len(rt.Tabs) != 1 {
		t.Fatalf("Tabs = %+v, want one tab", rt.Tabs)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{{Kind: revier.PanelShell, Dir: "/home/hans/dev/github/revier"}})
}

func TestNewShellNeedsARuntimeWithTabs(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: noTabs{rt}}
	rt.Add("session:revier", "")
	w, err := c.AgentWorkspace(context.Background(), prepared(t, agentProject()), "home", nil)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	if err := c.NewShell(context.Background(), w, ""); !errors.Is(err, core.ErrNoTabsToOpen) {
		t.Errorf("err = %v, want ErrNoTabsToOpen", err)
	}
}

// linkProject is a link to far on buildbox: its home the agent and the shell
// on the host, each through an ssh here, and logs a window of its own on this
// machine.
func linkProject(t *testing.T) core.Project {
	t.Helper()
	return prepared(t, revier.Project{Name: "far", Remote: &revier.Link{Host: "buildbox", Project: "far-there"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "far", Match: revier.Match{Title: "^far$"}, Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"sh", "-c", "exec ssh far", "sh"}},
			{Kind: revier.PanelShell, Command: []string{"sh", "-c", "exec ssh far", "sh"}},
		}}},
		{Name: "logs", Runtime: &revier.Realization{Name: "far-logs", Match: revier.Match{Title: "^far-logs$"},
			Panels: []revier.PanelSpec{{Kind: revier.PanelShell, Command: []string{"zsh"}}}}},
	}})
}

// An agent asked for a link opens here, as a tab of the link's workspace: the
// ssh that runs the agent on the host, with the conversation to resume as its
// arguments, since only the host can resume it.
func TestAnAgentTabOfALinkIsTheSSHPanelWithTheResume(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("far", "")
	c := &core.Core{Runtime: rt}
	w, err := c.AgentWorkspace(context.Background(), linkProject(t), "home", nil)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	outcome, err := c.NewAgent(context.Background(), w, core.Resume{Session: "abc-123", Dir: "/srv/far/wt", Harness: "claude"})
	if err != nil || outcome != core.AgentResumed {
		t.Fatalf("NewAgent = %v, %v; want resumed", outcome, err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref {
		t.Fatalf("tabs = %+v, want one in %v", rt.Tabs, ref)
	}
	got := rt.Tabs[0].Real.Panels[0]
	want := []string{"sh", "-c", "exec ssh far", "sh", "--resume", "abc-123", "--dir", "/srv/far/wt"}
	if !slices.Equal(got.Command, want) || got.Dir != "" {
		t.Errorf("agent panel = %+v, want %q and no directory here", got, want)
	}

	// A word a shell would read is not sent: the agent starts empty.
	outcome, err = c.NewAgent(context.Background(), w, core.Resume{Session: "abc; rm -rf /"})
	if err != nil || outcome != core.AgentEmpty {
		t.Errorf("NewAgent = %v, %v; want empty", outcome, err)
	}
	if got := rt.Tabs[1].Real.Panels[0].Command; !slices.Equal(got, want[:4]) {
		t.Errorf("agent panel = %q, want %q", got, want[:4])
	}
}

// noTabs is a runtime without the PanelOpener capability.
type noTabs struct{ rt *hosttest.FakeRuntime }

func (n noTabs) Name() string                                        { return n.rt.Name() }
func (n noTabs) Probe(ctx context.Context) error                     { return n.rt.Probe(ctx) }
func (n noTabs) Capabilities() revier.Capabilities                   { return n.rt.Capabilities() }
func (n noTabs) Focus(ctx context.Context, r revier.TargetRef) error { return n.rt.Focus(ctx, r) }
func (n noTabs) Focused(ctx context.Context) (revier.TargetRef, error) {
	return n.rt.Focused(ctx)
}
func (n noTabs) Instances(ctx context.Context) ([]revier.Instance, error) {
	return n.rt.Instances(ctx)
}
func (n noTabs) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	return n.rt.Open(ctx, r)
}

// On a runtime without tabs, a restore still opens the workspace with its
// declared agents, and names the agents past them as not restored.
func TestRestoreDropsTheAgentsPastTheLayoutWithoutTabs(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: noTabs{rt}, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())
	resumes := []core.Resume{{Harness: "claude", Session: "a"}, {Harness: "claude", Session: "b"}}

	res, err := c.GoResuming(context.Background(), p, "home", nil, resumes)
	if err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	want := []core.AgentOutcome{core.AgentResumed, core.AgentDropped}
	if !slices.Equal(res.Agents, want) {
		t.Errorf("Agents = %v, want %v", res.Agents, want)
	}
	if dry := c.Resumes(p, "home", resumes); !slices.Equal(dry, want) {
		t.Errorf("dry run = %v, want %v", dry, want)
	}
}

// twoWorkspaces is agentProject and a second project, each with its home
// open, holding the given panels.
func twoWorkspaces(t *testing.T, revierPanels, otherPanels []revier.Panel) (*core.Core, *hosttest.FakeRuntime, []core.Project, revier.TargetRef, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	other := agentProject()
	other.Name, other.Path = "other", "/home/hans/dev/other"
	other.Targets[0].Runtime.Name, other.Targets[0].Runtime.Match.Title = "session:other", "^session:other$"
	projects := []core.Project{prepared(t, agentProject()), prepared(t, other)}
	a := rt.Add("session:revier", "", revierPanels...)
	b := rt.Add("session:other", "", otherPanels...)
	return c, rt, projects, a, b
}

// The key knows only the window it was pressed in. The workspace holding that
// panel is found, with its project and target.
func TestPanelOwnerFindsTheWorkspaceHoldingAPanel(t *testing.T) {
	c, _, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}, {ID: "2"}}, []revier.Panel{{ID: "7"}})

	w, err := c.PanelOwner(context.Background(), projects, nil, "7")
	if err != nil {
		t.Fatalf("PanelOwner: %v", err)
	}
	if w.Project.Name != "other" || w.Target != "home" || w.Ref != other {
		t.Errorf("owner = %s:%s %+v, want other:home %+v", w.Project.Name, w.Target, w.Ref, other)
	}
}

// One instance backs two targets, and only the second declares an agent
// panel. The key opens its tab from that one, not from the first target in
// the file, which has no tab to give.
func TestPanelOwnerTakesTheTargetThatDeclaresAnAgent(t *testing.T) {
	proj := agentProject()
	plain := *proj.Targets[1].Runtime
	plain.Match.Title = proj.Targets[0].Runtime.Match.Title
	proj.Targets[1].Runtime = &plain
	proj.Targets[0], proj.Targets[1] = proj.Targets[1], proj.Targets[0]
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	ref := rt.Add("session:revier", "", revier.Panel{ID: "5"})

	w, err := c.PanelOwner(context.Background(), []core.Project{prepared(t, proj)}, nil, "5")
	if err != nil {
		t.Fatalf("PanelOwner: %v", err)
	}
	if w.Target != "home" || w.Ref != ref {
		t.Errorf("owner = %s %+v, want home %+v", w.Target, w.Ref, ref)
	}
	if _, err := c.NewAgent(context.Background(), w, core.Resume{}); err != nil {
		t.Errorf("NewAgent: %v, want the tab opened", err)
	}
}

// Two workspaces holding one id - one per kitty process - on a runtime that
// cannot say which the id means are refused rather than guessed at.
func TestPanelOwnerRefusesAnIDTwoWorkspacesHold(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	if _, err := c.PanelOwner(context.Background(), projects, nil, "1"); !errors.Is(err, core.ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}

// finding is a runtime that says which instance a panel id means, as kitty
// does from the process the command was started in.
type finding struct {
	*hosttest.FakeRuntime
	ref revier.TargetRef
}

func (f finding) FindPanel([]revier.Instance, revier.PanelID) (revier.TargetRef, error) {
	return f.ref, nil
}

// A runtime that says which instance an id means decides between two
// workspaces holding it; one that finds nothing is named.
func TestPanelOwnerTakesTheInstanceTheRuntimeFinds(t *testing.T) {
	_, rt, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	c := &core.Core{Runtime: finding{rt, other}}
	if w, err := c.PanelOwner(context.Background(), projects, nil, "1"); err != nil || w.Project.Name != "other" || w.Ref != other {
		t.Errorf("owner = %s %+v, %v, want other %+v", w.Project.Name, w.Ref, err, other)
	}
	c = &core.Core{Runtime: finding{rt, revier.TargetRef{}}}
	if _, err := c.PanelOwner(context.Background(), projects, nil, "1"); !errors.Is(err, core.ErrNoPanel) {
		t.Errorf("nothing found: err = %v, want ErrNoPanel", err)
	}
	c = &core.Core{Runtime: finding{rt, rt.Add("a kitty window of no project", "")}}
	if _, err := c.PanelOwner(context.Background(), projects, nil, "1"); err == nil || !strings.Contains(err.Error(), "no project") {
		t.Errorf("found in no workspace: err = %v, want it named", err)
	}
}

// An id no workspace holds is named.
func TestPanelOwnerNamesAnUnknownPanel(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)

	_, err := c.PanelOwner(context.Background(), projects, nil, "42")
	if !errors.Is(err, core.ErrNoPanel) || !strings.Contains(err.Error(), "42") {
		t.Errorf("err = %v, want ErrNoPanel naming 42", err)
	}
}

// -p names a project; the target is the one that declares an agent panel.
func TestAgentTargetIsTheTargetDeclaringAnAgent(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	name, err := c.AgentTarget(prepared(t, agentProject()))
	if err != nil || name != "home" {
		t.Errorf("AgentTarget = %q, %v, want home", name, err)
	}

	two := agentProject()
	second := *two.Targets[0].Runtime
	second.Name, second.Match.Title = "second", "^second$"
	two.Targets[1].Runtime = &second
	if _, err := c.AgentTarget(prepared(t, two)); err == nil {
		t.Error("two targets with an agent panel: want an error asking for one")
	}
}

// A key pressed in a kitty workspace on GNOME raises the OS window around the
// new agent through the window host. kitty's own focus of an unfocused OS
// window is an "is ready" notice, not the window.
func TestNewAgentRaisesTheOSWindow(t *testing.T) {
	rt, wm, homeWm, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{resumable()}}

	if _, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{}); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != homeWm {
		t.Errorf("window focuses = %v, want the workspace's OS window %v raised", wm.Focuses, homeWm)
	}
}

// With no OS window to raise, nothing opens: the tab would come up behind, and
// its focus would be the notice D63 refuses.
func TestNewAgentOpensNothingItCannotRaise(t *testing.T) {
	rt, _, _, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: hosttest.New("wm"), Probes: []revier.AgentProbe{resumable()}}

	outcome, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{})
	if !errors.Is(err, core.ErrUnraisable) || outcome != core.AgentNotAdded {
		t.Errorf("%v, %v, want ErrUnraisable and not added", outcome, err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("Tabs = %+v, want none opened", rt.Tabs)
	}
}

// A restore lays a recorded conversation over the declared agent panel only
// when that panel runs its harness. A claude conversation over an opencode
// panel would start `opencode --resume <claude id>`: the agent starts empty
// instead, and the restore says why.
func TestRestoreDoesNotResumeAConversationIntoAnotherHarness(t *testing.T) {
	proj := agentProject()
	proj.Targets[0].Runtime.Panels[1] = revier.PanelSpec{Kind: revier.PanelAgent, Title: "opencode", Command: []string{"opencode"}}
	c := &core.Core{Probes: []revier.AgentProbe{resumable(), &hosttest.FakeProbe{Harness: "opencode", Marker: "opencode"}}}

	resumes := []core.Resume{{Harness: "claude", Session: "abc-123"}}
	panels := launched(t, c, proj, resumes)
	if got := panels[1].Command; !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("agent command = %q, want opencode as declared", got)
	}
	if got := c.Resumes(prepared(t, proj), "home", resumes); !slices.Equal(got, []core.AgentOutcome{core.AgentUnresumable}) {
		t.Errorf("outcomes = %v, want unresumable", got)
	}
}
