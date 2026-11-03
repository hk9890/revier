package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/state"
)

// `revier shutdown` says when it starts to wait for what it closed to go,
// and how many it waits for, and once everything went, that the shutdown is
// complete and it is safe to leave.
func TestAShutdownSaysWhenItWaitsAndWhenItIsComplete(t *testing.T) {
	a, _ := demoDesktop(t, &state.State{}, "home", "diff")

	out := output(t, a, func() error { return cmdShutdown(context.Background(), a, nil) })
	wait := strings.Index(out, "waiting for 2 to close\n")
	if wait < 0 || wait > strings.Index(out, "closed 2, 0 still open") {
		t.Errorf("output = %q, want the wait for both steps said before the result", out)
	}
	if !strings.HasSuffix(out, "shutdown complete; safe to leave\n") {
		t.Errorf("output = %q, want the shutdown said complete last", out)
	}
}

// A target still open after the wait, as an editor that asks about unsaved
// work is, fails `revier shutdown`: `revier shutdown && poweroff` must not
// power off under it.
func TestAShutdownThatLeavesATargetOpenFails(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home")
	rt.Refuses = map[string]bool{rt.Add("diff", "").ID: true}

	var err error
	out := output(t, a, func() error { err = cmdShutdown(context.Background(), a, nil); return nil })
	if !errors.Is(err, errShutdownIncomplete) {
		t.Errorf("err = %v, want the shutdown incomplete", err)
	}
	if !strings.Contains(out, "closed 1, 1 still open") || strings.Contains(out, "safe to leave") {
		t.Errorf("output = %q, want the target still open and no safe to leave said", out)
	}
}

// A step that did not close keeps `revier shutdown` from saying it is
// complete.
func TestAShutdownWithAStepThatFailedIsNotComplete(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home", "diff")
	rt.CloseErr = errors.New("tmux refused")

	var err error
	out := output(t, a, func() error { err = cmdShutdown(context.Background(), a, nil); return nil })
	if err == nil || !strings.Contains(err.Error(), "did not close") {
		t.Errorf("err = %v, want the steps that did not close", err)
	}
	if strings.Contains(out, "complete") || strings.Contains(out, "waiting for") {
		t.Errorf("output = %q, want no complete said, and no wait for nothing", out)
	}
}
