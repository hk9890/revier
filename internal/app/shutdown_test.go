package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
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

// fullShutdown is a close of every project, all of each, that saves first:
// the one the state records.
func fullShutdown(c *core.Core, root string, plan []core.CloseStep, projects []core.Project) app.CloseRequest {
	return app.CloseRequest{Core: c, StateRoot: root, Plan: plan, Projects: projects, Save: true}
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

	res, err := app.Close(context.Background(), fullShutdown(c, root, plan, projects))
	if err != nil {
		t.Fatalf("Close: %v", err)
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
	if !res.Recorded || res.Save == nil || !res.Save.New || res.Save.Session.ID != newest.ID {
		t.Errorf("result = %+v, want it recorded and the save of session %s", res, newest.ID)
	}
}

// A close of every project saves as a shutdown, and one of a single project
// as that project's close.
func TestACloseSavesWithTheReasonOfItsScope(t *testing.T) {
	for only, want := range map[revier.ProjectName]session.Reason{"": session.ReasonShutdown, "demo": session.ReasonProjectClosed} {
		c, _, report, projects := desktop(t)
		root := t.TempDir()
		req := fullShutdown(c, root, c.ShutdownPlan(report, only, core.ShutdownAll), projects)
		req.Project = only
		if _, err := app.Close(t.Context(), req); err != nil {
			t.Fatalf("%q: Close: %v", only, err)
		}
		all, err := session.List(root)
		if err != nil || len(all) != 1 || all[0].Reason != want {
			t.Errorf("%q: sessions = %+v, %v; want one saved as %q", only, all, err, want)
		}
	}
}

// A full shutdown on record removes what a surface last saw: it was a clean
// end, and the file would read as an end with no shutdown once something
// opened and ended the record. One that is not full leaves it.
func TestAFullShutdownRemovesWhatWasLastSeen(t *testing.T) {
	for _, only := range []revier.ProjectName{"", "demo"} {
		c, _, report, projects := desktop(t)
		root := t.TempDir()
		if err := session.SaveSeen(root, session.Session{Projects: []session.Project{{Name: "project-00"}}}); err != nil {
			t.Fatal(err)
		}
		req := app.CloseRequest{Core: c, StateRoot: root, Plan: c.ShutdownPlan(report, only, core.ShutdownAll), Projects: projects, Project: only}
		if _, err := app.Close(context.Background(), req); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if _, err := session.LoadSeen(root); errors.Is(err, session.ErrNoSession) != (only == "") {
			t.Errorf("close of %q: last seen after it: %v, want it removed only by a full shutdown", only, err)
		}
	}
}

// A full shutdown that saved nothing names no session: the start dialog then
// offers none as its own.
func TestAFullShutdownThatSavedNothingNamesNoSession(t *testing.T) {
	c, _, report, projects := desktop(t)
	req := app.CloseRequest{Core: c, StateRoot: t.TempDir(), Plan: c.ShutdownPlan(report, "", core.ShutdownAll), Projects: projects}
	res, err := app.Close(context.Background(), req)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sd := c.Ledger.State().ShutDown; sd == nil || sd.Session != "" || res.Save != nil {
		t.Errorf("shutdown on record = %+v, save %+v; want one naming no session, and no save", sd, res.Save)
	}
}

// A shutdown of one project, or of less than everything, is no full one, and
// records nothing.
func TestAShutdownThatIsNotFullRecordsNothing(t *testing.T) {
	for name, req := range map[string]func(c *core.Core, report core.Report) app.CloseRequest{
		"one project": func(c *core.Core, report core.Report) app.CloseRequest {
			return app.CloseRequest{Plan: c.ShutdownPlan(report, "demo", core.ShutdownAll), Project: "demo"}
		},
		"the targets": func(c *core.Core, report core.Report) app.CloseRequest {
			return app.CloseRequest{Plan: c.ShutdownPlan(report, "", core.ShutdownTargets), Scope: core.ShutdownTargets}
		},
	} {
		c, _, report, projects := desktop(t)
		r := req(c, report)
		r.Core, r.StateRoot, r.Projects, r.Save = c, t.TempDir(), projects, true
		res, err := app.Close(context.Background(), r)
		if err != nil {
			t.Fatalf("%s: Close: %v", name, err)
		}
		if shutDownOnRecord(c) || res.Recorded {
			t.Errorf("%s: a shutdown that is not full was recorded", name)
		}
	}
}

// A record handed to a close that is not full is not written, and abandoning
// it keeps an earlier shutdown's record.
func TestACloseThatIsNotFullLeavesItsRecordUnwritten(t *testing.T) {
	c, _, report, projects := desktop(t)
	earlier := earlierShutdown(c)
	root := t.TempDir()
	record := app.NewShutdownRecord(c, root)
	req := app.CloseRequest{Core: c, StateRoot: root, Plan: c.ShutdownPlan(report, "demo", core.ShutdownAll), Projects: projects, Project: "demo", Record: record}

	if _, err := app.Close(t.Context(), req); err != nil {
		t.Fatalf("Close: %v", err)
	}
	record.Abandon()
	if record.Recorded() {
		t.Error("the record of a close that is not full was written")
	}
	if sd := c.Ledger.State().ShutDown; sd == nil || *sd != earlier {
		t.Errorf("shutdown on record = %+v, want the earlier one %+v kept", sd, earlier)
	}
}

// Abandoning the record of a close that is not full does not touch the
// ledger: the surface quits on its second ctrl+c without the state's lock.
func TestAbandoningARecordNoShutdownStartedTakesNoLock(t *testing.T) {
	c, _, report, projects := desktop(t)
	root := t.TempDir()
	record := app.NewShutdownRecord(c.WithLedger(untouchedLedger{c.Ledger, t}), root)
	req := app.CloseRequest{Core: c, StateRoot: root, Plan: c.ShutdownPlan(report, "demo", core.ShutdownAll), Projects: projects, Project: "demo", Record: record}

	if _, err := app.Close(t.Context(), req); err != nil {
		t.Fatalf("Close: %v", err)
	}
	record.Abandon()
}

// untouchedLedger fails the test on an update: one takes the state's lock.
type untouchedLedger struct {
	core.Ledger
	t *testing.T
}

func (l untouchedLedger) Update(func(*state.State) bool) *state.State {
	l.t.Error("the ledger was updated")
	return l.State()
}

// The close of one agent saves nothing and records nothing.
func TestAnAgentCloseSavesNothingAndRecordsNothing(t *testing.T) {
	c, _, report, projects := desktop(t)
	root := t.TempDir()
	if len(report.Views) != 1 || len(report.Views[0].Agents) != 1 {
		t.Fatalf("views = %+v, want one project with one agent", report.Views)
	}
	ag := report.Views[0].Agents[0]
	plan := c.ClosePlan(report, "demo", core.CloseRow{Agent: ag.Ref, Panel: ag.Panel})
	res, err := app.Close(t.Context(), app.CloseRequest{Core: c, StateRoot: root, Plan: plan, Projects: projects, Project: "demo"})
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if all, _ := session.List(root); len(all) != 0 || res.Save != nil || shutDownOnRecord(c) || len(res.Closed) != 1 {
		t.Errorf("sessions %+v, save %+v, recorded %v, closed %+v; want one step closed and nothing saved or recorded", all, res.Save, shutDownOnRecord(c), res.Closed)
	}
}

// A close the busy guard refuses returns the plan as the recheck read it,
// and the refusal is core.ErrAgentBusy. A close whose save fails closes
// nothing. Neither records anything.
func TestARefusedOrUnsavedShutdownRecordsNothing(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		c, rt, _, projects := desktop(t)
		c.Probes[0].(*hosttest.FakeResumableProbe).State = revier.AgentState{Harness: "claude", Status: revier.StatusRunning}
		plan := c.ShutdownPlan(surveyed(t, c, projects), "", core.ShutdownAll)
		res, err := app.Close(context.Background(), fullShutdown(c, t.TempDir(), plan, projects))
		if !errors.Is(err, core.ErrAgentBusy) || len(core.Busy(res.Refused)) == 0 || shutDownOnRecord(c) || len(rt.Closed) != 0 {
			t.Errorf("err = %v, refused %+v, recorded %v, closed %v; want the refusal with its busy plan and nothing recorded or closed", err, res.Refused, shutDownOnRecord(c), rt.Closed)
		}
	})
	t.Run("save failed", func(t *testing.T) {
		c, rt, report, projects := desktop(t)
		root := t.TempDir()
		// A file where the directory of the sessions belongs: no save is
		// written there.
		if err := os.WriteFile(session.Dir(root), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := app.Close(context.Background(), fullShutdown(c, root, c.ShutdownPlan(report, "", core.ShutdownAll), projects))
		if err == nil || shutDownOnRecord(c) || len(rt.Closed) != 0 {
			t.Errorf("err = %v, recorded %v, closed %v; want the save's failure and nothing recorded or closed", err, shutDownOnRecord(c), rt.Closed)
		}
	})
}

// unsaved is a ledger whose state file takes no write, as on a full disk:
// Update applies the change to the state it returns and loses it.
type unsaved struct{ *ledger }

func (l unsaved) Update(apply func(*state.State) bool) *state.State {
	st := l.State().Clone()
	apply(st)
	return st
}

// A full shutdown whose record did not reach the state still closes, but is
// not on record: the surface would quit into a start that offers nothing.
// What a surface last saw stays, since the next start finds no shutdown on
// record and offers it as an end with no shutdown.
func TestAFullShutdownWhoseRecordWasNotSavedIsNotRecorded(t *testing.T) {
	c, rt, report, projects := desktop(t)
	c = c.WithLedger(unsaved{c.Ledger.(*ledger)})
	root := t.TempDir()
	if err := session.SaveSeen(root, session.Session{Projects: []session.Project{{Name: "project-00"}}}); err != nil {
		t.Fatal(err)
	}

	res, err := app.Close(context.Background(), fullShutdown(c, root, c.ShutdownPlan(report, "", core.ShutdownAll), projects))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if res.Recorded || len(rt.Closed) == 0 {
		t.Errorf("recorded %v, closed %v; want the closes made and nothing recorded", res.Recorded, rt.Closed)
	}
	if _, err := session.LoadSeen(root); err != nil {
		t.Errorf("last seen after the shutdown: %v, want it kept", err)
	}
}

// A full shutdown whose record an open ended on its walk is not on record
// either (decisions.md D141): the next start offers nothing.
func TestAFullShutdownWhoseRecordAnOpenEndedIsNotRecorded(t *testing.T) {
	c, rt, report, projects := desktop(t)
	rt.OnClose = func(revier.TargetRef) {
		c.Ledger.Update(func(st *state.State) bool {
			st.Launched("demo", "notes", time.Now())
			return true
		})
	}

	res, err := app.Close(context.Background(), fullShutdown(c, t.TempDir(), c.ShutdownPlan(report, "", core.ShutdownAll), projects))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if res.Recorded || len(rt.Closed) == 0 {
		t.Errorf("recorded %v, closed %v; want the closes made and nothing recorded", res.Recorded, rt.Closed)
	}
}

// A full shutdown cancelled on its walk takes its record back: what it did
// not reach is still open.
func TestACancelledFullShutdownTakesItsRecordBack(t *testing.T) {
	c, rt, report, projects := desktop(t)
	ctx, cancel := context.WithCancel(context.Background())
	rt.OnClose = func(revier.TargetRef) { cancel() }

	res, err := app.Close(ctx, fullShutdown(c, t.TempDir(), c.ShutdownPlan(report, "", core.ShutdownAll), projects))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(res.Closed) != 1 {
		t.Fatalf("closed = %+v, want the walk stopped after the first step", res.Closed)
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

	res, err := app.Close(ctx, fullShutdown(c, t.TempDir(), plan, projects))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(plan) < 2 || len(res.Closed) != len(plan) {
		t.Fatalf("closed %d of %d steps, want every step of a plan of two or more", len(res.Closed), len(plan))
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
	req := fullShutdown(c, t.TempDir(), c.ShutdownPlan(report, "", core.ShutdownAll), projects)
	req.Saved = func(app.Saved) { cancel() }
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	res, err := app.Close(ctx, req)
	if err != nil || len(res.Closed) != 0 || shutDownOnRecord(c) || len(rt.Closed) != 0 {
		t.Errorf("err = %v, walked %d, recorded %v, closed %v; want no error, nothing walked, recorded or closed", err, len(res.Closed), shutDownOnRecord(c), rt.Closed)
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
	req := fullShutdown(c, t.TempDir(), c.ShutdownPlan(report, "", core.ShutdownAll), projects)
	req.Record = app.NewShutdownRecord(c, req.StateRoot)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The recheck lists the runtime before the save: both presses come
	// there.
	rt.OnInstances = func() {
		rt.OnInstances = nil
		cancel()
		req.Record.Abandon()
	}

	if _, err := app.Close(ctx, req); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Close: %v", err)
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
	req := fullShutdown(c, t.TempDir(), plan, projects)
	req.Record = app.NewShutdownRecord(c, req.StateRoot)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var onRecord bool
	rt.OnClose = func(revier.TargetRef) {
		if len(rt.Closed) == len(plan) {
			cancel()
			req.Record.Abandon()
			onRecord = shutDownOnRecord(c)
		}
	}

	if _, err := app.Close(ctx, req); err != nil {
		t.Fatalf("Close: %v", err)
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
		req := fullShutdown(c, t.TempDir(), plan, projects)
		req.Record = app.NewShutdownRecord(c, req.StateRoot)
		ctx, cancel := context.WithCancel(context.Background())
		// The first press comes during step at, and the second once that
		// step is done.
		rt.OnClose = func(revier.TargetRef) {
			if len(rt.Closed) == at {
				cancel()
			}
		}
		var onRecord bool
		req.Progress = func(p core.Progress) {
			if p.Phase == core.PhaseClose && p.Done && p.N == at {
				req.Record.Abandon()
				onRecord = shutDownOnRecord(c)
			}
		}

		if _, err := app.Close(ctx, req); err != nil {
			t.Fatalf("Close: %v", err)
		}
		cancel()
		if want := at == len(plan); onRecord != want || shutDownOnRecord(c) != want {
			t.Errorf("abandoned after %d of %d steps: on record %v, then %v; want %v", at, len(plan), onRecord, shutDownOnRecord(c), want)
		}
	}
}
