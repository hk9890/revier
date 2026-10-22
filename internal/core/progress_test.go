// Layer L2: what a shutdown and a restore report while they run, and where a
// cancel stops them.
package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// progressLog keeps every report as one line: the phase, the step, its label,
// and whether it is the report after the step.
type progressLog []string

func (l *progressLog) add(p core.Progress) {
	line := fmt.Sprintf("%s %d/%d %s", phaseNames[p.Phase], p.N, p.Of, p.Label)
	if p.Done {
		line += " done"
	}
	*l = append(*l, line)
}

var phaseNames = map[core.Phase]string{
	core.PhaseRecheck: "recheck", core.PhaseSave: "save", core.PhaseClose: "close", core.PhaseWait: "wait",
	core.PhaseSurvey: "survey", core.PhaseLaunch: "launch", core.PhaseHome: "home",
}

// A shutdown reports the recheck, the save and the wait as each starts, and
// each step before and after it closes, in the order it closes them: the
// step that ends this process last.
func TestShutdownReportsEachPhaseAndEachStepInTheOrderItCloses(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	c.Self = func(p revier.Panel) bool { return p.ID == "1" }
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	var got progressLog
	var closedAt []bool

	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{
		Projects: projects,
		Before:   func(context.Context, core.Report) error { return nil },
		Progress: func(p core.Progress) {
			got.add(p)
			if p.Phase == core.PhaseClose && p.Done {
				closedAt = append(closedAt, p.Closed.Err == nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	want := progressLog{
		"recheck 0/3 ", "save 0/3 ",
		"close 1/3 notes", "close 1/3 notes done",
		"close 2/3 editor", "close 2/3 editor done",
		"close 3/3 home", "close 3/3 home done",
		"wait 0/3 ",
	}
	if !slices.Equal(got, want) {
		t.Errorf("progress =\n%q\nwant\n%q", got, want)
	}
	if !slices.Equal(closedAt, []bool{true, true, true}) || len(out) != 3 {
		t.Errorf("results after each step = %v over %d steps, want each step's close as it came", closedAt, len(out))
	}
}

// A shutdown the busy guard refuses reports no save, no step and no wait: the
// recheck that refused it is all it did.
func TestARefusedShutdownReportsNothingPastTheRecheck(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusRunning)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	var got progressLog

	_, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects, Progress: got.add})
	var refused *core.BusyRefusal
	if !errors.As(err, &refused) {
		t.Fatalf("shutdown err = %v, want the busy agent to refuse it", err)
	}
	if want := (progressLog{"recheck 0/3 "}); !slices.Equal(got, want) {
		t.Errorf("progress = %q, want %q", got, want)
	}
}

// A restore reports each step before and after it is walked, a step that
// failed and one it kept included, and the return home before it.
func TestRestoreReportsEachStepAndTheReturnHome(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	rt.OpenErr = errors.New("no socket")
	c := &core.Core{Runtime: rt}
	projects := []core.Project{prepared(t, agentProject())}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home"}, {Name: "notes"},
	}}}}
	var got progressLog
	var failed []string

	_, _ = c.Restore(context.Background(), s, survey(t, c, projects, nil), projects, core.RestoreOpts{Progress: func(p core.Progress) {
		got.add(p)
		if p.Done && p.Restored.Err != nil {
			failed = append(failed, p.Label)
		}
	}})
	want := progressLog{
		"launch 1/2 home", "launch 1/2 home done",
		"launch 2/2 notes", "launch 2/2 notes done",
		"home 0/0 revier",
	}
	if !slices.Equal(got, want) {
		t.Errorf("progress =\n%q\nwant\n%q", got, want)
	}
	if !slices.Equal(failed, []string{"notes"}) {
		t.Errorf("failed steps = %v, want notes reported with its failure", failed)
	}
}

// A cancel during a restore stops it before the next step: the result holds
// what it walked, the next target is not opened, and it does not return home.
func TestACancelledRestoreStopsBeforeTheNextStep(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	home := rt.Add("session:revier", "kitty")
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: rt, Window: wm}
	projects := []core.Project{prepared(t, agentProject())}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "notes"}, {Name: "editor"},
	}}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.OnOpen = func(revier.Realization) { cancel() }
	var got progressLog

	out, back := c.Restore(ctx, s, survey(t, c, projects, nil), projects, core.RestoreOpts{Progress: got.add})
	if back != nil {
		t.Errorf("back = %v, want none: a cancelled restore does not try to return", back)
	}
	if len(out) != 1 || out[0].Target != "notes" {
		t.Errorf("walked %+v, want notes alone", out)
	}
	if len(wm.Opened) != 0 {
		t.Errorf("window host opened %+v, want the editor left after the cancel", wm.Opened)
	}
	if slices.Contains(rt.Focuses, home) {
		t.Errorf("Focuses = %+v, want no return home", rt.Focuses)
	}
	if want := (progressLog{"launch 1/2 notes", "launch 1/2 notes done"}); !slices.Equal(got, want) {
		t.Errorf("progress = %q, want %q", got, want)
	}
}
