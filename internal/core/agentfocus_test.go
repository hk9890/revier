package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// agentsOf is the agents a survey reports for the one project.
func agentsOf(t *testing.T, c *core.Core, p core.Project) []revier.AgentView {
	t.Helper()
	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return report.Views[0].Agents
}

// An agent on this machine is reached by its tab: its instance is focused, the
// tab becomes current and the OS window holding it is raised. On tmux the
// instance's focus is what switches a terminal showing another session.
func TestGoAgentFocusesItsTabAndRaisesTheWindow(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	workspace := rt.Add("session:revier", "kitty", shellPanel("1"), agentPanel("2", "idle"), agentPanel("3", "busy"))
	wm := hosttest.New("wm")
	osw := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{titleProbe{}}}
	p := prepared(t, project())

	if _, err := c.GoAgent(context.Background(), p, agentsOf(t, c, p)[1], nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(rt.Focuses) != 1 || rt.Focuses[0] != workspace {
		t.Errorf("runtime focuses = %v, want the workspace %v", rt.Focuses, workspace)
	}
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != "3" {
		t.Errorf("panel focuses = %v, want panel 3", rt.PanelFocuses)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != osw {
		t.Errorf("window focuses = %v, want the workspace raised", wm.Focuses)
	}
}

// Two instances of one project, as two kitty processes, can each hold a panel
// 1. The survey reports each agent with its instance, and going to one focuses
// that one, where a lookup by panel id alone was ambiguous (decisions.md D75).
func TestGoAgentReachesOneOfTwoAgentsThatShareAPanelID(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	diff := rt.Add("diff:revier", "kitty", agentPanel("1", "busy"))
	p := prepared(t, project())

	agents := agentsOf(t, c, p)
	if len(agents) != 2 || agents[1].Ref != diff {
		t.Fatalf("agents = %+v, want two, the second in %v", agents, diff)
	}
	if _, err := c.GoAgent(context.Background(), p, agents[1], nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if got, _ := rt.FocusedPanel(context.Background(), diff); got != "1" {
		t.Errorf("focused panel in %v = %q, want panel 1 there", diff, got)
	}
	if got, _ := rt.FocusedPanel(context.Background(), agents[0].Ref); got != "" {
		t.Errorf("focused panel in %v = %q, want the other workspace untouched", agents[0].Ref, got)
	}
}

// An agent whose panel closed since the survey is said to be gone, and nothing
// is focused.
func TestGoAgentRefusesAnAgentThatIsGone(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	p := prepared(t, project())
	a := agentsOf(t, c, p)[0]
	a.Panel = "9"

	if _, err := c.GoAgent(context.Background(), p, a, nil); !errors.Is(err, core.ErrAgentGone) {
		t.Errorf("err = %v, want ErrAgentGone", err)
	}
	if len(rt.PanelFocuses) != 0 {
		t.Errorf("panel focuses = %v, want none", rt.PanelFocuses)
	}
}

// A runtime that cannot focus a panel still brings the user to the instance
// the agent is in.
func TestGoAgentWithoutTabsFocusesTheInstance(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:revier", "kitty", agentPanel("2", "idle"))
	c := &core.Core{Runtime: bareRuntime{rt}, Probes: []revier.AgentProbe{titleProbe{}}}
	p := prepared(t, project())

	if _, err := c.GoAgent(context.Background(), p, agentsOf(t, c, p)[0], nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(rt.Focuses) != 1 || rt.Focuses[0] != ref {
		t.Errorf("focuses = %v, want the instance %v", rt.Focuses, ref)
	}
}

// A link's agent runs on its host and is shown by a panel here. The host
// reports it under the tag that panel gave it - this machine's name and the
// panel's pid - and the survey names it by the panel, so going to it is a focus
// of that panel here and asks the host nothing.
func TestALinksAgentIsReachedInThePanelThatShowsIt(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{
		Project: revier.Project{Name: "far-there"}, PathExists: true,
		Agents: []revier.AgentView{
			{Panel: "box.4242", Ref: revier.TargetRef{Host: "proc", ID: "session:far-there"}, State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention}},
			{Panel: "laptop.77", Ref: revier.TargetRef{Host: "proc", ID: "session:far-there"}, State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}},
		},
	})
	rt := hosttest.NewRuntime("rt")
	pane := rt.Add("far", "", revier.Panel{ID: "9", Kind: revier.PanelTool, PID: 4242, Command: []string{"ssh", "-t", "buildbox"}})
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	p := linkProject(t)

	agents := agentsOf(t, c, p)
	if len(agents) != 2 || agents[0].Panel != "9" || agents[0].Ref != pane || agents[0].State.Status != revier.StatusAttention {
		t.Fatalf("agents = %+v, want the first in panel 9 of %v, waiting", agents, pane)
	}
	if _, err := c.GoAgent(context.Background(), p, agents[0], nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != "9" {
		t.Errorf("panel focuses = %v, want panel 9", rt.PanelFocuses)
	}

	// The second was started from another machine: nothing here shows it.
	if _, err := c.GoAgent(context.Background(), p, agents[1], nil); !errors.Is(err, core.ErrAgentElsewhere) {
		t.Errorf("err = %v, want ErrAgentElsewhere", err)
	}
}
