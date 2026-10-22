package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// saving is the save a shutdown runs before its closes, as both surfaces
// make it.
func saving(c *core.Core, root string, projects []core.Project) core.ShutdownOpts {
	return core.ShutdownOpts{Projects: projects, Before: func(ctx context.Context, now core.Report) error {
		_, _, _, err := app.SaveChanged(ctx, c, root, now, session.ReasonShutdown, time.Now())
		return err
	}}
}

func shutDownOnRecord(c *core.Core) bool { return c.Ledger.State().ShutDown != nil }

// A full shutdown records itself once the save is made and before the first
// close, since the close may end the caller's own terminal, and names the
// session it saved. The record stays after the close.
func TestAFullShutdownIsRecordedBeforeTheFirstClose(t *testing.T) {
	c, rt, report, projects := desktop(t)
	root := t.TempDir()
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)
	var atFirstClose []bool
	rt.OnClose = func(revier.TargetRef) { atFirstClose = append(atFirstClose, shutDownOnRecord(c)) }

	if _, err := app.Shutdown(context.Background(), c, root, plan, 0, saving(c, root, projects), true); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(atFirstClose) == 0 || !atFirstClose[0] {
		t.Errorf("on record at each close = %v, want it written before the first", atFirstClose)
	}
	newest, err := session.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if sd := c.Ledger.State().ShutDown; sd == nil || sd.Session != newest.ID {
		t.Errorf("shutdown on record = %+v, want one naming session %s", sd, newest.ID)
	}
}

// A shutdown of one project, or of less than everything, is no full one, and
// records nothing.
func TestAShutdownThatIsNotFullRecordsNothing(t *testing.T) {
	c, _, report, projects := desktop(t)
	root := t.TempDir()
	plan := c.ShutdownPlan(report, "demo", core.ShutdownAll)
	if _, err := app.Shutdown(context.Background(), c, root, plan, 0, saving(c, root, projects), false); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if shutDownOnRecord(c) {
		t.Error("a shutdown that is not full was recorded")
	}
}

// A close the busy guard refuses, or whose save fails, closed nothing and
// records nothing.
func TestARefusedOrUnsavedShutdownRecordsNothing(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		c, rt, _, projects := desktop(t)
		c.Probes[0].(*hosttest.FakeResumableProbe).State = revier.AgentState{Harness: "claude", Status: revier.StatusRunning}
		root := t.TempDir()
		plan := c.ShutdownPlan(surveyed(t, c, projects), "", core.ShutdownAll)
		_, err := app.Shutdown(context.Background(), c, root, plan, 0, saving(c, root, projects), true)
		if !errors.Is(err, core.ErrAgentBusy) || shutDownOnRecord(c) || len(rt.Closed) != 0 {
			t.Errorf("err = %v, recorded %v, closed %v; want the refusal and nothing recorded or closed", err, shutDownOnRecord(c), rt.Closed)
		}
	})
	t.Run("save failed", func(t *testing.T) {
		c, _, report, projects := desktop(t)
		failed := errors.New("disk full")
		opts := core.ShutdownOpts{Projects: projects, Before: func(context.Context, core.Report) error { return failed }}
		_, err := app.Shutdown(context.Background(), c, t.TempDir(), c.ShutdownPlan(report, "", core.ShutdownAll), 0, opts, true)
		if !errors.Is(err, failed) || shutDownOnRecord(c) {
			t.Errorf("err = %v, recorded %v; want the save's failure and nothing recorded", err, shutDownOnRecord(c))
		}
	})
}

// A full shutdown cancelled on its walk takes its record back: what it did
// not reach is still open.
func TestACancelledFullShutdownTakesItsRecordBack(t *testing.T) {
	c, rt, report, projects := desktop(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	rt.OnClose = func(revier.TargetRef) { cancel() }

	closed, err := app.Shutdown(ctx, c, root, c.ShutdownPlan(report, "", core.ShutdownAll), 0, saving(c, root, projects), true)
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(closed) != 1 {
		t.Fatalf("closed = %+v, want the walk stopped after the first step", closed)
	}
	if shutDownOnRecord(c) {
		t.Error("the cancelled shutdown is still on record")
	}
}
