package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// detailWorld is one project whose home holds two agent panels, read by the
// given probe, surveyed once. It returns the core, its runtime, and the
// agents the survey found.
func detailWorld(t *testing.T, probe revier.AgentProbe) (*core.Core, *hosttest.FakeRuntime, []revier.AgentView) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude one"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude two"})
	projects := core.Prepare([]revier.Project{{Name: "demo", Path: "/p/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:demo$"}}},
	}}})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	r, err := c.Survey(context.Background(), projects, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Views) != 1 || len(r.Views[0].Agents) != 2 {
		t.Fatalf("survey found %+v, want one project with two agents", r.Views)
	}
	return c, rt, r.Views[0].Agents
}

// Details is what each agent said, in the views' order, read from the panels
// the survey listed: a detail asks no host anything.
func TestDetailsAreWhatEachAgentSaidAndAskNoHost(t *testing.T) {
	probe := hosttest.NewDetailedProbe("claude", "claude")
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	probe.Said["1"] = revier.AgentDetail{Message: "one said this", At: at}
	probe.Said["2"] = revier.AgentDetail{Message: "two said that"}
	c, rt, agents := detailWorld(t, probe)

	listed := rt.InstancesCalls
	got := c.Details(context.Background(), agents)
	if len(got) != 2 || got[0].Message != "one said this" || !got[0].At.Equal(at) || got[1].Message != "two said that" {
		t.Errorf("Details = %+v, want each agent's own, in order", got)
	}
	if rt.InstancesCalls != listed {
		t.Errorf("Details listed the host %d times, want none", rt.InstancesCalls-listed)
	}
	if probe.DetailCalls() != 2 {
		t.Errorf("the probe was asked %d times, want once per agent", probe.DetailCalls())
	}
}

// A detail is display: a probe that cannot say, and one that fails, leave it
// empty, and neither is an error to the caller.
func TestDetailsAreEmptyWhenTheProbeCannotSay(t *testing.T) {
	failing := hosttest.NewDetailedProbe("claude", "claude")
	failing.DetailErr = errors.New("transcript format changed")
	for name, probe := range map[string]revier.AgentProbe{
		"a probe with no Detail": &hosttest.FakeProbe{Harness: "claude", Marker: "claude"},
		"a probe that fails":     failing,
	} {
		t.Run(name, func(t *testing.T) {
			c, _, agents := detailWorld(t, probe)
			for i, d := range c.Details(context.Background(), agents) {
				if d != (revier.AgentDetail{}) {
					t.Errorf("detail %d = %+v, want it empty", i, d)
				}
			}
		})
	}
}

// Before any survey there is no listing to find a panel in, and an agent is
// given no detail rather than a host call.
func TestDetailsBeforeAnySurveyAreEmpty(t *testing.T) {
	probe := hosttest.NewDetailedProbe("claude", "claude")
	probe.Said["1"] = revier.AgentDetail{Message: "said"}
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	got := c.Details(context.Background(), []revier.AgentView{{Panel: "1", Ref: revier.TargetRef{Host: "rt", ID: "1"}}})
	if len(got) != 1 || got[0] != (revier.AgentDetail{}) {
		t.Errorf("Details = %+v, want one empty detail", got)
	}
	if rt.InstancesCalls != 0 || probe.DetailCalls() != 0 {
		t.Errorf("listed %d times and asked the probe %d times, want neither", rt.InstancesCalls, probe.DetailCalls())
	}
}
