package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
)

// Shutdown closes the plan as core.Shutdown does, for both surfaces. save,
// when set, is opts.Before: it saves the session the shutdown ends and returns
// the session that holds what is open, which is zero when it recorded
// nothing. record is set for a full shutdown, of every project, all of each:
// it records in the state that revier was shut down, and the session save
// returned, so the next surface offers to restore what it closed
// (decisions.md D141).
//
// The record is written once the save is made and before the first close,
// because the close may end the caller's own terminal last. A close the busy
// guard refuses, or whose save fails, records nothing, and neither does one
// cancelled before the record is written: the cancel stops it before its
// first step. A close cancelled on its walk takes the record back as
// ShutdownRecord says.
func Shutdown(ctx context.Context, c *core.Core, plan []core.CloseStep, wait time.Duration, opts core.ShutdownOpts,
	save func(context.Context, core.Report) (session.Session, error), record *ShutdownRecord,
) (core.Closed, error) {
	if record != nil {
		record.start(len(plan))
		progress := opts.Progress
		opts.Progress = func(p core.Progress) {
			if p.Phase == core.PhaseClose && !p.Done {
				record.reach()
			}
			if progress != nil {
				progress(p)
			}
		}
	}
	if save != nil || record != nil {
		opts.Before = func(saving context.Context, now core.Report) error {
			var stored session.Session
			if save != nil {
				var err error
				if stored, err = save(saving, now); err != nil {
					return err
				}
			}
			// A cancel that came during the save leaves the save made and
			// writes no record. The walk reads the cancel before its first
			// step, so nothing closes.
			if record != nil {
				record.write(ctx, stored.ID)
			}
			return nil
		}
	}
	closed, err := c.Shutdown(ctx, plan, wait, opts)
	if record != nil && errors.Is(ctx.Err(), context.Canceled) {
		record.takeBack()
	}
	return closed, err
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
	mu      sync.Mutex
	at      time.Time // when this shutdown wrote its record; zero when it holds none
	steps   int
	reached int
}

// NewShutdownRecord is the record of a full shutdown about to run.
func NewShutdownRecord(c *core.Core) *ShutdownRecord { return &ShutdownRecord{c: c} }

// Abandon is the end of a cancelled shutdown whose surface quits before the
// walk returns, on the second ctrl+c: the record is taken back as the walk
// would have taken it back on its cancel. The first ctrl+c cancelled the
// walk, so the walk writes no record after this.
func (r *ShutdownRecord) Abandon() { r.takeBack() }

// Recorded reports whether the shutdown is on record: written by it, and not
// taken back.
func (r *ShutdownRecord) Recorded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.at.IsZero()
}

func (r *ShutdownRecord) start(steps int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = steps
}

func (r *ShutdownRecord) reach() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reached++
}

// write records the shutdown unless ctx was cancelled. The cancel is read
// under the state's lock, so a surface that takes the record back after its
// cancel is never followed by this write.
//
// The ledger does not report a write that failed, so the record is read
// back: one the state does not hold is not this shutdown's, and the surface
// that quits on a recorded shutdown stays, since its next start would offer
// nothing. A record an open cleared since is not held either.
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
	if sd := r.c.Ledger.State().ShutDown; sd != nil && sd.At.Equal(at) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.at.Equal(at) {
		r.at = time.Time{}
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
