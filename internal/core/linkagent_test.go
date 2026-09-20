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

// activityProbe reads the activity from a title and nothing from a panel, as
// the Claude probe does for a panel whose process is not on this machine.
type activityProbe struct{}

func (activityProbe) Name() string            { return "claude" }
func (activityProbe) Match(revier.Panel) bool { return false }
func (activityProbe) Inspect(context.Context, revier.Panel) (revier.AgentState, error) {
	return revier.AgentState{}, errors.New("no claude here")
}
func (activityProbe) Activity(title string) string { return title }

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
// link with no agent at all, and from a panel here the host lists no agent in.
func TestAnAgentShownNowhereHereIsNotFound(t *testing.T) {
	c, _, _, _ := linked(t, hostAgent("laptop.77", revier.StatusIdle))
	if _, err := c.Agent(context.Background(), linkProject(t), "", nil); !errors.Is(err, core.ErrAgentElsewhere) {
		t.Errorf("err = %v, want ErrAgentElsewhere", err)
	}
	if _, err := c.Agent(context.Background(), linkProject(t), "10", nil); !errors.Is(err, core.ErrNotAgent) {
		t.Errorf("err = %v, want ErrNotAgent for the shell panel", err)
	}
	c, _, _, _ = linked(t)
	if _, err := c.Agent(context.Background(), linkProject(t), "", nil); !errors.Is(err, core.ErrNoAgent) {
		t.Errorf("err = %v, want ErrNoAgent", err)
	}
}

// A link's agent is addressed by the target whose instance shows it, as a
// local one is: the home for the agent in the link's workspace, and a local
// window of the link holds none of the host's agents.
func TestALinksAgentIsAddressedByItsTarget(t *testing.T) {
	c, rt, _, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	rt.Add("far-logs", "", revier.Panel{ID: "20", Kind: revier.PanelShell, PID: 5000})
	p := linkProject(t)
	a, err := c.Agent(context.Background(), p, "home", nil)
	if err != nil || a.Ref != pane || a.Panel.ID != "9" {
		t.Errorf("Agent(far:home) = %+v, %v; want panel 9 of %v", a, err, pane)
	}
	if _, err := c.Agent(context.Background(), p, "logs", nil); !errors.Is(err, core.ErrNoAgent) {
		t.Errorf("Agent(far:logs) err = %v, want ErrNoAgent", err)
	}
}

// A host with a terminal of its own reports the agents in it too, in a runtime
// that may be named as the one here. Only an agent a panel here shows is
// here: the host's own is reached from nowhere here, and a shutdown leaves it
// alone rather than closing a panel of the runtime here by the host's ids.
func TestTheHostsOwnAgentIsNotTakenForOneHere(t *testing.T) {
	theirs := revier.AgentView{Panel: "3", Ref: revier.TargetRef{Host: "kitty", ID: "unix:@kitty-999/1"}, State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}}
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{Project: revier.Project{Name: "far-there"}, PathExists: true, Agents: []revier.AgentView{theirs}})
	rt := hosttest.NewRuntime("kitty")
	rt.Add("far", "", revier.Panel{ID: "3", Kind: revier.PanelTool, PID: 4242})
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	p := linkProject(t)

	agents := agentsOf(t, c, p)
	if len(agents) != 1 || agents[0].Panel != "3" || !agents[0].Ref.IsZero() {
		t.Fatalf("agents = %+v, want the host's under its own name and no ref here", agents)
	}
	if _, err := c.GoAgent(context.Background(), p, agents[0], nil); !errors.Is(err, core.ErrAgentElsewhere) {
		t.Errorf("GoAgent err = %v, want ErrAgentElsewhere", err)
	}
	if _, err := c.Agent(context.Background(), p, "", nil); !errors.Is(err, core.ErrAgentElsewhere) {
		t.Errorf("Agent err = %v, want ErrAgentElsewhere", err)
	}
	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan := c.ShutdownPlan(report, "", core.ShutdownAgents); len(plan) != 0 {
		t.Errorf("plan = %+v, want nothing closed here for the host's own agent", plan)
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

// An agent this machine serves to a terminal elsewhere ends with that
// terminal (decisions.md D84): a shutdown here plans nothing for it, and no
// busy check names it.
func TestAShutdownLeavesAServedAgentToItsTerminal(t *testing.T) {
	served := hosttest.New("proc")
	served.Add("session:revier", "", revier.Panel{ID: "box.4242", Kind: revier.PanelTool, Title: "agent", PID: 7})
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "agent", State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Served: served, Probes: []revier.AgentProbe{probe}}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Views[0].Agents) != 1 {
		t.Fatalf("agents = %+v, want the served one surveyed", report.Views[0].Agents)
	}
	if plan := c.ShutdownPlan(report, "", core.ShutdownAgents); len(plan) != 0 {
		t.Errorf("plan = %+v, want nothing to close here", plan)
	}
}

// Two links to one project on one host are handed one answer, and each
// names the host's agents by its own panels: the second must not undo the
// first, and neither reads the other's agents as elsewhere.
func TestTwoLinksToOneProjectEachKeepTheirAgents(t *testing.T) {
	c, _, _, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	alt := linkProject(t)
	alt.Name = "far-alt"
	report, err := c.Survey(context.Background(), []core.Project{linkProject(t), alt}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range report.Views {
		if len(v.Agents) != 1 || v.Agents[0].Ref != pane || v.Agents[0].Panel != "9" {
			t.Errorf("%s: agents = %+v, want the one in panel 9 of %v", v.Project.Name, v.Agents, pane)
		}
	}
}

// A target its file refused is not served: the realization it keeps is as
// written, not as rendered, so nothing may run from it (decisions.md D85),
// and the reason is what the panel on the other machine prints.
func TestServeSkipsARefusedTarget(t *testing.T) {
	p := prepared(t, revier.Project{Name: "demo", Path: "/srv/demo", Targets: []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
		Name: "session:demo", Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"claude", "--add-dir", "{{.Vars.extra}}"}},
			{Kind: revier.PanelShell},
		}}}}})
	if p.TargetErr(0) == nil {
		t.Fatal("the target should be refused for the key it renders")
	}
	c := &core.Core{}
	if _, err := c.ServeAgent(p, core.Resume{}); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("ServeAgent = %v, want the refusal, naming the key that is missing", err)
	}
	if _, err := c.ServeShell(p, ""); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("ServeShell = %v, want the refusal", err)
	}
}

// A conversation reaches `revier agent exec` by its id alone, so the harness
// is the panel's, as an agent tab takes it: a conversation is not resumed
// into a panel that runs another harness, which would start
// `opencode --resume <claude id>`.
func TestServeResumesOnlyIntoThePanelsOwnHarness(t *testing.T) {
	p := prepared(t, revier.Project{Name: "demo", Path: "/srv/demo", Targets: []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
		Name: "session:demo", Match: revier.Match{Title: "^session:demo$"},
		Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Title: "opencode", Command: []string{"opencode"}}}}}}})
	c := &core.Core{Probes: []revier.AgentProbe{resumable(), &hosttest.FakeProbe{Harness: "opencode", Marker: "opencode"}}}
	s, err := c.ServeAgent(p, core.Resume{Session: "abc-123"})
	if err != nil || s.Outcome != core.AgentUnresumable || !slices.Equal(s.Argv, []string{"opencode"}) {
		t.Errorf("ServeAgent = %+v, %v; want the agent started empty, its command as declared", s, err)
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

// A terminal attached to a link by hand holds an agent of this machine. It is
// probed like any attachment, and the host's answer adds to it rather than
// replacing it, so the view shows both and a shutdown sees the busy one
// (decisions.md D101).
func TestALinkKeepsTheAgentInAnAttachedTerminal(t *testing.T) {
	c, rt, _, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	c.Probes = []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}}
	term := rt.Add("scratch", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	p := linkProject(t)

	report, err := c.Survey(context.Background(), []core.Project{p}, nil,
		map[revier.ProjectName][]revier.TargetRef{p.Name: {term}})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	agents := report.Views[0].Agents
	if len(agents) != 2 {
		t.Fatalf("agents = %+v, want the attached terminal's and the host's", agents)
	}
	if agents[0].Ref != term || agents[0].State.Status != revier.StatusRunning {
		t.Errorf("first agent = %+v, want the running one in the attached terminal", agents[0])
	}
	if agents[1].Panel != "9" || agents[1].State.Status != revier.StatusIdle {
		t.Errorf("second agent = %+v, want the host's, on the panel here that shows it", agents[1])
	}

	s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownAll), term)
	if !ok || !s.Busy() {
		t.Errorf("step = %+v, %v; want the attached terminal marked busy", s, ok)
	}
}

// A link whose host stops answering between the plan and the close reports no
// agent, and no agent read is not idle: the close is refused rather than
// ending a busy agent on the far side unasked.
func TestShutdownRefusesALinkWhoseHostStoppedAnswering(t *testing.T) {
	c, rt, remote, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	p := linkProject(t)
	projects := []core.Project{p}

	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	s, ok := stepFor(plan, pane)
	if !ok || len(s.Agents) != 1 {
		t.Fatalf("plan = %+v, want the workspace with the host's idle agent", plan)
	}

	remote.Err = errors.New("connection refused")
	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects})
	if err == nil || out != nil || len(rt.Closed) != 0 {
		t.Fatalf("shutdown = %+v, %v, closed %v; want a refusal with nothing closed", out, err, rt.Closed)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want the host's failure named", err)
	}
}
