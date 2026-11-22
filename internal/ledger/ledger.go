// Package ledger is the state file, the event file and the state record as
// the core reads and writes them: the one implementation of core.Ledger
// outside a test (decisions.md D120).
package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

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
	// Failed, when set, is told why a write did not reach the file.
	Failed func(error)
}

// popupHidden is the file whose presence is the mark of a raise of the
// minimized popup (core.PopupMarks). It is no part of state.json: it lives
// from one press to the focus the press gives, and state.json's save waits
// for the disk, milliseconds on the raise's path.
const popupHidden = "popup-hidden"

// MarkPopupHidden leaves the mark of a raise of the minimized popup.
// The root is there: the surface of the popup to raise writes to it.
func (f File) MarkPopupHidden() error {
	return os.WriteFile(filepath.Join(f.Root, popupHidden), nil, 0o644)
}

// TakePopupHidden reports whether the mark was there, and removes it: one
// call, so two focuses never both take one raise.
func (f File) TakePopupHidden() bool {
	return os.Remove(filepath.Join(f.Root, popupHidden)) == nil
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

// State is the state on disk, or the one read last. A read that fails writes
// "state load" with root and err, through logging.Repeat, and State goes on
// with the state it read last, or an empty one when it read none. A file
// that does not parse is read as an empty state, and its "state load" err
// says so. The bindings and a shutdown on record are then lost. The next
// write that changes the state replaces the file. The first read that
// succeeds after a failure writes "state load: recovered".
func (f File) State() *state.State {
	st, err := state.Load(f.Root)
	logging.Repeat("state load", "state load", err, "root", f.Root)
	if st == nil {
		return f.kept()
	}
	f.keep(st)
	return st
}

// pressLockPoll is how often HoldPress tries a lock another press holds.
const pressLockPoll = 20 * time.Millisecond

// HoldPress takes the project's press lock, a flock on one file per project
// under the root, so a crash releases it. It waits for another press to
// release it, and returns an error only when ctx ends first.
//
// A lock file that cannot be opened or locked must not cost the press, so
// the press runs unguarded. That writes "press lock" with root, project and
// err through logging.Repeat, and the first lock taken after it writes
// "press lock: recovered".
func (f File) HoldPress(ctx context.Context, p revier.ProjectName) (func(), error) {
	lock, err := openPressLock(f.Root, p)
	logging.Repeat("press lock", "press lock", err, "root", f.Root, "project", p)
	if err != nil {
		return func() {}, nil
	}
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				_ = lock.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = lock.Close()
			logging.Repeat("press lock", "press lock", err, "root", f.Root, "project", p)
			return func() {}, nil
		}
		select {
		case <-ctx.Done():
			_ = lock.Close()
			err := fmt.Errorf("%s: another press holds its lock: %w", p, ctx.Err())
			// Written when a press gave up waiting for the lock, so it ran
			// nothing and wrote no "go" line. It carries project and err.
			slog.Error("press lock: gave up waiting", "project", p, "err", err)
			return nil, err
		case <-time.After(pressLockPoll):
		}
	}
}

func openPressLock(root string, p revier.ProjectName) (*os.File, error) {
	dir := filepath.Join(root, "press-locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, string(p)+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
}

// Update applies a change to the state on disk and returns the state after
// it. apply reports whether it changed anything; it runs under the lock, so
// it does no I/O. A change that could not be started changed nothing, and
// the state after it is the one on disk: a caller that keeps what Update
// returns must not lose its bindings to a lock it could not take.
//
// A failed update writes "state update" with root and err, through
// logging.Repeat. A save that failed returns the changed state, which the
// root does not hold. The first update that succeeds after a failure writes
// "state update: recovered". A file that does not parse logs "state load",
// as State describes, and the change applies to an empty state.
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
	return st
}

// logShutDown says when a write set or ended the full shutdown on record.
// Every write goes through here, so one line covers the shutdown that sets
// it, a second one that sets it over the first, and each open that ends it.
//
// "shutdown state set" carries at and the session the shutdown saved, empty
// when it made no save; a full shutdown writes the record after its save
// (decisions.md D141). "shutdown state cleared" carries set_at, the at of the
// record that ended: something opened, a restore started, the start dialog
// started fresh, or a cancel or a second ctrl+c took the record back because
// its shutdown left a step not reached.
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
