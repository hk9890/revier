// Package ledger is the state file and the event file as the core reads and
// writes them: the one implementation of core.Ledger outside a test
// (decisions.md D120).
package ledger

import (
	"sync"

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
// file that cannot be read is the state this process read from it last, and
// an empty state before the first read: a surface reads the file every
// refresh, and one failed read must not take its bindings and attached
// windows off the screen. A failure is logged once for each cause.
type File struct {
	Root string
	// Written, when set, is told of every write, so a surface can take the
	// state into itself before its next survey does.
	Written chan<- struct{}
	// Failed, when set, is told why a write did not reach the file.
	Failed func(error)
}

// read is the state each root held when this process last read or wrote it.
var read = struct {
	sync.Mutex
	last map[string]*state.State
}{last: map[string]*state.State{}}

// keep records st as the state of the root, in a copy: the caller changes
// its own.
func (f File) keep(st *state.State) {
	read.Lock()
	defer read.Unlock()
	read.last[f.Root] = st.Clone()
}

// kept is the state of the root as last recorded, or an empty one.
func (f File) kept() *state.State {
	read.Lock()
	defer read.Unlock()
	if st := read.last[f.Root]; st != nil {
		return st.Clone()
	}
	return &state.State{}
}

// State is the state on disk, or the one read last.
func (f File) State() *state.State {
	st, err := state.Load(f.Root)
	logging.Repeat("state load", "state load", err, "root", f.Root)
	if st == nil {
		return f.kept()
	}
	f.keep(st)
	return st
}

// Update applies a change to the state on disk and returns the state after
// it. apply reports whether it changed anything; it runs under the lock, so
// it does no I/O. A change that could not be started changed nothing, and
// the state after it is the one on disk: a caller that keeps what Update
// returns must not lose its bindings to a lock it could not take.
func (f File) Update(apply func(*state.State) bool) *state.State {
	st, err := state.Update(f.Root, apply)
	logging.Repeat("state update", "state update", err, "root", f.Root)
	if err != nil && f.Failed != nil {
		f.Failed(err)
	}
	if st == nil {
		return f.State()
	}
	if err == nil {
		f.keep(st)
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
