package main

import (
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

func TestSelectRuntimeFallsThroughThePreferenceList(t *testing.T) {
	broken := hosttest.NewRuntime("broken")
	broken.ProbeErr = errors.New("not here")
	working := hosttest.NewRuntime("working")
	adapters := map[string]revier.Runtime{"broken": broken, "working": working}

	got, err := selectHost(context.Background(), "runtime", []string{"broken", "working"}, defaultRuntimeOrder, adapters)
	if err != nil {
		t.Fatalf("selectHost: %v", err)
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

	_, err := selectHost(context.Background(), "runtime", []string{"a", "b"}, defaultRuntimeOrder, adapters)
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
	_, err := selectHost(context.Background(), "runtime", []string{"nosuch"}, defaultRuntimeOrder, map[string]revier.Runtime{})
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("err = %v, want it to name the unknown host", err)
	}
}

// "none" disables a host class even on a machine where an adapter would work.
// It is how a test, or a user who wants their windows left alone, opts out.
func TestHostNoneDisablesTheClass(t *testing.T) {
	working := hosttest.New("gnome")
	adapters := map[string]revier.WindowController{"gnome": working}

	got, err := selectHost(context.Background(), "window", []string{hostNone, "gnome"}, defaultWindowOrder, adapters)
	if err != nil {
		t.Fatalf("selectHost: %v", err)
	}
	if got != nil {
		t.Fatalf("selected %v, want none", got)
	}
}

// No configured list and nothing usable is survivable: window-only targets
// report unavailable rather than the command failing.
func TestSelectWindowWithNoDefaultsAvailable(t *testing.T) {
	got, err := selectHost(context.Background(), "window", nil, defaultWindowOrder, map[string]revier.WindowController{})
	if err != nil {
		t.Fatalf("selectHost: %v", err)
	}
	if got != nil {
		t.Fatalf("selected %v, want nil", got)
	}
}

// A probe the configuration declares runs before every compiled-in probe but
// Claude Code's: herdr's takes herdr's word and opencode's knows no state, so
// a probe written for the same harness knows more.
func TestAConfiguredProbeRunsBeforeHerdrAndOpencode(t *testing.T) {
	var names []string
	for _, p := range probes(&config.Config{Probes: []config.Probe{{Name: "codex", Exec: "codex-probe"}}}) {
		names = append(names, p.Name())
	}
	if got, want := strings.Join(names, " "), "claude codex herdr opencode"; got != want {
		t.Errorf("probes = %q, want %q", got, want)
	}
}

// A key pressed in a tmux pane reaches tmux, also where kitty comes first and
// would probe as usable: kitty holds no tmux pane, and the key failed.
func TestATmuxPaneNarrowsTheRuntimeToTmux(t *testing.T) {
	cases := []struct {
		order, want []string
	}{
		{defaultRuntimeOrder, []string{"tmux"}},
		{[]string{"kitty", "tmux"}, []string{"tmux"}},
		{[]string{"kitty"}, []string{"kitty"}},
		{[]string{"kitty", hostNone, "tmux"}, []string{"kitty", hostNone, "tmux"}},
		{[]string{hostNone}, []string{hostNone}},
		{nil, nil},
	}
	for _, c := range cases {
		if got := atTmuxPane(c.order); !slices.Equal(got, c.want) {
			t.Errorf("atTmuxPane(%q) = %q, want %q", c.order, got, c.want)
		}
	}
}

// The two commands a tmux key runs try tmux alone, and a kitty window id
// still tries the list in order. With no tool on the PATH no host probes as
// usable, so the error names every host the command tried.
func TestATmuxKeysCommandsTryTmuxAlone(t *testing.T) {
	root := configRoot(t, "[hosts]\nruntime = [\"kitty\", \"tmux\"]\nwindow = [\"none\"]\n", nil)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
	testenv.NoTools(t)

	for _, args := range [][]string{
		{"agent", "new", "--panel", "%3"},
		{"shell", "new", "--panel", "%3"},
	} {
		err := run(io.Discard, args)
		if err == nil || !strings.Contains(err.Error(), "tmux:") || strings.Contains(err.Error(), "kitty") {
			t.Errorf("revier %s: err = %v, want tmux alone tried", strings.Join(args, " "), err)
		}
	}
	err := run(io.Discard, []string{"agent", "new", "--panel", "3"})
	if err == nil || !strings.Contains(err.Error(), "kitty:") || !strings.Contains(err.Error(), "tmux:") {
		t.Errorf("revier agent new --panel 3: err = %v, want kitty and tmux tried", err)
	}
}

// selectHost skips a default it cannot find without a word, so a name spelled
// one way in the order and another in the registry drops that adapter, and an
// adapter left out of the order is never tried.
func TestEveryAdapterIsInItsDefaultOrder(t *testing.T) {
	check := func(kind string, registry, order []string) {
		if !slices.Equal(registry, slices.Sorted(slices.Values(order))) {
			t.Errorf("%s adapters %q, default order %q: want the same names", kind, registry, order)
		}
	}
	check("runtime", slices.Sorted(maps.Keys(runtimeAdapters())), defaultRuntimeOrder)
	check("window", slices.Sorted(maps.Keys(windowAdapters())), defaultWindowOrder)
}
