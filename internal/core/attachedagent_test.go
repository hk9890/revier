// Layer L2: an agent in a terminal attached to a project by hand is surveyed
// like one in a declared target, so a shutdown treats it the same.
package core_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// attachScratch adds a terminal no rule declares, with an agent in it, and
// attaches it to the revier project.
func attachScratch(rt *hosttest.FakeRuntime) (revier.TargetRef, map[revier.ProjectName][]revier.TargetRef) {
	ref := rt.Add("scratch", "kitty", agent("9", "", ""))
	return ref, map[revier.ProjectName][]revier.TargetRef{"revier": {ref}}
}

func stepFor(plan []core.CloseStep, ref revier.TargetRef) (core.CloseStep, bool) {
	i := slices.IndexFunc(plan, func(s core.CloseStep) bool { return s.Ref == ref })
	if i < 0 {
		return core.CloseStep{}, false
	}
	return plan[i], true
}

// A busy agent in an attached terminal is in the project's agents, and a
// shutdown of the targets alone leaves the terminal open.
func TestShutdownTargetsLeavesAnAttachedTerminalWithABusyAgent(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusRunning)
	ref, attached := attachScratch(rt)
	report := survey(t, c, projects, attached)

	if !slices.ContainsFunc(report.Views[0].Agents, func(a revier.AgentView) bool {
		return a.Ref == ref && a.Panel == "9" && a.State.Status == revier.StatusRunning
	}) {
		t.Fatalf("agents = %+v, want the running agent in the attached terminal", report.Views[0].Agents)
	}
	if s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownTargets), ref); ok {
		t.Errorf("targets plan closes the attached terminal: %+v", s)
	}
	s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownAll), ref)
	if !ok || !s.Busy() {
		t.Errorf("full plan step = %+v, %v; want the attached terminal marked busy", s, ok)
	}
}

// An idle agent in an attached terminal lets a full shutdown close the
// terminal without a refusal; a shutdown of the targets alone still keeps it,
// as it keeps every instance that holds an agent.
func TestShutdownClosesAnAttachedTerminalWithAnIdleAgent(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	ref, attached := attachScratch(rt)
	report := survey(t, c, projects, attached)

	s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownAll), ref)
	if !ok || s.Busy() || len(s.Agents) != 1 || s.Agents[0].Panel != "9" {
		t.Errorf("full plan step = %+v, %v; want the close step with its idle agent", s, ok)
	}
	if s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownTargets), ref); ok {
		t.Errorf("targets plan closes the attached terminal that holds an agent: %+v", s)
	}
}

// countingProbe counts the panels it reads.
type countingProbe struct {
	*hosttest.FakeProbe
	reads int
}

func (p *countingProbe) Inspect(ctx context.Context, panel revier.Panel) (revier.AgentState, error) {
	p.reads++
	return p.FakeProbe.Inspect(ctx, panel)
}

// Probing attached terminals costs one read per agent panel and no host call
// per project: the runtime lists once for 90 projects as for one, and a
// terminal is read once however many projects are surveyed beside it.
func TestSurveyProbesAttachedTerminalsWithoutAPerProjectCost(t *testing.T) {
	for _, tc := range []struct {
		projects, attachedTo int
	}{{1, 1}, {90, 1}, {90, 90}} {
		rt := hosttest.NewRuntime("rt")
		probe := &countingProbe{FakeProbe: &hosttest.FakeProbe{Harness: "claude", Marker: "claude"}}
		c := &core.Core{Runtime: rt, Window: hosttest.New("wm"), Probes: []revier.AgentProbe{probe}}
		projects := core.Prepare(benchProjects(tc.projects))
		attached := map[revier.ProjectName][]revier.TargetRef{}
		for i := range tc.attachedTo {
			name := projects[i].Name
			attached[name] = []revier.TargetRef{rt.Add(fmt.Sprintf("scratch-%d", i), "kitty",
				revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"},
				revier.Panel{ID: "2", Kind: revier.PanelShell, Title: "zsh"},
			)}
		}
		if _, err := c.Survey(context.Background(), projects, nil, attached); err != nil {
			t.Fatal(err)
		}
		if rt.InstancesCalls != 1 || probe.reads != tc.attachedTo {
			t.Errorf("%d projects, %d attached: %d listings, %d probe reads; want 1 listing and %d reads",
				tc.projects, tc.attachedTo, rt.InstancesCalls, probe.reads, tc.attachedTo)
		}
	}
}
