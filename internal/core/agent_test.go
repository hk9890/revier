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

// titleProbe claims a panel running "agent" and reads its state off the first
// word of the title, the way the Claude probe reads a glyph: the state lives
// in the panel, so a test changes it by retitling.
type titleProbe struct{}

func (titleProbe) Name() string { return "agent" }

func (titleProbe) Match(p revier.Panel) bool { return len(p.Command) > 0 && p.Command[0] == "agent" }

func (titleProbe) Inspect(_ context.Context, p revier.Panel) (revier.AgentState, error) {
	status := map[string]revier.Status{
		"idle": revier.StatusIdle, "busy": revier.StatusRunning, "ask": revier.StatusAttention,
	}[strings.Fields(p.Title + " ?")[0]]
	return revier.AgentState{Harness: "agent", Status: status}, nil
}

func agentPanel(id, title string) revier.Panel {
	return revier.Panel{ID: revier.PanelID(id), Kind: revier.PanelAgent, Title: title, Command: []string{"agent"}}
}

func shellPanel(id string) revier.Panel {
	return revier.Panel{ID: revier.PanelID(id), Kind: revier.PanelShell, Title: "zsh", Command: []string{"zsh"}}
}

// agentCore is a runtime holding the project's home workspace with the given
// panels, and the core reading it with titleProbe.
func agentCore(panels ...revier.Panel) (*core.Core, *hosttest.FakeRuntime) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", panels...)
	return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{titleProbe{}}}, rt
}

func TestAgentIsTheProjectsOnlyAgent(t *testing.T) {
	c, _ := agentCore(shellPanel("1"), agentPanel("2", "idle"))

	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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

	_, err := c.Agent(context.Background(), p, "", nil)
	if !errors.Is(err, core.ErrAmbiguous) || !strings.Contains(err.Error(), "revier:1, revier:2") {
		t.Fatalf("err = %v, want ErrAmbiguous naming revier:1 and revier:2", err)
	}
	a, err := c.Agent(context.Background(), p, "2", nil)
	if err != nil || a.Panel.ID != "2" {
		t.Fatalf("Agent by panel id = %+v, %v; want panel 2", a, err)
	}
}

// A target name narrows the address to that target's instance.
func TestAgentByTargetName(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	rt.Add("diff:revier", "kitty", agentPanel("7", "busy"))
	p := prepared(t, project())

	a, err := c.Agent(context.Background(), p, "diff", nil)
	if err != nil || a.Panel.ID != "7" {
		t.Fatalf("Agent(diff) = %+v, %v; want panel 7", a, err)
	}
	if _, err := c.Agent(context.Background(), p, "editor", nil); err == nil || !strings.Contains(err.Error(), "not running") {
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
		{"1", core.ErrNotAgent},
		{"3", core.ErrNotAgent},
	} {
		if _, err := c.Agent(context.Background(), p, tc.addr, nil); !errors.Is(err, tc.want) {
			t.Errorf("Agent(%q) err = %v, want %v", tc.addr, err, tc.want)
		}
	}
	if _, err := c.Agent(context.Background(), p, "nosuch", nil); err == nil {
		t.Error("an address naming nothing must be an error")
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
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.Prompt(context.Background(), a, "hello", time.Millisecond)
	if err != nil || state.Status != revier.StatusIdle {
		t.Fatalf("Prompt = %v, %v; want idle and no error", state.Status, err)
	}
}

// The Enter that submits a prompt answers a dialog instead, so an agent
// waiting for the human, or one whose state is unknown, is not typed into.
func TestPromptRefusesAnAgentThatMayShowADialog(t *testing.T) {
	for _, title := range []string{"ask permission", "mystery"} {
		c, rt := agentCore(agentPanel("1", title))
		a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
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
	a, _ := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if _, err := c.Prompt(context.Background(), a, "one\ntwo", time.Millisecond); err == nil || len(rt.Sent) != 0 {
		t.Fatalf("err = %v, sent %+v; want a refusal and nothing sent", err, rt.Sent)
	}
}

// A working agent queues the prompt and nothing marks its arrival, so there
// is nothing to wait for.
func TestPromptToAWorkingAgentReturnsAtOnce(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "busy"))
	a, _ := c.Agent(context.Background(), prepared(t, project()), "", nil)
	calls := rt.InstancesCalls
	if _, err := c.Prompt(context.Background(), a, "also this", time.Hour); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(rt.Sent) != 2 || rt.InstancesCalls != calls {
		t.Errorf("sent %d, listed %d more times; want the prompt sent and no poll", len(rt.Sent), rt.InstancesCalls-calls)
	}
}

// noWriter is a runtime without the PanelWriter capability.
type noWriter struct{ *hosttest.Fake }

func (noWriter) Capabilities() revier.Capabilities { return revier.Capabilities{} }

func TestPromptNeedsARuntimeThatCanType(t *testing.T) {
	fake := hosttest.New("rt")
	fake.Add("session:revier", "kitty", agentPanel("1", "idle"))
	c := &core.Core{Runtime: noWriter{fake}, Probes: []revier.AgentProbe{titleProbe{}}}
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(context.Background(), a, "hello", time.Millisecond); !errors.Is(err, core.ErrNoWriter) {
		t.Fatalf("err = %v, want ErrNoWriter", err)
	}
}
