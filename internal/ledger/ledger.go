// Package ledger is the state file and the event file as the core reads and
// writes them: the one implementation of core.Ledger outside a test
// (decisions.md D120).
package ledger

import (
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// File is the ledger under one state root. Every read is of the file as it
// is now, and every write is made under its lock, because another process
// writes launches to it - a desktop key while the surface runs - and a press
// must find them.
//
// State is a convenience: losing it costs a binding, not the operation. A
// file that cannot be read is an empty state, and a failure is logged once
// for each cause, since a surface reads the file every refresh.
type File struct {
	Root string
	// Written, when set, is told of every write, so a surface can take the
	// state into itself before its next survey does.
	Written chan<- struct{}
	// Failed, when set, is told why a write did not reach the file.
	Failed func(error)
}

// State is the state on disk, or an empty one.
func (f File) State() *state.State {
	st, err := state.Load(f.Root)
	logging.Repeat("state load", "state load", err, "root", f.Root)
	if st == nil {
		return &state.State{}
	}
	return st
}

// Update applies a change to the state on disk and returns the state after
// it. apply reports whether it changed anything; it runs under the lock, so
// it does no I/O.
func (f File) Update(apply func(*state.State) bool) *state.State {
	st, err := state.Update(f.Root, apply)
	logging.Repeat("state update", "state update", err, "root", f.Root)
	if err != nil && f.Failed != nil {
		f.Failed(err)
	}
	if st == nil {
		return &state.State{}
	}
	if f.Written != nil {
		// A write still waiting to be read already stands for this one: the
		// reader takes the whole state again.
		select {
		case f.Written <- struct{}{}:
		default:
		}
	}
	return st
}

// Record appends one event to the file events.Setup named.
func (File) Record(e revier.Event) { events.Record(e) }
