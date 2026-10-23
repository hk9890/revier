package app

import (
	"context"
	"errors"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
)

// Shutdown closes the plan as core.Shutdown does, for both surfaces. save,
// when set, is opts.Before: it saves the session the shutdown ends and returns
// the session that holds what is open, which is zero when it recorded
// nothing. full is a shutdown of every project, all of each: it records in the
// state that revier was shut down, and the session save returned, so the next
// surface offers to restore what it closed (decisions.md D141).
//
// The record is written once the save is made and before the first close,
// because the close may end the caller's own terminal last. A close the busy
// guard refuses, or whose save fails, records nothing, and neither does one
// cancelled before the record is written: the cancel stops it before its
// first step. A close cancelled on its walk takes the record back, since what
// it did not reach is still open; a cancel that came after the last step was
// walked reached nothing, and the record stays. A close that leaves some
// steps open keeps it.
func Shutdown(ctx context.Context, c *core.Core, plan []core.CloseStep, wait time.Duration, opts core.ShutdownOpts,
	save func(context.Context, core.Report) (session.Session, error), full bool,
) (core.Closed, error) {
	marked := false
	if save != nil || full {
		opts.Before = func(saving context.Context, now core.Report) error {
			var stored session.Session
			if save != nil {
				var err error
				if stored, err = save(saving, now); err != nil {
					return err
				}
			}
			if full {
				// The cancel is read under the state's lock, so a surface that
				// takes the record back after its cancel - on the second
				// ctrl+c, as it quits - is never followed by this write.
				at := time.Now()
				c.Ledger.Update(func(st *state.State) bool {
					if marked = ctx.Err() == nil; marked {
						st.MarkShutDown(at, stored.ID)
					}
					return marked
				})
				if !marked {
					return ctx.Err()
				}
			}
			return nil
		}
	}
	closed, err := c.Shutdown(ctx, plan, wait, opts)
	if marked && errors.Is(ctx.Err(), context.Canceled) && len(closed) < len(plan) {
		ClearShutDown(c)
	}
	return closed, err
}

// ClearShutDown ends the full shutdown on record: its close was cancelled, a
// restore started, or the start dialog started fresh (decisions.md D141).
func ClearShutDown(c *core.Core) {
	c.Ledger.Update(func(st *state.State) bool { return st.ClearShutDown() })
}
