package state_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

func TestRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := &state.State{Current: "revier"}
	s.Attach("revier", revier.TargetRef{Host: "gnome", ID: "7", Title: "Pull requests"})
	if err := s.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := state.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Current != "revier" {
		t.Errorf("Current = %q", got.Current)
	}
	if len(got.Attached["revier"]) != 1 || got.Attached["revier"][0].ID != "7" {
		t.Errorf("Attached = %+v", got.Attached)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s, err := state.Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Current != "" || s.Attached == nil {
		t.Errorf("state = %+v, want empty with a usable map", s)
	}
}

// Corrupt state holds only a convenience, so it is discarded rather than
// preventing revier from starting.
func TestLoadCorruptIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(root)
	if err != nil {
		t.Fatalf("Load should not fail on corrupt state: %v", err)
	}
	if s.Current != "" {
		t.Errorf("state = %+v", s)
	}
}

func TestAttachIsIdempotent(t *testing.T) {
	s := &state.State{}
	ref := revier.TargetRef{Host: "gnome", ID: "7"}
	s.Attach("p", ref)
	s.Attach("p", ref)
	if len(s.Attached["p"]) != 1 {
		t.Errorf("got %d attachments, want 1", len(s.Attached["p"]))
	}
}

// Window ids do not survive an application restart, so a stale attachment must
// be dropped rather than pointing at whatever now holds that id.
func TestPruneDropsDeadRefs(t *testing.T) {
	s := &state.State{}
	live := revier.TargetRef{Host: "gnome", ID: "7"}
	dead := revier.TargetRef{Host: "gnome", ID: "9"}
	s.Attach("p", live)
	s.Attach("p", dead)
	s.Attach("q", dead)

	s.Prune(map[string]bool{state.Key(live): true})

	if len(s.Attached["p"]) != 1 || s.Attached["p"][0].ID != "7" {
		t.Errorf("p = %+v, want only the live ref", s.Attached["p"])
	}
	if _, ok := s.Attached["q"]; ok {
		t.Error("q had only dead refs and should have been removed")
	}
}

func TestRootHonoursOverride(t *testing.T) {
	t.Setenv("REVIER_STATE_HOME", "/tmp/scratch-state")
	got, err := state.Root()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/scratch-state" {
		t.Errorf("Root = %q", got)
	}
}

// The launch record crosses processes: written by revier go, read by the TUI.
func TestLaunchRoundTrip(t *testing.T) {
	root := t.TempDir()
	at := time.Now().Truncate(time.Second)
	s := &state.State{Launch: &state.Launch{Project: "revier", At: at}}
	if err := s.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Launch == nil || got.Launch.Project != "revier" || !got.Launch.At.Equal(at) {
		t.Errorf("launch = %+v", got.Launch)
	}
}
