package app

import (
	"context"
	"errors"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
)

// Shutdown closes the plan as core.Shutdown does, for both surfaces. full is
// a shutdown of every project, all of each: it records in the state that
// revier was shut down, so the next surface offers to restore what it closed
// (decisions.md D141).
//
// The record is written once opts.Before has saved and before the first
// close, because the close may end the caller's own terminal last. A close
// the busy guard refuses, or whose save fails, records nothing; a close
// cancelled on its walk takes the record back, since what it did not reach is
// still open. A close that leaves some steps open keeps it.
func Shutdown(ctx context.Context, c *core.Core, stateRoot string, plan []core.CloseStep, wait time.Duration, opts core.ShutdownOpts, full bool) (core.Closed, error) {
	if !full {
		return c.Shutdown(ctx, plan, wait, opts)
	}
	marked := false
	save := opts.Before
	opts.Before = func(saving context.Context, now core.Report) error {
		id := ""
		if save != nil {
			if err := save(saving, now); err != nil {
				return err
			}
			// The save wrote the newest session, or found the newest already
			// held what is open.
			if newest, err := session.Load(stateRoot, ""); err == nil {
				id = newest.ID
			}
		}
		at := time.Now()
		c.Ledger.Update(func(st *state.State) bool {
			st.MarkShutDown(at, id)
			return true
		})
		marked = true
		return nil
	}
	closed, err := c.Shutdown(ctx, plan, wait, opts)
	if marked && errors.Is(ctx.Err(), context.Canceled) {
		c.Ledger.Update(func(st *state.State) bool { return st.ClearShutDown() })
	}
	return closed, err
}
