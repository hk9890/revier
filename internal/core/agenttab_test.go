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

// The key's tab: the project's agent panel on the conversation asked for, and
// the project's shell, both in the directory asked for, opened in the open
// workspace, with the new agent made current.
func TestNewAgentOpensAnAgentTabInTheOpenWorkspace(t *testing.T) {
	c, rt, p, ref := openWorkspace(t)
	dir := t.TempDir()

	outcome, err := c.NewAgent(context.Background(), p, "home", ref, core.Resume{Session: "abc-123", Dir: dir})
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
	c, rt, p, ref := openWorkspace(t)

	outcome, err := c.NewAgent(context.Background(), p, "home", ref, core.Resume{})
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
	ref := rt.Add("session:revier", "")

	outcome, err := c.NewAgent(context.Background(), prepared(t, proj), "home", ref, core.Resume{Session: "abc-123"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentEmpty {
		t.Errorf("outcome = %v, want empty", outcome)
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
	ref := rt.Add("notes:revier", "")
	outcome, err := c.NewAgent(context.Background(), p, "notes", ref, core.Resume{})
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
	ref := rt.Add("session:revier", "")

	_, err := c.NewAgent(context.Background(), prepared(t, agentProject()), "home", ref, core.Resume{})
	if !errors.Is(err, core.ErrNoAgentTabs) || !strings.Contains(err.Error(), "rt") {
		t.Errorf("err = %v, want ErrNoAgentTabs naming the runtime", err)
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

	p, target, ref, err := c.PanelOwner(context.Background(), projects, nil, "7")
	if err != nil {
		t.Fatalf("PanelOwner: %v", err)
	}
	if p.Name != "other" || target != "home" || ref != other {
		t.Errorf("owner = %s:%s %+v, want other:home %+v", p.Name, target, ref, other)
	}
}

// Two workspaces holding one id - one per kitty process - on a runtime that
// cannot say which the id means are refused rather than guessed at.
func TestPanelOwnerRefusesAnIDTwoWorkspacesHold(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	if _, _, _, err := c.PanelOwner(context.Background(), projects, nil, "1"); !errors.Is(err, core.ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}

// finding is a runtime that says which instance a panel id means, as kitty
// does from the process the command was started in.
type finding struct {
	*hosttest.FakeRuntime
	ref revier.TargetRef
}

func (f finding) FindPanel(context.Context, revier.PanelID) (revier.TargetRef, error) {
	return f.ref, nil
}

// A runtime that says which instance an id means decides between two
// workspaces holding it; one that finds nothing is named.
func TestPanelOwnerTakesTheInstanceTheRuntimeFinds(t *testing.T) {
	_, rt, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	c := &core.Core{Runtime: finding{rt, other}}
	if p, _, ref, err := c.PanelOwner(context.Background(), projects, nil, "1"); err != nil || p.Name != "other" || ref != other {
		t.Errorf("owner = %s %+v, %v, want other %+v", p.Name, ref, err, other)
	}
	c = &core.Core{Runtime: finding{rt, revier.TargetRef{}}}
	if _, _, _, err := c.PanelOwner(context.Background(), projects, nil, "1"); !errors.Is(err, core.ErrNoPanel) {
		t.Errorf("nothing found: err = %v, want ErrNoPanel", err)
	}
	c = &core.Core{Runtime: finding{rt, rt.Add("a kitty window of no project", "")}}
	if _, _, _, err := c.PanelOwner(context.Background(), projects, nil, "1"); err == nil || !strings.Contains(err.Error(), "no project") {
		t.Errorf("found in no workspace: err = %v, want it named", err)
	}
}

// An id no workspace holds is named.
func TestPanelOwnerNamesAnUnknownPanel(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)

	_, _, _, err := c.PanelOwner(context.Background(), projects, nil, "42")
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
