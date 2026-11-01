package state_test

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

var chrome = revier.TargetRef{Host: "gnome", ID: "42"}

// A run that moved focus is kept for the next press, and makes its project
// current. It is no open: the launch and the shutdown on record stay.
func TestRanCommandKeepsTheRunAndLeavesTheLaunchAndTheShutdown(t *testing.T) {
	s := &state.State{}
	s.Launched("other", "editor", time.Now())
	s.MarkShutDown(time.Now(), "s1")

	s.RanCommand("p", "web", chrome)

	want := state.LastRun{Project: "p", Target: "web", Focused: chrome}
	if s.LastRun == nil || *s.LastRun != want {
		t.Errorf("last run = %+v, want %+v", s.LastRun, want)
	}
	if s.Current != "p" {
		t.Errorf("current = %q, want p", s.Current)
	}
	if s.Launch == nil || s.Launch.Project != "other" {
		t.Errorf("launch = %+v, want other's kept", s.Launch)
	}
	if s.ShutDown == nil {
		t.Error("shutdown on record cleared, want it kept")
	}
}

// A run that left focus where it was records no run, and ends the one
// before it.
func TestRanCommandWithNoFocusClearsTheRun(t *testing.T) {
	s := &state.State{}
	s.RanCommand("a", "web", chrome)

	s.RanCommand("b", "web", revier.TargetRef{})

	if s.LastRun != nil {
		t.Errorf("last run = %+v, want none", s.LastRun)
	}
	if s.Current != "b" {
		t.Errorf("current = %q, want b", s.Current)
	}
}

// Every other press clears the run, so the next press of the command target
// runs it.
func TestEveryOtherPressClearsTheRun(t *testing.T) {
	presses := map[string]func(*state.State){
		"launch":  func(s *state.State) { s.Launched("p", "editor", time.Now()) },
		"landing": func(s *state.State) { s.Landed("p", "home", revier.TargetRef{Host: "rt", ID: "1"}) },
		"claim": func(s *state.State) {
			s.Launch = &state.Launch{Project: "p", Target: "editor", At: time.Now()}
			s.Claim("editor", revier.TargetRef{Host: "gnome", ID: "7"})
		},
	}
	for name, press := range presses {
		s := &state.State{}
		s.RanCommand("p", "web", chrome)
		press(s)
		if s.LastRun != nil {
			t.Errorf("%s: last run = %+v, want none", name, s.LastRun)
		}
	}
}

func TestRenameMovesTheRun(t *testing.T) {
	s := &state.State{}
	s.RanCommand("old", "web", chrome)

	s.Rename("old", "new")

	if s.LastRun == nil || s.LastRun.Project != "new" {
		t.Errorf("last run = %+v, want it under new", s.LastRun)
	}
}

func TestLastRunRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := &state.State{}
	s.RanCommand("p", "web", chrome)
	if err := s.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := state.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastRun == nil || *got.LastRun != *s.LastRun {
		t.Errorf("last run = %+v, want %+v", got.LastRun, s.LastRun)
	}
}
