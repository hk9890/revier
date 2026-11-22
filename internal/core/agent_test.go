package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

func TestAgentIsTheProjectsOnlyAgent(t *testing.T) {
	c, _ := agentCore(shellPanel("1"), agentPanel("2", "idle"))

	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	if a.Panel.ID != "2" || a.State.Status != revier.StatusIdle {
		t.Errorf("agent = %+v, want panel 2, idle", a)
	}
}

// Two agents cannot be told apart by the project name, so the address must
// name one, and the refusal says how.
func TestAgentRefusesAnAmbiguousProject(t *testing.T) {
	c, _ := agentCore(agentPanel("1", "idle"), agentPanel("2", "busy"))
	p := prepared(t, project())

	_, err := c.Agent(context.Background(), p, "")
	if !errors.Is(err, core.ErrAmbiguous) || !strings.Contains(err.Error(), "revier:home:1, revier:home:2") {
		t.Fatalf("err = %v, want ErrAmbiguous naming revier:home:1 and revier:home:2", err)
	}
	a, err := c.Agent(context.Background(), p, "home:2")
	if err != nil || a.Panel.ID != "2" {
		t.Fatalf("Agent by panel id = %+v, %v; want panel 2", a, err)
	}
}

// Beside named agents, the project's address names its default agent: the
// one panel that carries no named agent's mark.
func TestAgentIsTheDefaultAgentAmongNamedOnes(t *testing.T) {
	named := agentPanel("1", "idle")
	named.Vars = map[string]string{revier.PanelAgentVar: "dtop"}
	c, _ := agentCore(named, agentPanel("2", "busy"))

	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil || a.Panel.ID != "2" {
		t.Fatalf("Agent = %+v, %v; want panel 2, the default agent", a, err)
	}
}

// Two agents with no mark, or none without one, are still ambiguous.
func TestAgentRefusesTwoDefaultAgentsAndOnlyNamedOnes(t *testing.T) {
	mark := func(p revier.Panel, name string) revier.Panel {
		p.Vars = map[string]string{revier.PanelAgentVar: name}
		return p
	}
	for what, panels := range map[string][]revier.Panel{
		"two default":     {agentPanel("1", "idle"), agentPanel("2", "busy"), mark(agentPanel("3", "idle"), "dtop")},
		"only named ones": {mark(agentPanel("1", "idle"), "dtop"), mark(agentPanel("2", "busy"), "review")},
	} {
		c, _ := agentCore(panels...)
		if _, err := c.Agent(context.Background(), prepared(t, project()), ""); !errors.Is(err, core.ErrAmbiguous) {
			t.Errorf("%s: err = %v, want ErrAmbiguous", what, err)
		}
	}
}

// A target name narrows the address to that target's instance.
func TestAgentByTargetName(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	rt.Add("diff:revier", "kitty", agentPanel("7", "busy"))
	p := prepared(t, project())

	a, err := c.Agent(context.Background(), p, "diff")
	if err != nil || a.Panel.ID != "7" {
		t.Fatalf("Agent(diff) = %+v, %v; want panel 7", a, err)
	}
	if _, err := c.Agent(context.Background(), p, "editor"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("Agent(editor) err = %v, want not running", err)
	}
}

func TestAgentRefusesWhatIsNotAnAgent(t *testing.T) {
	// The probe claims panel 3 by its command, but the host sees a shell in
	// its foreground: the agent exited and left the marker behind.
	exited := agentPanel("3", "idle")
	exited.Kind = revier.PanelShell
	c, rt := agentCore(shellPanel("1"), exited)
	rt.Add("diff:revier", "kitty", shellPanel("5"))
	p := prepared(t, project())

	for _, tc := range []struct {
		addr string
		want error
	}{
		{"", core.ErrNoAgent},
		{"diff", core.ErrNoAgent},
		{"home:1", core.ErrNotAgent},
		{"home:3", core.ErrNotAgent},
		{"diff:5", core.ErrNotAgent},
	} {
		if _, err := c.Agent(context.Background(), p, tc.addr); !errors.Is(err, tc.want) {
			t.Errorf("Agent(%q) err = %v, want %v", tc.addr, err, tc.want)
		}
	}
	// The reason holds for every command that names a panel, a read too.
	if _, err := c.Agent(t.Context(), p, "home:1"); err == nil || !strings.Contains(err.Error(), "its foreground is a shell, not an agent") {
		t.Errorf("Agent(home:1) err = %v, want the shell named as what is in the panel", err)
	}
	for _, addr := range []string{"nosuch", "home:9"} {
		if _, err := c.Agent(context.Background(), p, addr); err == nil {
			t.Errorf("Agent(%q): an address naming nothing must be an error", addr)
		}
	}
}

func TestUntil(t *testing.T) {
	stopped, err := core.Until("stopped")
	if err != nil || len(stopped) != 2 {
		t.Fatalf("Until(stopped) = %v, %v; want idle and attention", stopped, err)
	}
	if _, err := core.Until("working"); err == nil {
		t.Error("an unknown status must be refused, not waited on forever")
	}
}

func TestWaitReturnsWhenTheStatusArrives(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "busy"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		rt.Retitle("1", "idle")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state, err := c.Wait(ctx, a, []revier.Status{revier.StatusIdle}, time.Millisecond)
	if err != nil || state.Status != revier.StatusIdle {
		t.Fatalf("Wait = %v, %v; want idle", state.Status, err)
	}
}

// A timeout is the context's deadline, with the state the agent was last in,
// so the caller can report it and exit with its own status.
func TestWaitTimesOutWithTheLastState(t *testing.T) {
	c, _ := agentCore(agentPanel("1", "busy"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	state, err := c.Wait(ctx, a, []revier.Status{revier.StatusIdle}, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || state.Status != revier.StatusRunning {
		t.Fatalf("Wait = %v, %v; want the deadline and running", state.Status, err)
	}
}

// A wait with no timeout must still end when the agent does.
func TestWaitEndsWhenTheAgentIsGone(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "busy"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	rt.Remove(a.Ref)
	// The deadline only bounds this test; the wait under test has none.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.Wait(ctx, a, []revier.Status{revier.StatusIdle}, time.Millisecond); !errors.Is(err, core.ErrAgentGone) {
		t.Fatalf("err = %v, want ErrAgentGone", err)
	}
}

// The panel of a new agent tab is a shell until its harness starts:
// StartedAgent looks it up again until it is an agent's.
func TestStartedAgentWaitsForTheHarnessToStart(t *testing.T) {
	c, rt := agentCore(shellPanel("1"))
	go func() {
		time.Sleep(50 * time.Millisecond)
		rt.Replace(agentPanel("1", "busy"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := c.StartedAgent(ctx, prepared(t, project()), "home:1")
	if err != nil || a.Panel.ID != "1" || a.State.Status != revier.StatusRunning {
		t.Fatalf("StartedAgent = %+v, %v; want panel 1, running", a, err)
	}
}

// A panel that never becomes an agent's ends on ctx with the refusal of the
// last lookup, which says what the panel still is.
func TestStartedAgentEndsOnItsContextWithTheRefusal(t *testing.T) {
	c, _ := agentCore(shellPanel("1"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := c.StartedAgent(ctx, prepared(t, project()), "home:1"); !errors.Is(err, core.ErrNotAgent) {
		t.Fatalf("err = %v, want ErrNotAgent", err)
	}
}

// A core with Poll set looks a shell panel up again at that interval, not at
// AgentPoll.
func TestStartedAgentLooksAPanelUpAgainAtTheCoresPoll(t *testing.T) {
	c, rt := agentCore(shellPanel("1"))
	c.Poll = time.Millisecond
	go func() {
		time.Sleep(20 * time.Millisecond)
		rt.Replace(agentPanel("1", "agent idle"))
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := c.StartedAgent(ctx, prepared(t, project()), "home:1"); err != nil {
		t.Fatalf("StartedAgent: %v", err)
	}
	if time.Since(start) >= core.AgentPoll {
		t.Error("StartedAgent looked the panel up again at AgentPoll, not at the core's Poll")
	}
}

// Only a panel that is not an agent's is looked up again: an address with no
// agent in it fails at once.
func TestStartedAgentFailsAtOnceWhereNoPanelIsNamed(t *testing.T) {
	c, _ := agentCore(shellPanel("1"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := c.StartedAgent(ctx, prepared(t, project()), ""); !errors.Is(err, core.ErrNoAgent) {
		t.Fatalf("err = %v, want ErrNoAgent", err)
	}
	if time.Since(start) >= core.AgentPoll {
		t.Error("StartedAgent looked the address up again")
	}
}

// The prompt lands in the agent's panel and is submitted, and Prompt returns
// only once the agent has left idle: `prompt && wait --until stopped` must
// wait for the turn it asked for, not match the rest before it.
func TestPromptTypesSubmitsAndWaitsForTheTurn(t *testing.T) {
	c, rt := agentCore(shellPanel("1"), agentPanel("2", "idle"))
	rt.OnSend = func(panel revier.PanelID, text string) {
		if text == "\r" {
			rt.Retitle(panel, "busy")
		}
	}
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}

	state, err := c.Prompt(context.Background(), a, `fix the \n bug`, time.Millisecond)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if state.Status != revier.StatusRunning {
		t.Errorf("state = %v, want running: Prompt returns after the agent leaves idle", state.Status)
	}
	want := []hosttest.Sent{
		{Ref: a.Ref, Panel: "2", Text: `fix the \n bug`},
		{Ref: a.Ref, Panel: "2", Text: "\r"},
	}
	if len(rt.Sent) != len(want) || rt.Sent[0] != want[0] || rt.Sent[1] != want[1] {
		t.Errorf("sent = %+v, want %+v", rt.Sent, want)
	}
}

// An idle agent that never starts is reported, not waited on forever.
func TestPromptReportsAnAgentThatStaysIdle(t *testing.T) {
	c, _ := agentCore(agentPanel("1", "idle"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.Prompt(context.Background(), a, "hello", time.Millisecond)
	if !errors.Is(err, core.ErrStillIdle) || state.Status != revier.StatusIdle {
		t.Fatalf("Prompt = %v, %v; want idle and ErrStillIdle", state.Status, err)
	}
}

// A panel id is one process's: two kitty processes each number their windows
// from 1. The target in the address names the process, so each of two agents
// in a window 1 has an address of its own (decisions.md D155).
func TestAPanelIDHeldTwiceIsNamedByItsTarget(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", agentPanel("1", "idle"))
	rt.Add("diff:revier", "kitty", agentPanel("1", "idle"))
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}
	p := prepared(t, project())

	_, err := c.Agent(context.Background(), p, "")
	if !errors.Is(err, core.ErrAmbiguous) || !strings.Contains(err.Error(), "revier:home:1, revier:diff:1") {
		t.Errorf("Agent() = %v, want both agents named by their addresses", err)
	}
	for addr, title := range map[string]string{"home:1": "session:revier", "diff:1": "diff:revier"} {
		if a, err := c.Agent(context.Background(), p, addr); err != nil || a.Ref.Title != title {
			t.Errorf("Agent(%s) = %+v, %v; want the agent in %s", addr, a, err, title)
		}
	}
}

// A panel id alone is no address: the target that names the process the id
// is unique in comes first, and the refusal says so.
func TestAnAddressOfAPanelAloneIsRefused(t *testing.T) {
	c, _ := agentCore(agentPanel("1", "idle"), agentPanel("2", "busy"))
	_, err := c.Agent(context.Background(), prepared(t, project()), "2")
	if err == nil || !strings.Contains(err.Error(), `no target "2"`) || !strings.Contains(err.Error(), "<project>:<target>:<panel>") {
		t.Errorf("Agent(2) err = %v, want the address form named", err)
	}
}

// A panel named in full is that panel, a named agent's too: the default
// agent is what an address that names no panel means.
func TestAnAddressWithAPanelNamesANamedAgent(t *testing.T) {
	named := agentPanel("1", "idle")
	named.Vars = map[string]string{revier.PanelAgentVar: "dtop"}
	c, _ := agentCore(named, agentPanel("2", "busy"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "home:1")
	if err != nil || a.Panel.ID != "1" {
		t.Errorf("Agent(home:1) = %+v, %v; want the named agent in panel 1", a, err)
	}
}

// Every agent a survey reports carries the target its address names, and
// the address names it back.
func TestEveryAgentsAddressNamesItBack(t *testing.T) {
	c, rt := agentCore(shellPanel("1"), agentPanel("2", "idle"), agentPanel("3", "busy"))
	rt.Add("diff:revier", "kitty", agentPanel("2", "busy"))
	p := prepared(t, project())

	agents := agentsOf(t, c, p)
	if len(agents) != 3 {
		t.Fatalf("agents = %+v, want three", agents)
	}
	for _, v := range agents {
		addr := core.Address(p.Name, v.Target, v.Panel)
		_, rest, _ := strings.Cut(addr, ":")
		a, err := c.Agent(context.Background(), p, rest)
		if err != nil || a.Ref != v.Ref || a.Panel.ID != v.Panel {
			t.Errorf("Agent(%s) = %+v, %v; want panel %s of %v", addr, a, err, v.Panel, v.Ref)
		}
	}
}

// A read that comes back unknown after the prompt is no proof that the turn
// started: a probe that failed reads that way. Prompt keeps watching, and
// reports the agent still idle when that is all it saw, so the caller is
// warned rather than told the turn is under way.
func TestPromptDoesNotTakeAnUnknownReadForTheTurn(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	rt.OnSend = func(panel revier.PanelID, text string) {
		if text == "\r" {
			rt.Retitle(panel, "mystery") // TitleProbe reads this as unknown
		}
	}
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.Prompt(context.Background(), a, "hello", time.Millisecond)
	if !errors.Is(err, core.ErrStillIdle) || state.Status != revier.StatusIdle {
		t.Fatalf("Prompt = %v, %v; want idle and ErrStillIdle: an unknown read is not the turn", state.Status, err)
	}
}

// The Enter that submits a prompt answers a dialog instead, so an agent
// waiting for the human, or one whose state is unknown, is not typed into.
func TestPromptRefusesAnAgentThatMayShowADialog(t *testing.T) {
	for _, title := range []string{"ask permission", "mystery"} {
		c, rt := agentCore(agentPanel("1", title))
		a, err := c.Agent(context.Background(), prepared(t, project()), "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Prompt(context.Background(), a, "yes", time.Millisecond)
		if err == nil {
			t.Errorf("%q: Prompt succeeded, want a refusal", title)
		}
		if title == "ask permission" && (!errors.Is(err, core.ErrAttention) || !strings.Contains(err.Error(), "dialog")) {
			t.Errorf("err = %v, want ErrAttention saying why", err)
		}
		if len(rt.Sent) != 0 {
			t.Errorf("%q: sent %+v, want nothing", title, rt.Sent)
		}
	}
}

func TestPromptRefusesMoreThanOneLine(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, _ := c.Agent(context.Background(), prepared(t, project()), "")
	if _, err := c.Prompt(context.Background(), a, "one\ntwo", time.Millisecond); err == nil || len(rt.Sent) != 0 {
		t.Fatalf("err = %v, sent %+v; want a refusal and nothing sent", err, rt.Sent)
	}
}

// An empty or blank Enter starts no turn, so a wait after it would return at
// once as if the turn had ended.
func TestPromptRefusesAnEmptyOrBlankText(t *testing.T) {
	for _, text := range []string{"", "   "} {
		c, rt := agentCore(agentPanel("1", "idle"))
		a, err := c.Agent(t.Context(), prepared(t, project()), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Prompt(t.Context(), a, text, time.Millisecond); err == nil || !strings.Contains(err.Error(), "a prompt has text") || len(rt.Sent) != 0 {
			t.Errorf("Prompt(%q): err = %v, sent %+v; want a refusal and nothing sent", text, err, rt.Sent)
		}
	}
}

// A working agent queues the prompt and nothing marks its arrival, so there
// is nothing to wait for.
func TestPromptToAWorkingAgentReturnsAtOnce(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "busy"))
	a, _ := c.Agent(context.Background(), prepared(t, project()), "")
	calls := rt.InstancesCalls
	if _, err := c.Prompt(context.Background(), a, "also this", time.Hour); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(rt.Sent) != 2 || rt.InstancesCalls != calls {
		t.Errorf("sent %d, listed %d more times; want the prompt sent and no poll", len(rt.Sent), rt.InstancesCalls-calls)
	}
}

func TestPromptNeedsARuntimeThatCanType(t *testing.T) {
	fake := hosttest.NewRuntime("rt")
	fake.Add("session:revier", "kitty", agentPanel("1", "idle"))
	c := &core.Core{Runtime: bareRuntime{fake}, View: bareView{fake}, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}
	a, err := c.Agent(context.Background(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(context.Background(), a, "hello", time.Millisecond); !errors.Is(err, core.ErrNoWriter) {
		t.Fatalf("err = %v, want ErrNoWriter", err)
	}
}

// A project is summed up by one agent: the one closest to needing the human,
// and of two in that state the first, so `revier list` and the TUI row name
// the same one.
func TestWorstIsTheFirstAgentInTheWorstState(t *testing.T) {
	agent := func(s revier.Status, activity string) revier.AgentView {
		return revier.AgentView{State: revier.AgentState{Status: s, Activity: activity}}
	}
	for name, tc := range map[string]struct {
		agents []revier.AgentView
		want   string
	}{
		"worst wins":          {[]revier.AgentView{agent(revier.StatusIdle, "a"), agent(revier.StatusAttention, "b")}, "b"},
		"first of two tied":   {[]revier.AgentView{agent(revier.StatusRunning, "a"), agent(revier.StatusRunning, "b")}, "a"},
		"unknown is an agent": {[]revier.AgentView{agent(revier.StatusUnknown, "a")}, "a"},
	} {
		got, ok := core.Worst(tc.agents)
		if !ok || got.Activity != tc.want {
			t.Errorf("%s: Worst = %+v, %v; want activity %q", name, got, ok, tc.want)
		}
	}
	if _, ok := core.Worst(nil); ok {
		t.Error("Worst(nil) reported an agent")
	}
}

// A panel of a tab placed inside another target is addressed through the
// tab's target alone: the target it sits inside names it no second time
// (decisions.md D155). A tab no declared target holds stays the home's.
func TestAPanelOfATabInsideHomeIsNotAddressedThroughHome(t *testing.T) {
	marked := func(panel revier.Panel) revier.Panel {
		panel.Vars = map[string]string{core.PanelTargetVar: "agent"}
		return panel
	}
	rt := hosttest.NewRuntime("rt")
	rt.Add("demo", "", shellPanel("1"), marked(agentPanel("2", "idle")), marked(shellPanel("3")), agentPanel("4", "busy"))
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}
	p := placeProject(t, "demo")

	for _, panel := range []string{"2", "3"} {
		want := "its address is demo:agent:" + panel
		if _, err := c.Agent(t.Context(), p, "home:"+panel); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Agent(home:%s) err = %v, want the refusal to give %q", panel, err, want)
		}
	}
	if a, err := c.Agent(t.Context(), p, "agent:2"); err != nil || a.Panel.ID != "2" {
		t.Errorf("Agent(agent:2) = %+v, %v; want the tab's agent", a, err)
	}
	if a, err := c.Agent(t.Context(), p, "home:4"); err != nil || a.Panel.ID != "4" {
		t.Errorf("Agent(home:4) = %+v, %v; want the agent of a tab no target holds", a, err)
	}
}

// A panel of an instance two non-tab targets match answers to both addresses,
// the survey prints the first, and neither reaches another agent
// (decisions.md D155).
func TestAPanelOfTwoNonTabTargetsAnswersToBothAddresses(t *testing.T) {
	p := prepared(t, revier.Project{Name: "demo", Path: "/srv/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Match: revier.Match{Title: "^session:demo$"}}},
		{Name: "mirror", Runtime: &revier.Realization{Match: revier.Match{Title: "^session:"}}},
	}})
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "", agentPanel("2", "idle"), agentPanel("3", "busy"))
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}

	for _, addr := range []string{"home", "mirror"} {
		for _, panel := range []revier.PanelID{"2", "3"} {
			a, err := c.Agent(t.Context(), p, addr+":"+panel.String())
			if err != nil || a.Panel.ID != panel {
				t.Errorf("Agent(%s:%s) = %+v, %v; want panel %s", addr, panel, a, err, panel)
			}
		}
	}
	for _, a := range agentsOf(t, c, p) {
		if a.Target != "home" {
			t.Errorf("survey labels panel %s with %s, want the first target, home", a.Panel, a.Target)
		}
	}
}

// A refused tab target places no panel: the survey labels the agent of its
// open tab as the home's, and that address names it back.
func TestTheAgentOfARefusedTabIsAddressedAsTheSurveyLabelsIt(t *testing.T) {
	p := prepared(t, revier.Project{Name: "demo", Path: "/srv/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Match: revier.Match{Title: "^session:demo$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: []string{"agent", "{{.Vars.extra}}"}}, {Kind: revier.PanelShell}}}},
	}})
	if p.TargetErr(1) == nil {
		t.Fatal("the agent tab must be refused for the key it renders")
	}
	tabbed := agentPanel("2", "idle")
	tabbed.Vars = map[string]string{core.PanelTargetVar: "agent"}
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "", shellPanel("1"), tabbed)
	c := &core.Core{Runtime: rt, View: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}

	agents := agentsOf(t, c, p)
	if len(agents) != 1 {
		t.Fatalf("agents = %+v, want one", agents)
	}
	_, rest, _ := strings.Cut(core.Address(p.Name, agents[0].Target, agents[0].Panel), ":")
	if a, err := c.Agent(t.Context(), p, rest); err != nil || a.Panel.ID != "2" {
		t.Errorf("Agent(%s) = %+v, %v; want the agent the survey listed", rest, a, err)
	}
}

// goneAfterEnter is a runtime whose agent exits on the Enter of a prompt.
type goneAfterEnter struct{ *hosttest.FakeRuntime }

func (g goneAfterEnter) SendText(ctx context.Context, ref revier.TargetRef, panel revier.PanelID, text string) error {
	err := g.FakeRuntime.SendText(ctx, ref, panel, text)
	if text == "\r" {
		g.Remove(ref)
	}
	return err
}

// An agent that exits on the prompt is gone, not still idle: the watch ends at
// once and says so.
func TestPromptReportsAnAgentThatExits(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, err := c.Agent(t.Context(), prepared(t, project()), "")
	if err != nil {
		t.Fatal(err)
	}
	c.Runtime = goneAfterEnter{rt}

	if _, err := c.Prompt(t.Context(), a, "hello", time.Millisecond); !errors.Is(err, core.ErrAgentGone) {
		t.Fatalf("err = %v, want ErrAgentGone", err)
	}
}
