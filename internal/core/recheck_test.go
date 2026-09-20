package core_test

import (
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A tab step is rechecked against every panel of its tab, so an agent that
// turns busy in the panel beside the one that names the step is seen.
func TestRecheckReadsEveryPanelOfATabStep(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})
	if len(plan) != 1 {
		t.Fatalf("plan = %+v, want one tab step", plan)
	}

	c.Probes = []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}}
	out, err := c.Recheck(survey(t, c, projects, nil), plan)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if len(out) != 1 || !out[0].Busy() {
		t.Errorf("rechecked step = %+v, want the busy agent in panel 3", out)
	}
}
