package main

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// agent new takes one name at most.
func TestAgentNewTakesOneName(t *testing.T) {
	err := cmdAgentNew(io.Discard, []string{"dtop", "review"})
	if err == nil || !strings.Contains(err.Error(), "usage: revier agent new [<name>]") {
		t.Errorf("err = %v, want the usage", err)
	}
}

// agentDesktop is the demo workspace open on the runtime: its own tab holds
// a shell and the declared agent, panels 1 and 2, and the tab `revier agent
// new` added holds an agent and the shell beside it, panels 3 and 4. Every
// agent reports status.
func agentDesktop(t *testing.T, status revier.Status) (*app, *hosttest.FakeRuntime) {
	t.Helper()
	p := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Match: revier.Match{Title: "^home$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh", Tab: "t1"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude", Tab: "t1"},
		revier.Panel{ID: "3", Kind: revier.PanelAgent, Title: "claude", Tab: "t2"},
		revier.Panel{ID: "4", Kind: revier.PanelShell, Title: "zsh", Tab: "t2"},
	)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: status}}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{p}, stateRoot: t.TempDir(), core: c}, &state.State{})
	return a, rt
}

// closeAgent runs `revier agent close` on the app, and returns what it
// printed and its error.
func closeAgent(a *app, address string, force bool) (string, error) {
	var out strings.Builder
	a.out = &out
	err := a.closeAgent(context.Background(), address, force)
	return out.String(), err
}

// An agent in the tab `revier agent new` opened closes with that tab: the
// shell beside it goes too, and the command says it closed the tab.
func TestAgentCloseClosesTheTabAgentNewOpened(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	out, err := closeAgent(a, "demo:home:3", false)
	if err != nil {
		t.Fatalf("err = %v, want the agent closed", err)
	}
	if out != "closed the tab\n" {
		t.Errorf("output = %q, want the tab said closed", out)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"3", "4"}) {
		t.Errorf("panels closed = %v, want the agent and the shell beside it", rt.ClosedPanels)
	}
}

// An agent in the workspace's own tab closes as its panel alone: the shell
// the tab holds beside it stays (decisions.md D94), and the command says so.
func TestAgentCloseInTheWorkspacesOwnTabClosesTheAgentPanelAlone(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	out, err := closeAgent(a, "demo:home:2", false)
	if err != nil {
		t.Fatalf("err = %v, want the agent closed", err)
	}
	if out != "closed the agent panel\n" {
		t.Errorf("output = %q, want the agent panel said closed", out)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("panels closed = %v, want the agent's panel alone", rt.ClosedPanels)
	}
}

// A busy agent is refused and nothing closes; --force closes it.
func TestAgentCloseRefusesABusyAgentUnlessForced(t *testing.T) {
	for _, status := range []revier.Status{revier.StatusRunning, revier.StatusAttention} {
		a, rt := agentDesktop(t, status)

		out, err := closeAgent(a, "demo:home:3", false)
		if err == nil || !strings.Contains(err.Error(), "--force") || out != "" {
			t.Errorf("%s: err = %v, output = %q; want a refusal that names --force", status, err, out)
		}
		if len(rt.ClosedPanels) != 0 {
			t.Errorf("%s: panels closed = %v after the refusal, want none", status, rt.ClosedPanels)
		}

		if out, err := closeAgent(a, "demo:home:3", true); err != nil || out != "closed the tab\n" {
			t.Errorf("%s: forced: err = %v, output = %q; want the tab closed", status, err, out)
		}
		if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"3", "4"}) {
			t.Errorf("%s: forced: panels closed = %v, want the agent's tab", status, rt.ClosedPanels)
		}
	}
}

// A panel that runs no agent is refused with that said, and nothing closes.
func TestAgentCloseRefusesAPanelThatRunsNoAgent(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	_, err := closeAgent(a, "demo:home:4", false)
	if err == nil || err.Error() != "demo:home:4: no agent runs in that panel" {
		t.Errorf("err = %v, want no agent said to run in the panel", err)
	}
	if len(rt.ClosedPanels) != 0 {
		t.Errorf("panels closed = %v, want none", rt.ClosedPanels)
	}
}
