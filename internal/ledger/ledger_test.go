package ledger_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// The ledger reads and writes the state on disk: a launch written through it
// is pending to the next read, a landing is bound, and every write is told.
func TestFileReadsAndWritesTheStateFile(t *testing.T) {
	root := t.TempDir()
	written := make(chan struct{}, 1)
	l := ledger.File{Root: root, Written: written}

	if st := l.State(); st.Pending("work", "home", time.Now()) || len(st.Bound) != 0 {
		t.Fatal("a fresh root has nothing pending and nothing bound")
	}
	after := l.Update(func(st *state.State) bool {
		st.Launched("work", "home", time.Now())
		return true
	})
	if !after.Pending("work", "home", time.Now()) || !l.State().Pending("work", "home", time.Now()) {
		t.Error("the launch just written is not pending, in what Update returned or on disk")
	}
	select {
	case <-written:
	default:
		t.Error("the write was not told")
	}
	ref := revier.TargetRef{Host: "rt", ID: "1"}
	l.Update(func(st *state.State) bool {
		st.Landed("work", "home", ref)
		return true
	})
	// Another ledger on the same root is another process: it reads the same.
	st := ledger.File{Root: root}.State()
	if st.Bound["work"]["home"] != ref || st.Launch != nil || st.Current != "work" {
		t.Errorf("state = %+v; want the landing bound, the launch consumed and the project current", st)
	}
}

// A change apply reports as none is not saved.
func TestFileSavesNothingForAnUpdateThatChangedNothing(t *testing.T) {
	root := t.TempDir()
	ledger.File{Root: root}.Update(func(st *state.State) bool {
		st.Current = "work"
		return false
	})
	if st := (ledger.File{Root: root}).State(); st.Current != "" {
		t.Errorf("current = %q, want nothing saved", st.Current)
	}
}

// State is a convenience: a root that cannot be written costs the write, says
// so, and still hands back a state to go on with.
func TestFileSaysWhenAWriteDidNotReachTheFile(t *testing.T) {
	// The state root is a file, so no state directory can be made under it.
	root := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var failed error
	l := ledger.File{Root: root, Failed: func(err error) { failed = err }}

	st := l.Update(func(st *state.State) bool {
		st.Current = "work"
		return true
	})
	if failed == nil {
		t.Error("the failed write was not told")
	}
	if st == nil || l.State() == nil {
		t.Error("a ledger that cannot be read or written must still hand back a state")
	}
}

// A write that cannot take the lock changed nothing, so what it hands back is
// the state on disk: a surface keeps what Update returns, and must not lose
// its bindings to a write that failed.
func TestFileHandsBackTheStateOnDiskWhenAWriteCouldNotStart(t *testing.T) {
	root := t.TempDir()
	ref := revier.TargetRef{Host: "rt", ID: "1"}
	ledger.File{Root: root}.Update(func(st *state.State) bool {
		st.Landed("work", "home", ref)
		return true
	})
	// The lock is a directory, so it cannot be opened to be locked.
	lock := filepath.Join(root, "state.lock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	var failed error
	l := ledger.File{Root: root, Failed: func(err error) { failed = err }}

	st := l.Update(func(st *state.State) bool {
		st.Current = "other"
		return true
	})
	if failed == nil {
		t.Fatal("the failed write was not told")
	}
	if st.Bound["work"]["home"] != ref || st.Current != "work" {
		t.Errorf("state = %+v, want the one on disk", st)
	}
}

// An event recorded through the ledger is in the event file of the process.
func TestFileRecordsAnEvent(t *testing.T) {
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })

	ledger.File{Root: root}.Record(revier.Event{Kind: revier.EventGo, Project: "work", Target: "home"})

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventGo || got[0].Project != "work" {
		t.Errorf("events = %+v, want the one go", got)
	}
}
