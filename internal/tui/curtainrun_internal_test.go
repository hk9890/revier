package tui

import (
	"os"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A hide that comes once the walk ended leaves the summary up: the show of
// the end brings the curtains back, and a hide after it would put them away
// again. A hide while the walk runs still hides them.
func TestAHideAfterTheEndLeavesTheSummaryUp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		end   *curtain.End
		hides bool
	}{
		{"running", nil, true},
		{"ended", &curtain.End{Title: "Restore cancelled"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wm := hosttest.New("wm")
			ref := wm.Add(core.CurtainTitle(revier.Monitor{Primary: true}), core.CurtainClass)
			r := &curtainRun{state: curtain.State{End: tc.end}}
			r.hide(&core.Core{Window: wm})()
			if hid := slices.Contains(wm.Ops(), "above "+ref.ID+" off"); hid != tc.hides {
				t.Errorf("window calls = %q, want a hide %v", wm.Ops(), tc.hides)
			}
		})
	}
}

// A walk cancelled before its last steps ends with the bar at the steps it
// walked: the steps it did not reach are in the rows, not in the count.
func TestACancelledWalkCountsOnlyTheStepsItWalked(t *testing.T) {
	dir, err := os.MkdirTemp("", "rc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	srv, err := curtain.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	r := &curtainRun{srv: srv, state: curtain.State{Mode: "restore", Rows: []curtain.Row{
		{Project: "alpha", Label: "home", State: curtain.RowDone},
		{Project: "bravo", Label: "home", State: curtain.RowRunning},
		{Project: "charlie", Label: "home", State: curtain.RowWaiting},
	}}}
	r.finish(&core.Core{}, curtain.End{Title: "Restore cancelled"}, 0, nil)
	if r.state.N != 1 || r.state.Of != 3 {
		t.Errorf("bar = %d of %d, want 1 of 3", r.state.N, r.state.Of)
	}
	if got := r.state.Rows[2]; got.State != curtain.RowSkipped || got.Note != "not reached" {
		t.Errorf("row not reached = %+v, want skipped, not reached", got)
	}
}

// A step the recheck renamed is still the step the plan has: an agent panel
// whose agent exited is named by its panel then. Its row is marked, keeps the
// plan's label, and no second row is added.
func TestARenamedStepMarksItsPlannedRow(t *testing.T) {
	planned := core.CloseStep{
		Project: "alpha", Ref: revier.TargetRef{Host: "kitty", ID: "1"}, Panel: "2",
		Agents: []revier.AgentView{{State: revier.AgentState{Harness: "claude"}}},
	}
	rows, steps := closeRows([]core.CloseStep{planned})
	r := &curtainRun{state: curtain.State{Rows: rows}, steps: steps}
	walked := planned
	walked.Agents = nil
	r.take(core.Progress{Phase: core.PhaseClose, Done: true, Closed: core.CloseResult{CloseStep: walked}})
	want := []curtain.Row{{Project: "alpha", Label: "claude", State: curtain.RowDone}}
	if !slices.Equal(r.state.Rows, want) {
		t.Errorf("rows = %+v, want %+v", r.state.Rows, want)
	}
}
