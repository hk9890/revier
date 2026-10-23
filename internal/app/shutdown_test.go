package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/state"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// saving is the save a shutdown runs before its closes, as both surfaces
// make it.
func saving(c *core.Core, root string) func(context.Context, core.Report) (session.Session, error) {
	return func(ctx context.Context, now core.Report) (session.Session, error) {
		stored, _, _, err := app.SaveChanged(ctx, c, root, now, session.ReasonShutdown, time.Now())
		return stored, err
	}
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

	if _, err := app.Shutdown(context.Background(), c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, root), app.NewShutdownRecord(c)); err != nil {
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

// A full shutdown whose save recorded nothing, or that saved nothing at all,
// names no session: the start dialog then offers none as its own.
func TestAFullShutdownThatSavedNothingNamesNoSession(t *testing.T) {
	nothing := func(context.Context, core.Report) (session.Session, error) { return session.Session{}, nil }
	for name, save := range map[string]func(context.Context, core.Report) (session.Session, error){"nothing open": nothing, "no save": nil} {
		c, _, report, projects := desktop(t)
		if _, err := app.Shutdown(context.Background(), c, c.ShutdownPlan(report, "", core.ShutdownAll), 0, core.ShutdownOpts{Projects: projects}, save, app.NewShutdownRecord(c)); err != nil {
			t.Fatalf("%s: Shutdown: %v", name, err)
		}
		if sd := c.Ledger.State().ShutDown; sd == nil || sd.Session != "" {
			t.Errorf("%s: shutdown on record = %+v, want one naming no session", name, sd)
		}
	}
}

// A shutdown of one project, or of less than everything, is no full one, and
// records nothing.
func TestAShutdownThatIsNotFullRecordsNothing(t *testing.T) {
	c, _, report, projects := desktop(t)
	root := t.TempDir()
	plan := c.ShutdownPlan(report, "demo", core.ShutdownAll)
	if _, err := app.Shutdown(context.Background(), c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, root), nil); err != nil {
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
		_, err := app.Shutdown(context.Background(), c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, root), app.NewShutdownRecord(c))
		if !errors.Is(err, core.ErrAgentBusy) || shutDownOnRecord(c) || len(rt.Closed) != 0 {
			t.Errorf("err = %v, recorded %v, closed %v; want the refusal and nothing recorded or closed", err, shutDownOnRecord(c), rt.Closed)
		}
	})
	t.Run("save failed", func(t *testing.T) {
		c, _, report, projects := desktop(t)
		failed := errors.New("disk full")
		save := func(context.Context, core.Report) (session.Session, error) { return session.Session{}, failed }
		_, err := app.Shutdown(context.Background(), c, c.ShutdownPlan(report, "", core.ShutdownAll), 0, core.ShutdownOpts{Projects: projects}, save, app.NewShutdownRecord(c))
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

	closed, err := app.Shutdown(ctx, c, c.ShutdownPlan(report, "", core.ShutdownAll), 0, core.ShutdownOpts{Projects: projects}, saving(c, root), app.NewShutdownRecord(c))
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

// A cancel that comes during the last close reaches no step: the full
// shutdown closed everything, and stays on record.
func TestAFullShutdownCancelledDuringItsLastCloseKeepsItsRecord(t *testing.T) {
	c, rt, report, projects := desktop(t)
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.OnClose = func(revier.TargetRef) {
		if len(rt.Closed) == len(plan) {
			cancel()
		}
	}

	closed, err := app.Shutdown(ctx, c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, t.TempDir()), app.NewShutdownRecord(c))
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(plan) < 2 || len(closed) != len(plan) {
		t.Fatalf("closed %d of %d steps, want every step of a plan of two or more", len(closed), len(plan))
	}
	if !shutDownOnRecord(c) {
		t.Error("a shutdown that walked every step was taken off the record")
	}
}

// A cancel that comes during the save records nothing and closes nothing,
// and a surface that took the record back on its cancel is not followed by a
// write. The save was made, so the shutdown does not fail, and the log says
// the save succeeded: the cancel is the walk's line.
func TestAFullShutdownCancelledDuringItsSaveRecordsNothing(t *testing.T) {
	c, rt, report, projects := desktop(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	save := func(context.Context, core.Report) (session.Session, error) {
		cancel()
		return session.Session{}, nil
	}
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	closed, err := app.Shutdown(ctx, c, c.ShutdownPlan(report, "", core.ShutdownAll), 0, core.ShutdownOpts{Projects: projects}, save, app.NewShutdownRecord(c))
	if err != nil || len(closed) != 0 || shutDownOnRecord(c) || len(rt.Closed) != 0 {
		t.Errorf("err = %v, walked %d, recorded %v, closed %v; want no error, nothing walked, recorded or closed", err, len(closed), shutDownOnRecord(c), rt.Closed)
	}
	var saveLine, cancelLine bool
	for line := range strings.Lines(logged.String()) {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		switch entry["msg"] {
		case "shutdown save":
			saveLine = true
			if _, failed := entry["err"]; failed || entry["level"] != "INFO" {
				t.Errorf("save logged %s, want it logged as made", line)
			}
		case "shutdown cancelled":
			cancelLine = true
		}
	}
	if !saveLine || !cancelLine {
		t.Errorf("log = %s, want the save line and the cancel line", logged.String())
	}
}

// earlierShutdown puts the record of an earlier full shutdown in c's state.
func earlierShutdown(c *core.Core) state.ShutDown {
	at := time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)
	c.Ledger.Update(func(st *state.State) bool {
		st.MarkShutDown(at, "earlier")
		return true
	})
	return *c.Ledger.State().ShutDown
}

// A surface that quits on the second ctrl+c before this shutdown wrote its
// record takes nothing back: the record of an earlier shutdown stays.
func TestAnAbandonBeforeTheRecordIsWrittenKeepsAnEarlierRecord(t *testing.T) {
	c, rt, report, projects := desktop(t)
	earlier := earlierShutdown(c)
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)
	record := app.NewShutdownRecord(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The recheck lists the runtime before the save: both presses come
	// there.
	rt.OnInstances = func() {
		rt.OnInstances = nil
		cancel()
		record.Abandon()
	}

	if _, err := app.Shutdown(ctx, c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, t.TempDir()), record); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown: %v", err)
	}
	if sd := c.Ledger.State().ShutDown; sd == nil || *sd != earlier {
		t.Errorf("shutdown on record = %+v, want the earlier one %+v kept", sd, earlier)
	}
	if len(rt.Closed) != 0 {
		t.Errorf("closed %v, want nothing", rt.Closed)
	}
}

// A surface that quits on the second ctrl+c while the last step hangs keeps
// the record: the walk reached every step, the hung one too.
func TestAnAbandonDuringTheLastStepKeepsTheRecord(t *testing.T) {
	c, rt, report, projects := desktop(t)
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)
	record := app.NewShutdownRecord(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var onRecord bool
	rt.OnClose = func(revier.TargetRef) {
		if len(rt.Closed) == len(plan) {
			cancel()
			record.Abandon()
			onRecord = shutDownOnRecord(c)
		}
	}

	if _, err := app.Shutdown(ctx, c, plan, 0, core.ShutdownOpts{Projects: projects}, saving(c, t.TempDir()), record); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(plan) < 2 || !onRecord || !shutDownOnRecord(c) {
		t.Errorf("abandoned during the last of %d steps: on record %v, then %v; want it kept", len(plan), onRecord, shutDownOnRecord(c))
	}
}

// A surface that quits on the second ctrl+c takes the record back when the
// walk left a step not reached, and keeps it once every step was walked.
func TestAnAbandonTakesTheRecordBackOnlyWhenAStepWasNotReached(t *testing.T) {
	for _, at := range []int{1, 2} {
		c, rt, report, projects := desktop(t)
		plan := c.ShutdownPlan(report, "", core.ShutdownAll)
		if len(plan) != 2 {
			t.Fatalf("plan = %+v, want two steps", plan)
		}
		record := app.NewShutdownRecord(c)
		ctx, cancel := context.WithCancel(context.Background())
		// The first press comes during step at, and the second once that
		// step is done.
		rt.OnClose = func(revier.TargetRef) {
			if len(rt.Closed) == at {
				cancel()
			}
		}
		var onRecord bool
		opts := core.ShutdownOpts{Projects: projects, Progress: func(p core.Progress) {
			if p.Phase == core.PhaseClose && p.Done && p.N == at {
				record.Abandon()
				onRecord = shutDownOnRecord(c)
			}
		}}

		if _, err := app.Shutdown(ctx, c, plan, 0, opts, saving(c, t.TempDir()), record); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		cancel()
		if want := at == len(plan); onRecord != want || shutDownOnRecord(c) != want {
			t.Errorf("abandoned after %d of %d steps: on record %v, then %v; want %v", at, len(plan), onRecord, shutDownOnRecord(c), want)
		}
	}
}
