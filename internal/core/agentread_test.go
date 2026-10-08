// Layer L2: what a script reads of an agent and types into it besides a
// prompt, and the tab it opens without taking the focus.
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

// saidCore is the project's home holding one agent the probe claims.
func saidCore(t *testing.T, probe revier.AgentProbe) (*core.Core, core.Agent) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, a
}

// What `revier agent read` prints is what the agent's probe says it said last.
func TestSaidIsWhatTheAgentSaidLast(t *testing.T) {
	probe := hosttest.NewDetailedProbe("claude", "claude")
	probe.Said["1"] = revier.AgentDetail{Message: "the build is green"}
	c, a := saidCore(t, probe)

	got, err := c.Said(context.Background(), a)
	if err != nil || got.Message != "the build is green" {
		t.Fatalf("Said = %+v, %v; want the agent's message", got, err)
	}
}

// An agent with no message to read is a normal outcome with its own error,
// so the command can name the screen as the other thing to read. A probe that
// broke is not that outcome.
func TestSaidOfAnAgentWithNoMessageIsNoMessage(t *testing.T) {
	nothingYet := hosttest.NewDetailedProbe("claude", "claude")
	noSession := hosttest.NewDetailedProbe("claude", "claude")
	noSession.DetailErr = revier.ErrNoDetail
	for name, probe := range map[string]revier.AgentProbe{
		"a probe with no Detail":       &hosttest.FakeProbe{Harness: "claude", Marker: "claude"},
		"an agent that said nothing":   nothingYet,
		"a panel in no listed session": noSession,
	} {
		c, a := saidCore(t, probe)
		if _, err := c.Said(context.Background(), a); !errors.Is(err, core.ErrNoMessage) {
			t.Errorf("%s: err = %v, want ErrNoMessage", name, err)
		}
	}

	broken := hosttest.NewDetailedProbe("claude", "claude")
	broken.DetailErr = errors.New("transcript format changed")
	c, a := saidCore(t, broken)
	if _, err := c.Said(context.Background(), a); err == nil || errors.Is(err, core.ErrNoMessage) {
		t.Errorf("a probe that broke: err = %v, want its own error", err)
	}
}

// A link's agent keeps its conversation on its host, and the panel here runs
// an ssh: there is no message to read here, whatever a probe would make of
// the panel.
func TestSaidOfALinksAgentIsNoMessage(t *testing.T) {
	c, _, _, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	probe := hosttest.NewDetailedProbe("claude", "ssh")
	c.Probes = []revier.AgentProbe{probe}
	a, err := c.Agent(context.Background(), linkProject(t), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Said(context.Background(), a); !errors.Is(err, core.ErrNoMessage) || !strings.Contains(err.Error(), "buildbox") {
		t.Errorf("err = %v, want ErrNoMessage naming the host", err)
	}
	if probe.DetailCalls() != 0 {
		t.Errorf("the probe was asked %d times, want none", probe.DetailCalls())
	}
}

// Each key is typed by itself, in order, as the bytes a terminal sends for
// it; one character stands for itself.
func TestSendKeysTypesEachKeyInOrder(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, _ := c.Agent(context.Background(), prepared(t, project()), "", nil)

	if err := c.SendKeys(context.Background(), a, []string{"down", "2", "Enter", "esc"}, time.Millisecond); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	var got []string
	for _, s := range rt.Sent {
		if s.Ref != a.Ref || s.Panel != "1" {
			t.Errorf("sent to %v %s, want the agent's panel", s.Ref, s.Panel)
		}
		got = append(got, s.Text)
	}
	if want := []string{"\x1b[B", "2", "\r", "\x1b"}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

// A key answers the dialog a prompt is kept out of and interrupts a working
// agent, so no state refuses it (decisions.md D117).
func TestSendKeysReachesAnAgentInAnyState(t *testing.T) {
	for _, title := range []string{"idle", "busy", "ask permission", "mystery"} {
		c, rt := agentCore(agentPanel("1", title))
		a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SendKeys(context.Background(), a, []string{"esc"}, time.Millisecond); err != nil || len(rt.Sent) != 1 {
			t.Errorf("%q: err = %v, sent %+v; want the key typed", title, err, rt.Sent)
		}
	}
}

// One name that is no key refuses every key: nothing is typed up to a typo.
func TestSendKeysTypesNothingWhenAKeyIsUnknown(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, _ := c.Agent(context.Background(), prepared(t, project()), "", nil)
	for _, name := range []string{"escape", "", "\x1b"} {
		err := c.SendKeys(context.Background(), a, []string{"down", name}, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "unknown key") || len(rt.Sent) != 0 {
			t.Errorf("%q: err = %v, sent %+v; want a refusal and nothing sent", name, err, rt.Sent)
		}
	}
}

func TestSendKeysNeedsARuntimeThatCanType(t *testing.T) {
	fake := hosttest.NewRuntime("rt")
	fake.Add("session:revier", "kitty", agentPanel("1", "idle"))
	c := &core.Core{Runtime: bareRuntime{fake}, Probes: []revier.AgentProbe{titleProbe{}}}
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendKeys(context.Background(), a, []string{"esc"}, time.Millisecond); !errors.Is(err, core.ErrNoWriter) {
		t.Fatalf("err = %v, want ErrNoWriter", err)
	}
}

// A script's tab: the same agent tab, with nothing made current and nothing
// raised, and the new agent's panel returned to find it by
// (decisions.md D118).
func TestAddAgentOpensTheTabAndLeavesTheFocus(t *testing.T) {
	rt, wm, _, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{resumable()}}
	w, err := c.AgentWorkspace(context.Background(), prepared(t, agentProject()), "home", nil)
	if err != nil {
		t.Fatal(err)
	}

	panel, outcome, err := c.AddAgent(context.Background(), w, core.Resume{Session: "abc-123"})
	if err != nil || outcome != core.AgentResumed {
		t.Fatalf("AddAgent = %v, %v; want resumed", outcome, err)
	}
	if len(rt.Tabs) != 1 || panel != rt.Tabs[0].Panel {
		t.Fatalf("panel = %q, tabs = %+v; want the one tab's agent panel", panel, rt.Tabs)
	}
	if len(rt.Tabs[0].Real.Panels) != 2 {
		t.Errorf("tab panels = %+v, want the agent and its shell", rt.Tabs[0].Real.Panels)
	}
	if len(rt.PanelFocuses) != 0 || len(wm.Focuses) != 0 {
		t.Errorf("panel focuses = %v, window focuses = %v; want none", rt.PanelFocuses, wm.Focuses)
	}
}

// Nothing is raised, so the OS window NewAgent refuses for being unraisable
// takes a script's tab.
func TestAddAgentNeedsNoWindowToRaise(t *testing.T) {
	rt, _, _, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: hosttest.New("wm"), Probes: []revier.AgentProbe{resumable()}}
	w, err := c.AgentWorkspace(context.Background(), prepared(t, agentProject()), "home", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := c.AddAgent(context.Background(), w, core.Resume{}); err != nil || outcome != core.AgentEmpty || len(rt.Tabs) != 1 {
		t.Errorf("AddAgent = %v, %v, tabs %+v; want one empty agent's tab", outcome, err, rt.Tabs)
	}
}

// A tab that did not open names no panel and no agent.
func TestAddAgentReportsATabThatDidNotOpen(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)
	rt.OpenTabErr = errors.New("no room")
	w, err := c.AgentWorkspace(context.Background(), p, "home", nil)
	if err != nil {
		t.Fatal(err)
	}
	if panel, outcome, err := c.AddAgent(context.Background(), w, core.Resume{}); err == nil || panel != "" || outcome != core.AgentNotAdded {
		t.Errorf("AddAgent = %q, %v, %v; want no panel, not added and the error", panel, outcome, err)
	}
}
