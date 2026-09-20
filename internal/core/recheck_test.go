package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A tab step is rechecked against every panel of its tab, so an agent that
// turns busy in the panel beside the one that names the step refuses the
// close.
func TestShutdownRefusesABusyAgentBesideTheMarkedPanelOfATab(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})
	if len(plan) != 1 {
		t.Fatalf("plan = %+v, want one tab step", plan)
	}

	c.Probes = []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}}
	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects})
	var refused *core.BusyRefusal
	if !errors.As(err, &refused) || out != nil {
		t.Fatalf("shutdown = %+v, %v; want a busy refusal", out, err)
	}
	if len(refused.Plan) != 1 || !refused.Plan[0].Busy() {
		t.Errorf("refused plan = %+v, want the busy agent in panel 3", refused.Plan)
	}
}

// The same tab closes once the agent beside it is idle again, and the close
// is not refused for an agent the plan was drawn with.
func TestShutdownClosesWhenTheRecheckFindsNoBusyAgent(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})

	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if closed, _, _ := out.Counts(); closed != 1 {
		t.Errorf("counts = %d closed, want the tab closed", closed)
	}
}

// A shutdown that cannot read the plan's agents again closes nothing: a
// degraded survey reports no agent, and no agent read is not idle.
func TestShutdownRefusesWhenTheRecheckCannotBeRead(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{})
	if err == nil || out != nil {
		t.Fatalf("shutdown = %+v, %v; want a refusal with nothing surveyed", out, err)
	}
	var refused *core.BusyRefusal
	if errors.As(err, &refused) {
		t.Errorf("err = %v, want an unreadable recheck rather than a busy refusal", err)
	}
}
