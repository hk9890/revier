package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/adapter/execprobe"
	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/internal/adapter/opencode"
	"github.com/hk9890/revier/internal/adapter/sway"
	"github.com/hk9890/revier/internal/adapter/tmux"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The compiled-in adapters. This list is the whole registry: adding one is a
// line here, not an init() somewhere, so what a build contains is readable in
// one place.
func runtimeAdapters() map[string]revier.Runtime {
	return map[string]revier.Runtime{
		"kitty": &kitty.Host{},
		"tmux":  &tmux.Host{},
	}
}

func windowAdapters() map[string]revier.WindowController {
	return map[string]revier.WindowController{
		"gnome": &gnome.Host{},
		"sway":  &sway.Host{},
	}
}

// probes lists the compiled-in probes first, then the ones config declares
// as subprocesses, so a compiled probe wins for the harness it knows.
func probes(cfg *config.Config) []revier.AgentProbe {
	out := []revier.AgentProbe{&claude.Probe{}, &opencode.Probe{}}
	for _, pr := range cfg.Probes {
		out = append(out, execprobe.New(pr.Name, pr.Exec))
	}
	return out
}

// preference order used when config names none.
var (
	defaultRuntimeOrder = []string{"kitty", "tmux"}
	defaultWindowOrder  = []string{"gnome", "sway"}
)

// hostNone disables a host class. Listing it is how a machine that HAS a usable
// adapter runs without one anyway - a test that must not touch the desktop, or
// a user who wants revier to leave their windows alone. An empty list cannot
// express this: it means "search the defaults".
const hostNone = "none"

// selectHosts picks the adapters for this machine.
//
// A configured list is a preference order: each name is tried until one probes
// successfully. If none does, that is a hard error naming all of them - the
// user asked for these by name, and silently falling back to an adapter they
// did not list would hide a broken setup. With no list configured the defaults
// are searched and finding nothing is survivable.
func selectHosts(ctx context.Context, cfg *config.Config) (revier.Runtime, revier.WindowController, error) {
	rt, err := selectRuntime(ctx, cfg.Hosts.Runtime, runtimeAdapters())
	if err != nil {
		return nil, nil, err
	}
	win, err := selectWindow(ctx, cfg.Hosts.Window, windowAdapters())
	if err != nil {
		return nil, nil, err
	}
	return rt, win, nil
}

func selectRuntime(ctx context.Context, want []string, adapters map[string]revier.Runtime) (revier.Runtime, error) {
	if len(want) > 0 {
		var errs []error
		for _, name := range want {
			if name == hostNone {
				return nil, nil
			}
			h, ok := adapters[name]
			if !ok {
				return nil, fmt.Errorf("unknown runtime host %q", name)
			}
			if err := h.Probe(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			return h, nil
		}
		return nil, fmt.Errorf("no configured runtime host is usable: %w", errors.Join(errs...))
	}
	for _, name := range defaultRuntimeOrder {
		if h := adapters[name]; h != nil && h.Probe(ctx) == nil {
			return h, nil
		}
	}
	// No runtime is survivable: window-only targets still work.
	return nil, nil
}

func selectWindow(ctx context.Context, want []string, adapters map[string]revier.WindowController) (revier.WindowController, error) {
	if len(want) > 0 {
		var errs []error
		for _, name := range want {
			if name == hostNone {
				return nil, nil
			}
			h, ok := adapters[name]
			if !ok {
				return nil, fmt.Errorf("unknown window host %q", name)
			}
			if err := h.Probe(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			return h, nil
		}
		return nil, fmt.Errorf("no configured window host is usable: %w", errors.Join(errs...))
	}
	for _, name := range defaultWindowOrder {
		if h := adapters[name]; h != nil && h.Probe(ctx) == nil {
			return h, nil
		}
	}
	// No window host is survivable: window-only targets report unavailable.
	return nil, nil
}

func newCore(cfg *config.Config, rt revier.Runtime, win revier.WindowController) *core.Core {
	return &core.Core{Runtime: rt, Window: win, Probes: probes(cfg)}
}
