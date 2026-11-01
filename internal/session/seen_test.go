// Layer L1: what a surface last saw is one file beside the stored sessions.
package session_test

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/hk9890/revier/internal/session"
)

// What a surface last saw is no stored session: neither the list nor a plain
// restore takes it, and it reads back whole.
func TestTheSeenSessionIsNoStoredSession(t *testing.T) {
	root := t.TempDir()
	seen := sample(12, "")
	if err := session.SaveSeen(root, seen); err != nil {
		t.Fatalf("SaveSeen: %v", err)
	}
	if all, err := session.List(root); err != nil || len(all) != 0 {
		t.Errorf("List = %+v, %v, want no stored session", all, err)
	}
	if _, err := session.Load(root, ""); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("Load(\"\") err = %v, want ErrNoSession", err)
	}
	got, err := session.LoadSeen(root)
	if err != nil {
		t.Fatalf("LoadSeen: %v", err)
	}
	if !got.SameAs(seen) || !got.At.Equal(seen.At) || got.Current != seen.Current {
		t.Errorf("LoadSeen = %+v, want %+v", got, seen)
	}
	if got.ID != "" || got.Reason != session.ReasonLastSeen {
		t.Errorf("id, reason = %q, %q, want none and last-seen", got.ID, got.Reason)
	}
}

// A rename made while no surface runs reaches what was last seen, so the
// session it becomes opens the project under its new name.
func TestARenameReachesTheSeenSession(t *testing.T) {
	root := t.TempDir()
	if err := session.SaveSeen(root, sample(12, "")); err != nil {
		t.Fatal(err)
	}
	if err := session.Rename(root, "revier", "rv"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, err := session.LoadSeen(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "rv" || got.Projects[0].Name != "rv" {
		t.Errorf("seen = %+v, want rv", got)
	}
}

// What was last seen becomes a stored session once: a second promotion, as
// of a second surface that starts at the same moment, finds nothing.
func TestPromoteMovesTheSeenFileOnce(t *testing.T) {
	root := t.TempDir()
	if err := session.SaveSeen(root, sample(12, "")); err != nil {
		t.Fatal(err)
	}
	stored, _, err := session.PromoteSeen(root)
	if err != nil {
		t.Fatalf("PromoteSeen: %v", err)
	}
	if stored.ID != "2026-09-12T14-33-05" || stored.Reason != session.ReasonLastSeen {
		t.Errorf("stored = %q %q, want the id of its moment and last-seen", stored.ID, stored.Reason)
	}
	if _, _, err := session.PromoteSeen(root); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("second PromoteSeen err = %v, want ErrNoSession", err)
	}
	if err := session.DropSeen(root); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("DropSeen after it err = %v, want ErrNoSession", err)
	}
}

// A promotion that runs beside a save of the same second keeps both: the
// move takes a free name as the save does, and renames over no session.
func TestPromoteBesideASaveOfItsSecondKeepsBoth(t *testing.T) {
	for round := range 50 {
		root := t.TempDir()
		if err := session.SaveSeen(root, sample(12, "seen")); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, _, err := session.PromoteSeen(root); err != nil {
				t.Errorf("PromoteSeen: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, _, err := session.Save(root, sample(12, "saved")); err != nil {
				t.Errorf("Save: %v", err)
			}
		}()
		wg.Wait()
		all, err := session.List(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("round %d: List = %d sessions, want the promoted one and the saved one", round, len(all))
		}
		entries, _ := os.ReadDir(session.Dir(root))
		if len(entries) != 2 {
			t.Fatalf("round %d: session directory holds %d entries, want the 2 sessions alone", round, len(entries))
		}
	}
}

// A promoted snapshot is what a plain restore opens when it is the newest
// save of the whole set, and it is listed under the id of its file.
func TestAPromotedLastSeenIsWhatARestoreOpens(t *testing.T) {
	root := t.TempDir()
	mustSave(t, root, sample(11, "older"))
	if err := session.SaveSeen(root, sample(12, "")); err != nil {
		t.Fatal(err)
	}
	stored, _, err := session.PromoteSeen(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := session.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != stored.ID || got.Reason != session.ReasonLastSeen {
		t.Errorf("Load(\"\") = %q %q, want the promoted %q", got.ID, got.Reason, stored.ID)
	}
}
