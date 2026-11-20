package tui

import (
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
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
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
