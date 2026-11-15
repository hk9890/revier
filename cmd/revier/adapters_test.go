package main

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
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

// A config written for a release that had the herdr or the tmux runtime is
// told it was removed and what to name instead, not that it is an unknown
// name (decisions.md D175, D184).
func TestSelectRuntimeSaysARemovedRuntimeWasRemoved(t *testing.T) {
	for _, name := range []string{"herdr", "tmux"} {
		_, err := selectHost(t.Context(), "runtime", []string{name, "kitty"}, defaultRuntimeOrder, runtimeAdapters())
		if !errors.As(err, new(hostRemoved)) {
			t.Fatalf("%s: err = %v, want a removed runtime", name, err)
		}
		if want := "hosts.runtime: the " + name + " runtime was removed; use hc or kitty"; err.Error() != want {
			t.Errorf("err = %q, want %q", err, want)
		}
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
// Claude Code's: opencode's knows no state, so a probe written for the same
// harness knows more.
func TestAConfiguredProbeRunsBeforeOpencode(t *testing.T) {
	var names []string
	for _, p := range probes(&config.Config{Probes: []config.Probe{{Name: "codex", Exec: "codex-probe"}}}) {
		names = append(names, p.Name())
	}
	if got, want := strings.Join(names, " "), "claude codex opencode"; got != want {
		t.Errorf("probes = %q, want %q", got, want)
	}
}

// selectHost skips a default it cannot find without a word, so a name spelled
// one way in the order and another in the registry drops that adapter, and an
// adapter left out of the order is never tried, unless it is one a user opts
// into by name.
func TestEveryAdapterIsInItsDefaultOrder(t *testing.T) {
	check := func(kind string, registry, order []string) {
		if !slices.Equal(registry, slices.Sorted(slices.Values(order))) {
			t.Errorf("%s adapters %q, default order %q: want the same names", kind, registry, order)
		}
	}
	check("runtime", slices.Sorted(maps.Keys(runtimeAdapters())), slices.Concat(defaultRuntimeOrder, optInRuntimes))
	for _, name := range optInRuntimes {
		if slices.Contains(defaultRuntimeOrder, name) {
			t.Errorf("opt-in runtime %q is in the default order %q: selection would try it unnamed", name, defaultRuntimeOrder)
		}
	}
	check("window", slices.Sorted(maps.Keys(windowAdapters())), defaultWindowOrder)
}

// A runtime that draws its own instances is its own view, and with no list
// it is the one chosen: kitty shows itself as before. hc draws
// nothing, so with no list it is shown by kitty where kitty and a display
// are, and by no view where they are not, as over ssh (decisions.md D185).
func TestTheDefaultViewIsTheRuntimesOwn(t *testing.T) {
	kittyOnPath(t, false)
	for name, rt := range runtimeAdapters() {
		view, err := selectView(t.Context(), nil, rt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		own, isView := rt.(revier.View)
		switch {
		case isView && view != own:
			t.Errorf("%s: view %v, want the runtime itself", name, view)
		case !isView && view != nil:
			t.Errorf("%s with no display: view %v, want none", name, view)
		}
	}
	if view, err := selectView(t.Context(), nil, nil); view != nil || err != nil {
		t.Errorf("no runtime: view %v, %v; want none", view, err)
	}

	kittyOnPath(t, true)
	hcRT := runtimeAdapters()["hc"]
	view, err := selectView(t.Context(), nil, hcRT)
	if kv, ok := view.(*kitty.View); err != nil || !ok || kv.Runtime != hcRT.(kitty.Attachable) {
		t.Errorf("hc with kitty and a display: view %#v, %v; want the kitty view over hc", view, err)
	}
}

// A configured view is a preference order: "none" leaves the runtime unshown,
// and a view that cannot show the runtime is refused by name, as an unknown
// name is. With runtime "none" there is nothing to show, and the view is
// none, so every command still runs.
func TestAConfiguredViewIsTriedInOrder(t *testing.T) {
	adapters := runtimeAdapters()
	kittyRT, hcRT := adapters["kitty"], adapters["hc"]
	ctx := t.Context()
	if view, err := selectView(ctx, []string{hostNone}, kittyRT); view != nil || err != nil {
		t.Errorf("none: view %v, %v; want none", view, err)
	}
	if view, err := selectView(ctx, []string{"kitty"}, kittyRT); err != nil || view != kittyRT.(revier.View) {
		t.Errorf("kitty over kitty: view %v, %v; want kitty", view, err)
	}
	cfg := &config.Config{}
	cfg.Hosts.View = []string{"kitty"}
	if rt, view, err := selectTerminal(ctx, cfg, []string{hostNone}); rt != nil || view != nil || err != nil {
		t.Errorf("kitty over runtime none: %v, %v, %v; want no runtime and no view, and no error", rt, view, err)
	}
	if _, err := selectView(ctx, []string{"tmux"}, kittyRT); err == nil || err.Error() != "hosts.view: the tmux view was removed; use kitty" {
		t.Errorf("tmux: err = %v, want it named removed", err)
	}
	if _, err := selectView(ctx, []string{"ghostty"}, kittyRT); err == nil || err.Error() != `hosts.view: unknown host "ghostty"; known: kitty, none` {
		t.Errorf("ghostty: err = %v, want it named unknown", err)
	}

	kittyOnPath(t, false)
	if _, err := selectView(ctx, []string{"kitty"}, hcRT); err == nil || !strings.Contains(err.Error(), "no display") {
		t.Errorf("kitty over hc with no display: err = %v, want the kitty view refused for it", err)
	}
	if view, err := selectView(ctx, []string{"kitty", hostNone}, hcRT); view != nil || err != nil {
		t.Errorf("kitty, none over hc with no display: view %v, %v; want none", view, err)
	}
	kittyOnPath(t, true)
	if view, err := selectView(ctx, []string{"kitty"}, hcRT); err != nil || !is[*kitty.View](view) {
		t.Errorf("kitty over hc with a display: view %v, %v; want the kitty view", view, err)
	}
}

func is[T any](v any) bool {
	_, ok := v.(T)
	return ok
}
