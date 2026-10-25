package app_test

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
)

var seenAt = time.Date(2026, 10, 24, 1, 20, 0, 0, time.UTC)

// lastSeen writes what a surface last saw under root: demo's home and notes.
func lastSeen(t *testing.T, root string) session.Session {
	t.Helper()
	s := session.Session{At: seenAt, Current: "demo", Projects: []session.Project{{Name: "demo", Targets: []session.Target{
		{Name: "home", Agents: []session.Agent{{Harness: "claude", Session: "abc-123", Dir: "/src/demo"}}}, {Name: "notes"},
	}}}}
	if err := app.SaveSeen(root, s); err != nil {
		t.Fatal(err)
	}
	return s
}

// bare is a start after the end: nothing open, and the state given.
func bare(t *testing.T, st state.State) (*core.Core, core.Report) {
	t.Helper()
	_, _, _, projects := desktop(t)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Ledger: &ledger{st: st}}
	return c, surveyed(t, c, projects)
}

// An end with no shutdown that left nothing open makes what was last seen
// a saved session of its own moment, which a plain restore opens.
func TestAnEndWithNoShutdownPromotesWhatWasLastSeen(t *testing.T) {
	root := t.TempDir()
	seen := lastSeen(t, root)
	c, report := bare(t, state.State{})

	offer, ok, err := app.Recover(c, root, report)
	if err != nil || !ok {
		t.Fatalf("Recover = %v, %v, want an offer", ok, err)
	}
	if offer.Reason != session.ReasonLastSeen || !offer.At.Equal(seenAt) || !offer.SameAs(seen) {
		t.Errorf("offer = %+v, want what was last seen, saved as last-seen at %v", offer, seenAt)
	}
	got, err := session.Load(root, "")
	if err != nil || got.ID != offer.ID {
		t.Errorf("Load(\"\") = %q, %v, want the offer %q", got.ID, err, offer.ID)
	}
}

// After a full shutdown its own dialog offers its save (decisions.md D141),
// and what was last seen is left where it is.
func TestAShutdownOnRecordPromotesNothing(t *testing.T) {
	root := t.TempDir()
	lastSeen(t, root)
	var st state.State
	st.MarkShutDown(seenAt.Add(time.Minute), "")
	c, report := bare(t, st)

	if _, ok, err := app.Recover(c, root, report); ok || err != nil {
		t.Errorf("Recover = %v, %v, want nothing", ok, err)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want none written", all)
	}
}

// Any save made after what was last seen is newer word on what was open, a
// save by hand as well as a project's close, so nothing is promoted.
func TestAnySaveNewerThanTheSnapshotPromotesNothing(t *testing.T) {
	for _, reason := range []session.Reason{session.ReasonProjectClosed, session.ReasonSaved} {
		root := t.TempDir()
		lastSeen(t, root)
		if _, _, err := session.Save(root, session.Session{Reason: reason, At: seenAt.Add(time.Minute), Projects: []session.Project{{Name: "demo"}}}); err != nil {
			t.Fatal(err)
		}
		c, report := bare(t, state.State{})
		if _, ok, err := app.Recover(c, root, report); ok || err != nil {
			t.Errorf("%s: Recover = %v, %v, want nothing", reason, ok, err)
		}
		if all, _ := session.List(root); len(all) != 1 {
			t.Errorf("%s: sessions = %d, want the save alone", reason, len(all))
		}
	}
}

// When the save a plain restore opens holds the same, that save is offered,
// no copy of it is written, and what was last seen is taken.
func TestASaveThatHoldsTheSameIsOfferedAndTheSnapshotDropped(t *testing.T) {
	root := t.TempDir()
	seen := lastSeen(t, root)
	seen.Reason, seen.At = session.ReasonShutdown, seenAt.Add(-time.Hour)
	held, _, err := session.Save(root, seen)
	if err != nil {
		t.Fatal(err)
	}
	c, report := bare(t, state.State{})

	offer, ok, err := app.Recover(c, root, report)
	if err != nil || !ok || offer.ID != held.ID {
		t.Errorf("Recover = %q, %v, %v, want the save %q", offer.ID, ok, err, held.ID)
	}
	if all, _ := session.List(root); len(all) != 1 {
		t.Errorf("sessions = %d, want the one save", len(all))
	}
	if _, err := session.LoadSeen(root); err == nil {
		t.Error("what was last seen is still there, want it dropped")
	}
}

// The offer is made once: the second start finds what was last seen taken.
func TestTheOfferIsMadeOnce(t *testing.T) {
	root := t.TempDir()
	lastSeen(t, root)
	c, report := bare(t, state.State{})
	if _, ok, _ := app.Recover(c, root, report); !ok {
		t.Fatal("first Recover offered nothing")
	}
	if offer, ok, err := app.Recover(c, root, report); ok || err != nil {
		t.Errorf("second Recover = %+v, %v, %v, want nothing", offer, ok, err)
	}
}

// Every target still open is no loss, and a dry run's check writes nothing.
func TestAnEndThatLostNothingIsClean(t *testing.T) {
	root := t.TempDir()
	lastSeen(t, root)
	c, _, report, _ := desktop(t)
	if _, ok, err := app.Recover(c, root, report); ok || err != nil {
		t.Errorf("Recover = %v, %v, want nothing with every target open", ok, err)
	}

	c, report = bare(t, state.State{})
	if _, ok, err := app.Unclean(c, root, report); !ok || err != nil {
		t.Errorf("Unclean = %v, %v, want an end that lost something", ok, err)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %d, want Unclean to write none", len(all))
	}
}
