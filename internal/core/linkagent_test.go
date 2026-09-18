package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// hostAgent is one agent as a link's host reports it: under the tag its panel
// on the other machine gave it, in the instance of the served processes.
func hostAgent(tag string, status revier.Status) revier.AgentView {
	return revier.AgentView{
		Panel: revier.PanelID(tag),
		Ref:   revier.TargetRef{Host: "proc", ID: "session:far-there"},
		State: revier.AgentState{Harness: "claude", Status: status},
	}
}

// linked is the link's workspace open here with one ssh panel on pid 4242, and
// the host answering with the agents given.
func linked(t *testing.T, agents ...revier.AgentView) (*core.Core, *hosttest.FakeRuntime, *hosttest.FakeRemote, revier.TargetRef) {
	t.Helper()
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{Project: revier.Project{Name: "far-there"}, PathExists: true, Agents: agents})
	rt := hosttest.NewRuntime("rt")
	pane := rt.Add("far", "",
		revier.Panel{ID: "9", Kind: revier.PanelTool, PID: 4242, Title: "fixing the build", Command: []string{"ssh", "-t", "buildbox"}},
		revier.Panel{ID: "10", Kind: revier.PanelTool, PID: 4250, Command: []string{"ssh", "-t", "buildbox"}})
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	return c, rt, remote, pane
}

// A machine serves a project's agent to a terminal elsewhere: no runtime here
// holds the process, and the served processes list it under the workspace's
// name. The survey probes it beside whatever the runtime holds, and on a
// machine with no runtime at all.
func TestASurveyReportsTheAgentsServedToAnotherMachine(t *testing.T) {
	served := hosttest.New("proc")
	ref := served.Add("session:revier", "", revier.Panel{ID: "box.4242", Kind: revier.PanelTool, Title: "agent", PID: 7})
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "agent", State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}
	p := prepared(t, project())

	for name, rt := range map[string]revier.Runtime{"beside a runtime": hosttest.NewRuntime("rt"), "with no runtime": nil} {
		c := &core.Core{Runtime: rt, Served: served, Probes: []revier.AgentProbe{probe}}
		agents := agentsOf(t, c, p)
		if len(agents) != 1 || agents[0].Panel != "box.4242" || agents[0].Ref != ref || agents[0].State.Status != revier.StatusRunning {
			t.Errorf("%s: agents = %+v, want box.4242 running in %v", name, agents, ref)
		}
		a, err := c.Agent(context.Background(), p, "box.4242", nil)
		if err != nil || a.Ref != ref {
			t.Errorf("%s: Agent = %+v, %v; want the served one", name, a, err)
		}
	}
}

// The activity line is the panel's title here: it crosses the ssh, and the
// process on the host has none.
func TestALinksAgentTakesItsActivityFromThePanelHere(t *testing.T) {
	c, _, _, _ := linked(t, hostAgent("box.4242", revier.StatusRunning))
	c.Probes = []revier.AgentProbe{activityProbe{}}
	if a := agentsOf(t, c, linkProject(t))[0]; a.State.Activity != "fixing the build" || a.State.Status != revier.StatusRunning {
		t.Errorf("state = %+v, want the panel's title and the host's status", a.State)
	}
}

// activityProbe reads the activity from the title and knows no status, as the
// Claude probe does for a panel whose process is not on this machine.
type activityProbe struct{}

func (activityProbe) Name() string            { return "claude" }
func (activityProbe) Match(revier.Panel) bool { return false }
func (activityProbe) Inspect(_ context.Context, p revier.Panel) (revier.AgentState, error) {
	return revier.AgentState{Harness: "claude", Activity: p.Title}, nil
}

// A prompt for a link's agent is typed into the panel here. Whether it may be
// is judged from the state the host reports, and the turn it starts is seen
// by asking the host again.
func TestAPromptForALinksAgentIsTypedIntoThePanelHere(t *testing.T) {
	c, rt, remote, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	p := linkProject(t)
	a, err := c.Agent(context.Background(), p, "", nil)
	if err != nil || a.Ref != pane || a.Panel.ID != "9" {
		t.Fatalf("Agent = %+v, %v; want panel 9 of %v", a, err, pane)
	}
	rt.OnSend = func(revier.PanelID, string) {
		remote.Views[0].Agents = []revier.AgentView{hostAgent("box.4242", revier.StatusRunning)}
	}
	state, err := c.Prompt(context.Background(), a, "run the tests", time.Millisecond)
	if err != nil || state.Status != revier.StatusRunning {
		t.Fatalf("Prompt = %+v, %v; want the turn seen running", state, err)
	}
	if len(rt.Sent) != 2 || rt.Sent[0].Panel != "9" || rt.Sent[0].Text != "run the tests" || rt.Sent[1].Text != "\r" {
		t.Errorf("sent = %+v, want the text and an Enter into panel 9", rt.Sent)
	}

	remote.Views[0].Agents = []revier.AgentView{hostAgent("box.4242", revier.StatusAttention)}
	waiting, err := c.Agent(context.Background(), p, "9", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(context.Background(), waiting, "again", time.Millisecond); !errors.Is(err, core.ErrAttention) {
		t.Errorf("err = %v, want ErrAttention from the host's state", err)
	}
}

// An agent nothing here shows cannot be driven from here, and is told from a
// link with no agent at all.
func TestAnAgentShownNowhereHereIsNotFound(t *testing.T) {
	c, _, _, _ := linked(t, hostAgent("laptop.77", revier.StatusIdle))
	if _, err := c.Agent(context.Background(), linkProject(t), "", nil); !errors.Is(err, core.ErrAgentElsewhere) {
		t.Errorf("err = %v, want ErrAgentElsewhere", err)
	}
	c, _, _, _ = linked(t)
	if _, err := c.Agent(context.Background(), linkProject(t), "", nil); !errors.Is(err, core.ErrNoAgent) {
		t.Errorf("err = %v, want ErrNoAgent", err)
	}
}

// Closing the panel that shows a link's agent ends the ssh and the agent with
// it, so a shutdown of the agents closes that panel, names a busy one, and
// leaves an agent no panel here shows to its host.
func TestAShutdownClosesThePanelsThatShowALinksAgents(t *testing.T) {
	c, _, _, pane := linked(t, hostAgent("box.4242", revier.StatusRunning), hostAgent("laptop.77", revier.StatusIdle))
	projects := []core.Project{linkProject(t)}
	report, err := c.Survey(context.Background(), projects, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := c.ShutdownPlan(report, "", core.ShutdownAgents)
	if len(plan) != 1 || plan[0].Ref != pane || plan[0].Panel != "9" || !plan[0].Busy() {
		t.Errorf("plan = %+v, want panel 9 of %v alone, busy", plan, pane)
	}
}

// A save records a link's agents as its host names them, each under the panel
// here that shows it and in the panels' order, so a restore resumes them on
// the host.
func TestASaveRecordsALinksConversationsFromItsHost(t *testing.T) {
	c, _, remote, _ := linked(t, hostAgent("box.4250", revier.StatusIdle), hostAgent("box.4242", revier.StatusIdle))
	second, first := hostAgent("box.4250", revier.StatusIdle), hostAgent("box.4242", revier.StatusIdle)
	first.Conversation = &revier.Conversation{ID: "abc-123", Dir: "/srv/far/wt"}
	remote.Named = []revier.ProjectView{{Project: revier.Project{Name: "far-there"}, Agents: []revier.AgentView{second, first}}}

	report, err := c.Survey(context.Background(), []core.Project{linkProject(t)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps := c.Session(context.Background(), report, "far")
	if len(s.Projects) != 1 || len(s.Projects[0].Targets) == 0 {
		t.Fatalf("session = %+v, want the link's home", s)
	}
	agents := s.Projects[0].Targets[0].Agents
	if len(agents) != 2 || agents[0].Session != "abc-123" || agents[0].Dir != "/srv/far/wt" || agents[0].Harness != "claude" || agents[1].Session != "" {
		t.Errorf("agents = %+v, want abc-123 in /srv/far/wt first, then one with no conversation", agents)
	}
	if gaps.Unnamed != 1 || len(gaps.Failed) != 0 {
		t.Errorf("gaps = %+v, want one agent unnamed", gaps)
	}

	remote.Err = errors.New("buildbox: connection refused")
	if _, gaps := c.Session(context.Background(), report, "far"); len(gaps.Failed) != 1 {
		t.Errorf("gaps = %+v, want the host's failure said", gaps)
	}
}

// The answer a host gives a save: the conversation of each agent it surveyed.
func TestNameConversationsFillsInWhatEachAgentHolds(t *testing.T) {
	served := hosttest.New("proc")
	served.Add("session:revier", "", revier.Panel{ID: "box.4242", Kind: revier.PanelTool, Title: "claude", Vars: map[string]string{"session": "abc-123", "dir": "/srv/wt"}})
	c := &core.Core{Served: served, Probes: []revier.AgentProbe{resumable()}}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.NameConversations(context.Background(), report)
	got := report.Views[0].Agents[0].Conversation
	if got == nil || got.ID != "abc-123" || got.Dir != "/srv/wt" {
		t.Errorf("conversation = %+v, want abc-123 in /srv/wt", got)
	}
}

// What `revier agent exec` becomes is the project's agent panel as a restore
// would start it, named for the workspace it belongs to; `revier shell exec`
// the declared shell.
func TestServeStartsTheDeclaredPanels(t *testing.T) {
	dir := t.TempDir()
	p := prepared(t, revier.Project{Name: "demo", Path: dir, Targets: []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
		Name: "session:demo", Dir: dir, Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude", "--model", "opus"}},
			{Kind: revier.PanelShell, Command: []string{"zsh", "-l"}},
		}}}}})
	c := &core.Core{Probes: []revier.AgentProbe{resumable()}}

	agent, err := c.ServeAgent(p, core.Resume{Session: "abc-123", Dir: dir})
	want := []string{"claude", "--model", "opus", "--resume", "abc-123"}
	if err != nil || agent.Workspace != "session:demo" || agent.Dir != dir || !slices.Equal(agent.Argv, want) || agent.Outcome != core.AgentResumed {
		t.Errorf("ServeAgent = %+v, %v; want %q in %s for session:demo", agent, err, want, dir)
	}
	gone, err := c.ServeAgent(p, core.Resume{Session: "abc-123", Dir: dir + "/gone"})
	if err != nil || gone.Outcome != core.AgentDirGone || !slices.Equal(gone.Argv, want[:3]) || gone.Dir != dir {
		t.Errorf("ServeAgent = %+v, %v; want the agent empty in the project", gone, err)
	}
	shell, err := c.ServeShell(p, "")
	if err != nil || shell.Workspace != "session:demo" || shell.Dir != dir || !slices.Equal(shell.Argv, []string{"zsh", "-l"}) {
		t.Errorf("ServeShell = %+v, %v; want zsh -l in %s", shell, err, dir)
	}
}
