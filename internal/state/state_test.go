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

func TestRenameMovesEverythingRecordedUnderTheProject(t *testing.T) {
	s := &state.State{}
	ref := revier.TargetRef{Host: "gnome", ID: "7"}
	s.Attach("old", ref)
	s.Attach("other", ref)
	s.Landed("old", "editor", ref)
	s.Launched("old", "home", time.Now())

	s.Rename("old", "new")

	if s.Current != "new" || s.Launch.Project != "new" {
		t.Errorf("current %q, launch %q, want new", s.Current, s.Launch.Project)
	}
	if _, ok := s.Attached["old"]; ok || len(s.Attached["new"]) != 1 || len(s.Attached["other"]) != 1 {
		t.Errorf("attached = %v, want old's moved to new and other's kept", s.Attached)
	}
	if _, ok := s.Bound["old"]; ok || s.Bound["new"]["editor"] != ref {
		t.Errorf("bound = %v, want old's moved to new", s.Bound)
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

	s.Prune([]string{"gnome"}, []revier.Instance{{Ref: live}}, s)

	if len(s.Attached["p"]) != 1 || s.Attached["p"][0].ID != "7" {
		t.Errorf("p = %+v, want only the live ref", s.Attached["p"])
	}
	if _, ok := s.Attached["q"]; ok {
		t.Error("q had only dead refs and should have been removed")
	}
}

// A survey that did not list a host says nothing about its windows. A TUI
// started where the window host does not probe must not throw away every
// attachment and binding made on it.
func TestPruneKeepsRefsOnHostsItDidNotAsk(t *testing.T) {
	s := &state.State{}
	window := revier.TargetRef{Host: "gnome", ID: "7"}
	s.Attach("p", window)
	s.Bind("p", "editor", window)

	if s.Prune([]string{"tmux"}, nil, s) {
		t.Error("Prune reported a change for refs on a host it did not ask")
	}
	if len(s.Attached["p"]) != 1 || s.Bound["p"]["editor"] != window {
		t.Errorf("state = %+v, want the gnome refs kept", s)
	}
}

// A survey lists what was there when it started. A binding written while it
// was listing may be to a window that opened after the listing, and dropping
// it would cost the target the window it just landed on.
func TestPruneKeepsRefsWrittenAfterTheSurveyStarted(t *testing.T) {
	before := &state.State{}
	old := revier.TargetRef{Host: "gnome", ID: "7"}
	before.Bind("p", "home", old)

	now := &state.State{}
	fresh := revier.TargetRef{Host: "gnome", ID: "9"}
	now.Bind("p", "home", old)
	now.Bind("p", "editor", fresh)
	now.Attach("p", fresh)

	now.Prune([]string{"gnome"}, nil, before)

	if _, ok := now.Bound["p"]["home"]; ok {
		t.Error("home's window was there to be listed and was not; want its binding dropped")
	}
	if now.Bound["p"]["editor"] != fresh || len(now.Attached["p"]) != 1 {
		t.Errorf("state = %+v, want the refs written after the survey started kept", now)
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
