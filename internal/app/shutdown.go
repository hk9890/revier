package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// CloseRequest is a close of what a plan names, as either surface asks for
// it: a shutdown of every project or of one, or the close of one agent.
type CloseRequest struct {
	Core      *core.Core
	StateRoot string
	Plan      []core.CloseStep
	// Projects are what the recheck surveys, as core.ShutdownOpts says.
	Projects []core.Project
	// Project is the one project the plan closes, and empty for every
	// project.
	Project revier.ProjectName
	Scope   core.ShutdownScope
	// Force closes a step whose agent is busy (decisions.md D78).
	Force bool
	// Save saves the session the close ends, when it changed.
	Save bool
	Wait time.Duration
	// Progress, when set, is told of each phase and step, as
	// core.ShutdownOpts says.
	Progress func(core.Progress)
	// Saved, when set, is told what the save came to once it is made and
	// before the first close.
	Saved func(Saved)
	// Record is the caller's handle on the record of a full shutdown, which
	// it abandons when it quits before Close returns. Close alone decides
	// whether it is written, and makes its own when the caller holds none.
	Record *ShutdownRecord
}

// Saved is what the save before a close came to: the session that holds what
// is open, zero when nothing was, whether this save wrote it, and what the
// save could not record.
type Saved struct {
	Session session.Session
	New     bool
	Gaps    core.SessionGaps
}

// CloseResult is what a close came to. Refused is the plan as the recheck
// read it when an agent turned busy since the plan was made; nothing closed
// then. Save is nil when no save ran. Recorded is a full shutdown the state
// now holds, as ShutdownRecord.Recorded says.
type CloseResult struct {
	Closed   core.Closed
	Refused  []core.CloseStep
	Save     *Saved
	Recorded bool
}

// Close closes the plan as core.Shutdown does, for both surfaces. A close of
// every project saves as a shutdown, and one of a single project as that
// project's close. The save runs after the busy guard, so a close the guard
// refuses saves nothing either (decisions.md D78). A refusal is returned as
// core.ErrAgentBusy, with the rechecked plan in Refused.
//
// A full shutdown, of every project, all of each, is recorded in the state
// with the session it saved, so the next surface offers to restore what it
// closed (D141). The record is written once the save is made and before the
// first close, because the close may end the caller's own terminal last. A
// close the busy guard refuses, or whose save fails, records nothing, and
// neither does one cancelled before the record is written: the cancel stops
// it before its first step. A record whose write did not reach the state does
// not stop the close; the shutdown is then not on record. A close cancelled
// on its walk takes the record back as ShutdownRecord says.
func Close(ctx context.Context, req CloseRequest) (CloseResult, error) {
	c := req.Core
	var res CloseResult
	record := req.Record
	switch {
	case req.Project != "" || req.Scope != core.ShutdownAll:
		record = nil
	case record == nil:
		record = NewShutdownRecord(c, req.StateRoot)
	}
	reason := session.ReasonShutdown
	if req.Project != "" {
		reason = session.ReasonProjectClosed
	}
	opts := core.ShutdownOpts{Force: req.Force, Projects: req.Projects, Progress: req.Progress}
	if record != nil {
		record.start(len(req.Plan))
		opts.Progress = func(p core.Progress) {
			if p.Phase == core.PhaseClose && !p.Done {
				record.reach()
			}
			if req.Progress != nil {
				req.Progress(p)
			}
		}
	}
	if req.Save || record != nil {
		opts.Before = func(saving context.Context, now core.Report) error {
			var id string
			if req.Save {
				stored, saved, gaps, err := SaveChanged(saving, c, req.StateRoot, now, reason, time.Now())
				if err != nil {
					return err
				}
				res.Save = &Saved{Session: stored, New: saved, Gaps: gaps}
				if req.Saved != nil {
					req.Saved(*res.Save)
				}
				id = stored.ID
			}
			// A cancel that came during the save leaves the save made and
			// writes no record. The walk reads the cancel before its first
			// step, so nothing closes.
			if record != nil {
				record.write(ctx, id)
			}
			return nil
		}
	}
	closed, err := c.Shutdown(ctx, req.Plan, req.Wait, opts)
	if record != nil && errors.Is(ctx.Err(), context.Canceled) {
		record.takeBack()
	}
	res.Closed = closed
	var refused *core.BusyRefusal
	if errors.As(err, &refused) {
		res.Refused = refused.Plan
	}
	res.Recorded = record != nil && record.Recorded()
	return res, err
}

// ShutdownRecord is one full shutdown's entry in the state: whether this
// shutdown wrote it, and how many of its steps the walk has reached. It alone
// decides whether a cancelled shutdown takes its record back, for the walk's
// own end and for a surface that quits before the walk returns.
//
// A cancel takes the record back only when this shutdown wrote it and the
// walk left a step not reached: what it did not reach is still open. A cancel
// that came before the record was written takes nothing back, so an earlier
// shutdown's record stays. One that came once the last step was reached left
// nothing, and the record stays, also when the second ctrl+c ends that step
// as it hangs. A step is reached as the core starts it, not as it ends: the
// count is the core's, read as it reports each step, not a surface's reading
// of those reports, which lags the walk.
type ShutdownRecord struct {
	c       *core.Core
	root    string // the state root, whose record of what was last seen the shutdown ends
	mu      sync.Mutex
	at      time.Time // when this shutdown wrote its record, which the state may not hold; zero when it holds none
	started bool      // Close runs a full shutdown on this record
	steps   int
	reached int
}

// NewShutdownRecord is the record of a full shutdown about to run, under the
// state root stateRoot.
func NewShutdownRecord(c *core.Core, stateRoot string) *ShutdownRecord {
	return &ShutdownRecord{c: c, root: stateRoot}
}

// Abandon is the end of a cancelled shutdown whose surface quits before the
// walk returns, on the second ctrl+c: the record is taken back as the walk
// would have taken it back on its cancel. The first ctrl+c cancelled the
// walk, so the walk writes no record after this.
//
// A surface holds a record for every close it runs, and Close starts only a
// full shutdown's. A record no shutdown started returns at once, without the
// state's lock, which another process can hold: the surface that quits has
// nothing to take back. One that started can still be on its write, so it
// waits for the lock.
func (r *ShutdownRecord) Abandon() {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started {
		r.takeBack()
	}
}

// Recorded reports whether the shutdown is on record: written by it, not
// taken back, and held by the state now. The ledger does not report a write
// that failed, and an open since ends the record (decisions.md D141), so the
// state is read: the surface that quits on a recorded shutdown stays when the
// state does not hold it, since its next start would not offer this one.
func (r *ShutdownRecord) Recorded() bool {
	r.mu.Lock()
	at := r.at
	r.mu.Unlock()
	if at.IsZero() {
		return false
	}
	sd := r.c.Ledger.State().ShutDown
	return sd != nil && sd.At.Equal(at)
}

func (r *ShutdownRecord) start(steps int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started, r.steps = true, steps
}

func (r *ShutdownRecord) reach() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reached++
}

// write records the shutdown unless ctx was cancelled. The cancel is read
// under the state's lock, so a surface that takes the record back after its
// cancel is never followed by this write. A recorded shutdown removes what a
// surface last saw: the end is a clean one, and a file left from before it
// would read as an end with no shutdown once the record ends (decisions.md
// D150). A record that did not reach the state leaves that file: the next
// start finds no shutdown on record and offers it.
func (r *ShutdownRecord) write(ctx context.Context, sessionID string) {
	at := time.Now().Round(0)
	r.c.Ledger.Update(func(st *state.State) bool {
		if ctx.Err() != nil {
			return false
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		st.MarkShutDown(at, sessionID)
		r.at = at
		return true
	})
	if !r.Recorded() {
		return
	}
	if err := session.DropSeen(r.root); err != nil && !errors.Is(err, session.ErrNoSession) {
		slog.Warn("session seen", "err", err, "path", session.SeenPath(r.root))
	}
}

// takeBack ends the record this shutdown wrote when its walk left a step not
// reached. A record written since by another shutdown is not this one's.
func (r *ShutdownRecord) takeBack() {
	r.c.Ledger.Update(func(st *state.State) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.at.IsZero() || r.reached >= r.steps || st.ShutDown == nil || !st.ShutDown.At.Equal(r.at) {
			return false
		}
		r.at = time.Time{}
		return st.ClearShutDown()
	})
}

// ClearShutDown ends the full shutdown on record: a restore started, or the
// start dialog started fresh (decisions.md D141).
func ClearShutDown(c *core.Core) {
	c.Ledger.Update(func(st *state.State) bool { return st.ClearShutDown() })
}
