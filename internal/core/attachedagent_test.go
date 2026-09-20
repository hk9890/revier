// Layer L2: an agent in a terminal attached to a project by hand is surveyed
// like one in a declared target, so a shutdown treats it the same.
package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// attachScratch opens a terminal no rule declares, with an agent in it, and
// attaches it to the revier project the way the product does: the user points
// at the window in front of them, and the core records the terminal inside it
// beside the window. It returns the terminal, which is what the survey
// reports, and the attachment as state holds it.
func attachScratch(t *testing.T, c *core.Core, rt *hosttest.FakeRuntime, wm *hosttest.Fake) (revier.TargetRef, map[revier.ProjectName][]revier.TargetRef) {
	t.Helper()
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	term := rt.AddInstance(revier.Instance{
		Title: "scratch", Class: "kitty", PID: 4242,
		Panels: []revier.Panel{agent("9", "", "")},
	})
	window := wm.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	wm.SetFocus(window)
	refs := c.Attachment(context.Background(), window)
	if len(refs) != 2 || refs[0] != window || refs[1] != term {
		t.Fatalf("attachment = %v, want the window %v and the terminal %v inside it", refs, window, term)
	}
	return term, map[revier.ProjectName][]revier.TargetRef{"revier": refs}
}

// The window and the terminal in it are one attached row, the terminal's: the
// side that holds the panels, so what the row closes is what holds the agent.
func TestAnAttachedTerminalIsOneRow(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	term, attached := attachScratch(t, c, rt, wm)
	report := survey(t, c, projects, attached)

	var rows []revier.TargetRef
	for _, tv := range report.Views[0].Targets {
		if tv.Attached {
			rows = append(rows, tv.Ref)
		}
	}
	if len(rows) != 1 || rows[0] != term {
		t.Errorf("attached rows = %v, want the terminal %v alone", rows, term)
	}
}

// An ambiguous window is attached alone, as D63 and D67 demand, and then no
// agent is reported for it: two terminals of one process with one title
// cannot be told apart, and attaching the wrong one is worse than attaching
// no terminal at all.
func TestAnAmbiguousWindowAttachesAlone(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusRunning)
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	for range 2 {
		rt.AddInstance(revier.Instance{
			Title: "scratch", Class: "kitty", PID: 4242,
			Panels: []revier.Panel{agent("9", "", "")},
		})
	}
	window := wm.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})

	refs := c.Attachment(context.Background(), window)
	if len(refs) != 1 || refs[0] != window {
		t.Fatalf("attachment = %v, want the window alone", refs)
	}
	report := survey(t, c, projects, map[revier.ProjectName][]revier.TargetRef{"revier": refs})
	for _, a := range report.Views[0].Agents {
		if a.Panel == "9" {
			t.Errorf("agents = %+v, want no agent for a window no terminal pairs with", report.Views[0].Agents)
		}
	}
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
	c, rt, wm, projects := openDesktop(t, revier.StatusRunning)
	ref, attached := attachScratch(t, c, rt, wm)
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
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	ref, attached := attachScratch(t, c, rt, wm)
	report := survey(t, c, projects, attached)

	s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownAll), ref)
	if !ok || s.Busy() || len(s.Agents) != 1 || s.Agents[0].Panel != "9" {
		t.Errorf("full plan step = %+v, %v; want the close step with its idle agent", s, ok)
	}
	if s, ok := stepFor(c.ShutdownPlan(report, "", core.ShutdownTargets), ref); ok {
		t.Errorf("targets plan closes the attached terminal that holds an agent: %+v", s)
	}
}

// Probing attached terminals costs one read per agent panel and no host call
// per project: the runtime lists once for 90 projects as for one, and a
// terminal is read once however many projects are surveyed beside it.
func TestSurveyProbesAttachedTerminalsWithoutAPerProjectCost(t *testing.T) {
	for _, tc := range []struct {
		projects, attachedTo int
	}{{1, 1}, {90, 1}, {90, 90}} {
		rt := hosttest.NewRuntime("rt")
		probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude"}
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
		if rt.InstancesCalls != 1 || probe.Reads != tc.attachedTo {
			t.Errorf("%d projects, %d attached: %d listings, %d probe reads; want 1 listing and %d reads",
				tc.projects, tc.attachedTo, rt.InstancesCalls, probe.Reads, tc.attachedTo)
		}
	}
}

// A terminal attached to two projects is read once a survey and listed under
// both: the cache that spares the second read is the survey's, not one view's.
func TestSurveyReadsATerminalAttachedToTwoProjectsOnce(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}}
	c := &core.Core{Runtime: rt, Window: hosttest.New("wm"), Probes: []revier.AgentProbe{probe}}
	projects := core.Prepare(benchProjects(2))
	ref := rt.Add("scratch", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	attached := map[revier.ProjectName][]revier.TargetRef{
		projects[0].Name: {ref}, projects[1].Name: {ref},
	}

	report, err := c.Survey(context.Background(), projects, nil, attached)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Reads != 1 {
		t.Errorf("probe reads = %d, want one for the one terminal", probe.Reads)
	}
	for i, v := range report.Views {
		if len(v.Agents) != 1 || v.Agents[0].Panel != "1" {
			t.Errorf("view %d agents = %+v, want the agent listed under every project it is attached to", i, v.Agents)
		}
	}
}

// A shutdown of the agents alone ends the agent in a hand-attached terminal
// too, and leaves the terminal open. An agent is an agent wherever the user
// put it: the scope is about agents and not about how their instance came to
// be known (decisions.md D78).
func TestShutdownAgentsEndsAnAgentInAnAttachedTerminal(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	term, attached := attachScratch(t, c, rt, wm)
	plan := c.ShutdownPlan(survey(t, c, projects, attached), "", core.ShutdownAgents)

	i := slices.IndexFunc(plan, func(s core.CloseStep) bool { return s.Ref == term })
	if i < 0 {
		t.Fatalf("plan = %+v, want a step for the agent in the attached terminal", plan)
	}
	if plan[i].Panel != "9" || plan[i].Action != core.ClosePanel {
		t.Errorf("step = %+v, want panel 9 of the terminal closed and the terminal left open", plan[i])
	}
}

// The picker focuses an attached instance by its ref alone, and an
// attachment is now a terminal as well as a window: the OS window it lives in
// is raised with it, because kitty on Wayland cannot raise its own
// (decisions.md D63).
func TestFocusingAnAttachedTerminalRaisesItsWindow(t *testing.T) {
	c, rt, wm, _ := openDesktop(t, revier.StatusIdle)
	term, _ := attachScratch(t, c, rt, wm)
	window, _ := wm.Focused(context.Background())

	if err := c.Focus(context.Background(), term); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if !slices.Contains(rt.Focuses, term) {
		t.Errorf("runtime focuses = %v, want the terminal %v", rt.Focuses, term)
	}
	if got := wm.Focuses; len(got) != 1 || got[0] != window {
		t.Errorf("window focuses = %v, want the OS window %v raised", got, window)
	}
}

// A terminal the window host lists no window for is not focused inside the
// terminal either: that is the "is ready" notice D63 refuses to produce.
func TestFocusingAnUnraisableTerminalMovesNothing(t *testing.T) {
	c, rt, wm, _ := openDesktop(t, revier.StatusIdle)
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	term := rt.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})

	if err := c.Focus(context.Background(), term); !errors.Is(err, core.ErrUnraisable) {
		t.Fatalf("err = %v, want ErrUnraisable", err)
	}
	if len(rt.Focuses) != 0 || len(wm.Focuses) != 0 {
		t.Errorf("focuses = %v %v, want none", rt.Focuses, wm.Focuses)
	}
}
