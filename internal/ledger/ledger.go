// Package ledger is the state file, the event file and the state record as
// the core reads and writes them: the one implementation of core.Ledger
// outside a test (decisions.md D120).
package ledger

import (
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// File is the ledger under one state root. Every read is of the file as it
// is now, and every write is made under its lock, because another process
// writes launches to it - a desktop key while the surface runs - and a press
// must find them.
//
// Losing the state costs bindings and a shutdown on record, not the
// operation. A file that cannot be read is the state this process read from
// it last, and an empty state before the first read: a surface reads the file
// every refresh, and one failed read must not take its bindings and attached
// windows off the screen. A file that does not parse is discarded: it is an
// empty state, and the next write replaces it. A failure is logged once for
// each cause, a discarded file as a failure to load.
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
	changed := false
	var before *state.ShutDown
	var was *state.State
	st, err := state.Update(f.Root, func(st *state.State) bool {
		before, was = st.ShutDown, st.Clone()
		changed = apply(st)
		return changed
	})
	if errors.Is(err, state.ErrCorrupt) {
		logging.Repeat("state load", "state load", err, "root", f.Root)
		err = nil
	}
	logging.Repeat("state update", "state update", err, "root", f.Root)
	if err != nil && f.Failed != nil {
		f.Failed(err)
	}
	if st == nil {
		return f.State()
	}
	// Only a state that was saved is what the root holds: what apply did to
	// one it reported as unchanged is not in the file.
	if changed && err == nil {
		f.keep(st)
		logShutDown(before, st.ShutDown)
		record.Write(changes(was, st)...)
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

// logShutDown says when a write set or ended the full shutdown on record.
// Every write goes through here, so one line covers the shutdown that sets
// it, a second one that sets it over the first, and each open that ends it.
func logShutDown(before, after *state.ShutDown) {
	switch {
	case after != nil && after != before:
		slog.Info("shutdown state set", "at", after.At, "session", after.Session)
	case before != nil && after == nil:
		slog.Info("shutdown state cleared", "set_at", before.At)
	}
}

// changes is what a write did, as lines of the state record (decisions.md
// D151): the project it selected, the run of a command target it recorded,
// each target it bound or unbound, and each instance it attached or
// detached. Every write goes through here, so one place covers a landing, a
// claim, a run, an attachment, a rename and a prune, in every process that
// writes the state.
func changes(was, is *state.State) []record.Line {
	var out []record.Line
	if is.Current != was.Current {
		out = append(out, record.Line{Kind: record.KindProjectSelected, Project: is.Current, From: string(was.Current)})
	}
	if run := is.LastRun; run != nil && (was.LastRun == nil || *run != *was.LastRun) {
		out = append(out, record.Line{Kind: record.KindRan, Project: run.Project, Target: run.Target, Host: run.Focused.Host, Ref: run.Focused.ID})
	}
	for _, p := range sortedKeys(was.Bound, is.Bound) {
		for _, t := range sortedKeys(was.Bound[p], is.Bound[p]) {
			from, bound := was.Bound[p][t]
			to, binds := is.Bound[p][t]
			moved := !sameRef(from, to)
			if bound && (!binds || moved) {
				out = append(out, record.Line{Kind: record.KindUnbound, Project: p, Target: t, Host: from.Host, Ref: from.ID})
			}
			if binds && (!bound || moved) {
				out = append(out, record.Line{Kind: record.KindBound, Project: p, Target: t, Host: to.Host, Ref: to.ID})
			}
		}
	}
	for _, p := range sortedKeys(was.Attached, is.Attached) {
		for _, ref := range was.Attached[p] {
			if !slices.ContainsFunc(is.Attached[p], func(o revier.TargetRef) bool { return sameRef(o, ref) }) {
				out = append(out, record.Line{Kind: record.KindDetached, Project: p, Host: ref.Host, Ref: ref.ID})
			}
		}
		for _, ref := range is.Attached[p] {
			if !slices.ContainsFunc(was.Attached[p], func(o revier.TargetRef) bool { return sameRef(o, ref) }) {
				out = append(out, record.Line{Kind: record.KindAttached, Project: p, Host: ref.Host, Ref: ref.ID})
			}
		}
	}
	for i := range out {
		out[i].By = record.ByDid
	}
	return out
}

// sameRef reports two refs to one instance: a title it had then is no other
// instance.
func sameRef(a, b revier.TargetRef) bool { return a.Host == b.Host && a.ID == b.ID }

func sortedKeys[K ~string, V any](a, b map[K]V) []K {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// Record appends one event to the file events.Setup named.
func (File) Record(e revier.Event) { events.Record(e) }

// Note appends one line to the state record record.Setup named, as one an
// operation did when it says nothing else.
func (File) Note(l record.Line) {
	if l.By == "" {
		l.By = record.ByDid
	}
	record.Write(l)
}
