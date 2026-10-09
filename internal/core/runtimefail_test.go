// Layer L2: what the core reports when the runtime fails in the middle of an
// operation. Each failure has a contract of its own: whether the caller is
// told to do it again, and whether what did open is still reported so the
// next press finds it.
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

var errRuntimeGone = errors.New("the runtime went away")

// The text arrived and the Enter did not. The error says so, because a caller
// that prompts again types the text a second time.
func TestAPromptWhoseEnterIsLostSaysTheTextSitsUnsent(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt.SendErr, rt.SendsBeforeErr = errRuntimeGone, 1

	state, err := c.Prompt(context.Background(), a, "hello", time.Millisecond)
	if !errors.Is(err, errRuntimeGone) || !strings.Contains(err.Error(), "sits unsent in the composer") {
		t.Fatalf("err = %v, want the runtime's failure, named as a text left unsent", err)
	}
	if state.Status != revier.StatusIdle {
		t.Errorf("state = %v, want the idle the agent was read in", state.Status)
	}
	if want := []hosttest.Sent{{Ref: a.Ref, Panel: "1", Text: "hello"}}; !slices.Equal(rt.Sent, want) {
		t.Errorf("sent = %+v, want the text alone", rt.Sent)
	}
}

// Nothing arrived, so nothing sits in the composer and the error does not say
// that it does: this prompt can be given again.
func TestAPromptThatTypesNothingIsThePlainFailure(t *testing.T) {
	c, rt := agentCore(agentPanel("1", "idle"))
	a, err := c.Agent(context.Background(), prepared(t, project()), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt.SendErr = errRuntimeGone

	_, err = c.Prompt(context.Background(), a, "hello", time.Millisecond)
	if !errors.Is(err, errRuntimeGone) || strings.Contains(err.Error(), "unsent") {
		t.Fatalf("err = %v, want the runtime's failure and no word of a text left unsent", err)
	}
	if len(rt.Sent) != 0 {
		t.Errorf("sent = %+v, want nothing", rt.Sent)
	}
}

// A workspace this press opened for a tab is reported when the tab cannot be
// focused, as it is when the tab cannot be opened: the caller pins it and the
// next press does not open another.
func TestATabThatCannotBeFocusedInTheWorkspaceItOpenedReportsTheWorkspace(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.FocusPanelErr = errRuntimeGone
	c := &core.Core{Runtime: rt}

	res, err := c.Go(context.Background(), prepared(t, tabProject()), "tickets", nil)
	if !errors.Is(err, errRuntimeGone) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if len(rt.Opened) != 1 || len(rt.Tabs) != 1 || res.Target != "home" || res.Ref.IsZero() {
		t.Errorf("result = %+v, opened %d, tabs %d; want the opened workspace reported as home", res, len(rt.Opened), len(rt.Tabs))
	}
}

// A workspace that was there is the caller's already, so the failure reports
// nothing to pin, and the OS window is not raised around a tab that is not
// showing.
func TestATabThatCannotBeFocusedInAnOpenWorkspaceReportsNothing(t *testing.T) {
	rt, wm, _ := tabHosts(t)
	rt.FocusPanelErr = errRuntimeGone
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, tabProject()), "tickets", nil)
	if !errors.Is(err, errRuntimeGone) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if !res.Ref.IsZero() || res.Launched {
		t.Errorf("result = %+v, want nothing to pin", res)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none", wm.Focuses)
	}
}

// restoreWithAgentTabs is agentProject restored on a fresh runtime with one agent in the
// layout and two past it, after the runtime was set up to fail.
func restoreWithAgentTabs(t *testing.T, fail func(*hosttest.FakeRuntime)) (core.Result, *hosttest.FakeRuntime) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	fail(rt)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	res, err := c.GoResuming(context.Background(), prepared(t, agentProject()), "home", nil, []core.Resume{
		{Harness: "claude", Session: "declared"},
		{Harness: "claude", Session: "second"},
		{Harness: "claude", Session: "third"},
	})
	if err != nil {
		t.Fatalf("GoResuming: %v; the workspace opened, so a tab's failure is not the launch's", err)
	}
	if res.Ref.IsZero() {
		t.Fatal("the opened workspace is not reported")
	}
	return res, rt
}

// A restore reads the current panel before its first agent tab, to make it
// current again after them. When that read fails no tab is opened: every agent
// past the layout is named as not added, with the reason beside them.
func TestARestoreThatCannotReadTheCurrentPanelAddsNoAgentTab(t *testing.T) {
	res, rt := restoreWithAgentTabs(t, func(rt *hosttest.FakeRuntime) { rt.FocusedPanelErr = errRuntimeGone })

	if !errors.Is(res.AgentErr, errRuntimeGone) {
		t.Errorf("AgentErr = %v, want the runtime's failure", res.AgentErr)
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentNotAdded, core.AgentNotAdded}; !slices.Equal(res.Agents, want) {
		t.Errorf("Agents = %v, want %v", res.Agents, want)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none", rt.Tabs)
	}
}

// The tabs opened and the workspace's own panel could not be made current
// again. The agents run, so each keeps its outcome, and the failure is told.
func TestARestoreThatCannotFocusTheWorkspaceAgainKeepsItsAgents(t *testing.T) {
	whole, _ := restoreWithAgentTabs(t, func(*hosttest.FakeRuntime) {})
	res, rt := restoreWithAgentTabs(t, func(rt *hosttest.FakeRuntime) { rt.FocusPanelErr = errRuntimeGone })

	if !errors.Is(res.AgentErr, errRuntimeGone) || !strings.Contains(res.AgentErr.Error(), "focus the workspace after its agent tabs") {
		t.Errorf("AgentErr = %v, want the focus failure after the tabs", res.AgentErr)
	}
	if !slices.Equal(res.Agents, whole.Agents) || slices.Contains(res.Agents, core.AgentNotAdded) {
		t.Errorf("Agents = %v, want %v as with no failure", res.Agents, whole.Agents)
	}
	if len(rt.Tabs) != 2 {
		t.Errorf("tabs = %d, want both agents past the layout opened", len(rt.Tabs))
	}
}

// The tab runs although it could not be shown. The outcome says the agent was
// added, so the caller does not open it a second time.
func TestANewAgentThatCannotBeFocusedIsStillAdded(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)
	rt.FocusPanelErr = errRuntimeGone

	outcome, err := newAgent(t, c, p, "home", core.Resume{})
	if !errors.Is(err, errRuntimeGone) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if outcome == core.AgentNotAdded || len(rt.Tabs) != 1 {
		t.Errorf("outcome = %v, tabs = %d; want the one tab that opened reported as added", outcome, len(rt.Tabs))
	}
}

// An agent whose tab cannot be made current is not raised: the OS window would
// come up showing another tab.
func TestAnAgentWhoseTabCannotBeFocusedIsNotRaised(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	workspace := rt.Add("session:revier", "kitty", shellPanel("1"), agentPanel("2", "idle"))
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}
	rt.FocusPanelErr = errRuntimeGone

	err := c.FocusAgent(context.Background(), workspace, "2")
	if !errors.Is(err, errRuntimeGone) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none", wm.Focuses)
	}
}

// The second press of the popup's key is typed into the popup. When the
// typing fails the press is what it was before the popup had a second list:
// the popup, raised.
func TestAPopupThatCannotBeTypedIntoIsRaised(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(open)
	rt := hosttest.NewRuntime("rt")
	rt.Add("revier", core.PopupClass, revier.Panel{ID: "9"})
	rt.SendErr = errRuntimeGone
	c := &core.Core{Window: wm, Runtime: rt}

	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil || ref != open {
		t.Fatalf("Popup = %v, %v; want the open popup and no error", ref, err)
	}
	if !slices.Equal(wm.Focuses, []revier.TargetRef{open}) || len(wm.Opened) != 0 {
		t.Errorf("focuses = %v, opened = %v; want the popup raised and none opened", wm.Focuses, wm.Opened)
	}
}

// A terminal whose current panel cannot be read is not taken for a surface:
// nothing is typed into it, and the popup opens.
func TestATerminalWhoseCurrentPanelCannotBeReadIsNoSurface(t *testing.T) {
	wm, rt, _, _ := surfaceTerminal(
		revier.Panel{ID: "1", Command: []string{"zsh"}},
		revier.Panel{ID: "2", Command: []string{"revier"}})
	wm.Workarea = 5120
	rt.FocusedPanelErr = errRuntimeGone
	c := &core.Core{Window: wm, Runtime: rt}

	if _, err := c.Popup(context.Background(), popupArgv); err != nil {
		t.Fatalf("Popup: %v", err)
	}
	if len(rt.Sent) != 0 || len(wm.Opened) != 1 {
		t.Errorf("sent = %+v, opened = %v; want nothing typed and the popup launched", rt.Sent, wm.Opened)
	}
}

// The surface's press of an agent: its tab is made current, the press is one
// agent event, and nothing is written to the ledger.
func TestActivateAgentWaitingFocusesTheAgentAndWritesNothing(t *testing.T) {
	recorded := recording(t)
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:revier", "kitty", shellPanel("1"), agentPanel("2", "idle"))
	c := &core.Core{Runtime: rt}
	a := revier.AgentView{Panel: "2", Ref: ref, State: revier.AgentState{Harness: "agent", Session: "abc"}}
	l := &ledger{}

	// A pending launch of a project on this machine does not hold its agent:
	// the agent is in an instance that is up.
	res, err := c.ActivateAgentWaiting(context.Background(), prepared(t, project()), a, pendingLedger{l})
	if err != nil || res.ComingUp {
		t.Fatalf("ActivateAgentWaiting = %+v, %v; want the agent reached", res, err)
	}
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{"2"}) {
		t.Errorf("panel focuses = %v, want panel 2", rt.PanelFocuses)
	}
	if len(l.writes) != 0 {
		t.Errorf("ledger writes = %v, want none", l.writes)
	}
	want := []revier.Event{{Kind: revier.EventGoAgent, Project: "revier", Agent: "agent", Session: "abc"}}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A link's agent is shown by a panel of the link's workspace. While that
// workspace is still coming up from an earlier press, the press of the agent
// waits with it: nothing is focused and no event is recorded.
func TestActivateAgentWaitingLeavesALinkThatIsComingUp(t *testing.T) {
	recorded := recording(t)
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Machine: "box"}
	a := revier.AgentView{Panel: "9", Ref: revier.TargetRef{Host: "rt", ID: "1"}, State: revier.AgentState{Harness: "claude"}}

	res, err := c.ActivateAgentWaiting(context.Background(), linkProject(t), a, pendingLedger{&ledger{}})
	if err != nil || !res.ComingUp || res.Target != "home" {
		t.Fatalf("ActivateAgentWaiting = %+v, %v; want home coming up", res, err)
	}
	if len(rt.PanelFocuses) != 0 || len(rt.Focuses) != 0 || len(rt.Opened) != 0 {
		t.Errorf("panel focuses = %v, focuses = %v, opened = %v; want nothing done", rt.PanelFocuses, rt.Focuses, rt.Opened)
	}
	if got := recorded(); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// An agent that could not be reached was not used: no event.
func TestActivateAgentWaitingRecordsNoEventForAFailure(t *testing.T) {
	recorded := recording(t)
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:revier", "kitty", agentPanel("1", "idle"))
	rt.FocusPanelErr = errRuntimeGone
	c := &core.Core{Runtime: rt}
	a := revier.AgentView{Panel: "1", Ref: ref, State: revier.AgentState{Harness: "agent"}}

	if _, err := c.ActivateAgentWaiting(context.Background(), prepared(t, project()), a, &ledger{}); !errors.Is(err, errRuntimeGone) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if got := recorded(); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}
