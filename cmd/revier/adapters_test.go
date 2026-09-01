package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

func TestSelectRuntimeFallsThroughThePreferenceList(t *testing.T) {
	broken := hosttest.NewRuntime("broken")
	broken.ProbeErr = errors.New("not here")
	working := hosttest.NewRuntime("working")
	adapters := map[string]revier.Runtime{"broken": broken, "working": working}

	got, err := selectRuntime(context.Background(), []string{"broken", "working"}, adapters)
	if err != nil {
		t.Fatalf("selectRuntime: %v", err)
	}
	if got == nil || got.Name() != "working" {
		t.Fatalf("selected %v, want the second entry: a list is a preference order", got)
	}
}

// When every named host fails, the error names them all. Silently running with
// no host would hide a broken setup the user described by name.
func TestSelectRuntimeReportsEveryFailure(t *testing.T) {
	a := hosttest.NewRuntime("a")
	a.ProbeErr = errors.New("a is down")
	b := hosttest.NewRuntime("b")
	b.ProbeErr = errors.New("b is down")
	adapters := map[string]revier.Runtime{"a": a, "b": b}

	_, err := selectRuntime(context.Background(), []string{"a", "b"}, adapters)
	if err == nil {
		t.Fatal("want an error when no configured host is usable")
	}
	for _, want := range []string{"a is down", "b is down"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestSelectRuntimeRejectsAnUnknownName(t *testing.T) {
	_, err := selectRuntime(context.Background(), []string{"nosuch"}, map[string]revier.Runtime{})
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("err = %v, want it to name the unknown host", err)
	}
}

// "none" disables a host class even on a machine where an adapter would work.
// It is how a test, or a user who wants their windows left alone, opts out.
func TestHostNoneDisablesTheClass(t *testing.T) {
	working := hosttest.New("gnome")
	adapters := map[string]revier.WindowController{"gnome": working}

	got, err := selectWindow(context.Background(), []string{hostNone, "gnome"}, adapters)
	if err != nil {
		t.Fatalf("selectWindow: %v", err)
	}
	if got != nil {
		t.Fatalf("selected %v, want none", got)
	}
}

// No configured list and nothing usable is survivable: window-only targets
// report unavailable rather than the command failing.
func TestSelectWindowWithNoDefaultsAvailable(t *testing.T) {
	got, err := selectWindow(context.Background(), nil, map[string]revier.WindowController{})
	if err != nil {
		t.Fatalf("selectWindow: %v", err)
	}
	if got != nil {
		t.Fatalf("selected %v, want nil", got)
	}
}
