package state_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// Writers that update state at once each keep their change. Every Update opens
// the lock on its own, as a separate process does.
func TestUpdateLosesNoConcurrentChange(t *testing.T) {
	root := t.TempDir()
	const writers = 40
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			_, err := state.Update(root, func(s *state.State) bool {
				s.Attach("demo", revier.TargetRef{Host: "wm", ID: strconv.Itoa(i)})
				return true
			})
			if err != nil {
				t.Errorf("Update: %v", err)
			}
		})
	}
	wg.Wait()

	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Attached["demo"]); n != writers {
		t.Errorf("attached %d, want %d: a concurrent update was overwritten", n, writers)
	}
}

// An update that changes nothing writes nothing.
func TestUpdateSavesOnlyAChange(t *testing.T) {
	root := t.TempDir()
	if _, err := state.Update(root, func(s *state.State) bool {
		s.Current = "demo"
		return false
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "" {
		t.Errorf("current = %q, want the empty state: nothing was saved", got.Current)
	}
}

func TestPendingIsTheTargetsLaunchInsideTheWindow(t *testing.T) {
	l := &state.Launch{Project: "demo", Target: "editor", At: time.Now().Add(-5 * time.Second)}
	if !l.Pending("demo", "editor", time.Minute) {
		t.Error("a launch of the target inside the window is pending")
	}
	if l.Pending("demo", "home", time.Minute) || l.Pending("other", "editor", time.Minute) {
		t.Error("a launch of another target is not this target's")
	}
	if l.Pending("demo", "editor", time.Second) {
		t.Error("a launch older than the window has expired")
	}
	var none *state.Launch
	if none.Pending("demo", "editor", time.Minute) {
		t.Error("no launch on record is nothing pending")
	}
}

// A target that lands consumes its own launch, and only its own.
func TestLandedConsumesOnlyItsOwnLaunch(t *testing.T) {
	ref := revier.TargetRef{Host: "wm", ID: "7"}
	s := &state.State{}
	s.Launched("demo", "editor", time.Now())
	s.Landed("demo", "home", revier.TargetRef{Host: "rt", ID: "1"})
	if s.Launch == nil {
		t.Fatal("home landing consumed the editor's launch")
	}
	s.Landed("demo", "editor", ref)
	if s.Launch != nil || s.Bound["demo"]["editor"] != ref || s.Current != "demo" {
		t.Errorf("state = %+v, want the editor bound, its launch consumed and demo current", s)
	}
}

// A claim binds a target's launch and attaches an action's.
func TestClaimBindsATargetAndAttachesAnAction(t *testing.T) {
	ref := revier.TargetRef{Host: "wm", ID: "7"}
	s := &state.State{}
	s.Launched("demo", "editor", time.Now())
	s.Claim("editor", ref)
	if s.Launch != nil || s.Bound["demo"]["editor"] != ref {
		t.Errorf("state = %+v, want the editor bound and the launch consumed", s)
	}
	s.Launched("demo", "", time.Now())
	s.Claim("", ref)
	if s.Launch != nil || len(s.Attached["demo"]) != 1 {
		t.Errorf("state = %+v, want the window attached and the launch consumed", s)
	}
}
