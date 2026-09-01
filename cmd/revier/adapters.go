package main

import (
	"context"
	"fmt"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/adapter/gnome"
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
		"tmux": &tmux.Host{},
	}
}

func windowAdapters() map[string]revier.WindowController {
	return map[string]revier.WindowController{
		"gnome": &gnome.Host{},
	}
}

func probes() []revier.AgentProbe {
	return []revier.AgentProbe{&claude.Probe{}}
}

// preference order used when config names none.
var (
	defaultRuntimeOrder = []string{"tmux"}
	defaultWindowOrder  = []string{"gnome"}
)

// hostNone disables a host class. Listing it is how a machine that HAS a usable
// adapter runs without one anyway - a test that must not touch the desktop, or
// a user who wants revier to leave their windows alone. An empty list cannot
// express this: it means "search the defaults".
const hostNone = "none"

// selectHosts picks the adapters for this machine.
//
// A host named in config that fails Probe is a hard error: the user asked for
// it by name and silently using another would hide a broken setup. An
// unnamed preference list is a search, so a failure there just moves on.
func selectHosts(ctx context.Context, cfg *config.Config) (revier.Runtime, revier.WindowController, error) {
	rt, err := selectRuntime(ctx, cfg.Hosts.Runtime)
	if err != nil {
		return nil, nil, err
	}
	win, err := selectWindow(ctx, cfg.Hosts.Window)
	if err != nil {
		return nil, nil, err
	}
	return rt, win, nil
}

func selectRuntime(ctx context.Context, want []string) (revier.Runtime, error) {
	adapters := runtimeAdapters()
	if len(want) > 0 {
		for _, name := range want {
			if name == hostNone {
				return nil, nil
			}
			h, ok := adapters[name]
			if !ok {
				return nil, fmt.Errorf("unknown runtime host %q", name)
			}
			if err := h.Probe(ctx); err != nil {
				return nil, fmt.Errorf("runtime host %q is configured but unusable: %w", name, err)
			}
			return h, nil
		}
	}
	for _, name := range defaultRuntimeOrder {
		if h := adapters[name]; h != nil && h.Probe(ctx) == nil {
			return h, nil
		}
	}
	// No runtime is survivable: window-only targets still work.
	return nil, nil
}

func selectWindow(ctx context.Context, want []string) (revier.WindowController, error) {
	adapters := windowAdapters()
	if len(want) > 0 {
		for _, name := range want {
			if name == hostNone {
				return nil, nil
			}
			h, ok := adapters[name]
			if !ok {
				return nil, fmt.Errorf("unknown window host %q", name)
			}
			if err := h.Probe(ctx); err != nil {
				return nil, fmt.Errorf("window host %q is configured but unusable: %w", name, err)
			}
			return h, nil
		}
	}
	for _, name := range defaultWindowOrder {
		if h := adapters[name]; h != nil && h.Probe(ctx) == nil {
			return h, nil
		}
	}
	// No window host is survivable: window-only targets report unavailable.
	return nil, nil
}

func newCore(rt revier.Runtime, win revier.WindowController) *core.Core {
	return &core.Core{Runtime: rt, Window: win, Probes: probes()}
}
