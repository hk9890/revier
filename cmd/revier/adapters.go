package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/adapter/execprobe"
	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/adapter/kitty"
	"github.com/hk9890/revier/internal/adapter/opencode"
	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/internal/adapter/ssh"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/popupswitch"
	"github.com/hk9890/revier/pkg/revier"
)

// The compiled-in adapters. This list is the whole registry: adding one is a
// line here and its name in defaultRuntimeOrder or optInRuntimes, not an
// init() somewhere, so what a build contains is readable in one place.
func runtimeAdapters() map[string]revier.Runtime {
	// Without its own path, a view's window over hc is not held once its
	// program ends: nothing worse.
	self, _ := os.Executable()
	return map[string]revier.Runtime{
		"kitty": &kitty.Host{},
		"hc":    &hc.Host{Revier: self},
	}
}

// popupListenOn is the control socket of the kitty the popup runs in.
var popupListenOn = kitty.ListenOn

func windowAdapters() map[string]revier.WindowController {
	return map[string]revier.WindowController{
		"gnome": &gnome.Host{},
	}
}

// probes lists the Claude probe, then the ones config declares as
// subprocesses, then opencode's, which knows less than a probe a user wrote:
// it knows no state.
func probes(cfg *config.Config) []revier.AgentProbe {
	out := []revier.AgentProbe{&claude.Probe{}}
	for _, pr := range cfg.Probes {
		out = append(out, execprobe.New(pr.Name, pr.Exec))
	}
	return append(out, &opencode.Probe{})
}

// preference order used when config names none.
var (
	defaultRuntimeOrder = []string{"kitty"}
	defaultWindowOrder  = []string{"gnome"}
)

// optInRuntimes are registered and left out of defaultRuntimeOrder, which
// TestEveryAdapterIsInItsDefaultOrder checks: selection tries a runtime
// outside the default order only when config names it. hc is a prototype, and
// must not take over a machine that has it installed.
var optInRuntimes = []string{"hc"}

// hostRemoved answers a config that still names a runtime or a view an
// earlier release had, so that its user learns what to name instead of
// "unknown host".
type hostRemoved struct{ kind, name, use string }

func (e hostRemoved) Error() string {
	return fmt.Sprintf("the %s %s was removed; use %s", e.name, e.kind, e.use)
}

// removedHosts are the runtimes and the views an earlier release had, by
// kind. Keeping herdr beside hc meant a second host and probe to track for
// the same makers' product. Its interface may come back as a view over hc.
var removedHosts = map[string][]string{"runtime": {"herdr", "tmux"}, "view": {"tmux"}}

// unknownHost is the error for a name in [hosts] that none of the adapters
// carries; it names theirs, and hostNone, as the names to use.
func unknownHost(kind, name string, adapters []string) error {
	if slices.Contains(removedHosts[kind], name) {
		return hostRemoved{kind: kind, name: name, use: orList(adapters)}
	}
	return fmt.Errorf("unknown host %q; known: %s", name, strings.Join(slices.Concat(adapters, []string{hostNone}), ", "))
}

// orList joins names as "a, b or c".
func orList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

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
func selectHosts(ctx context.Context, cfg *config.Config) (revier.Runtime, revier.View, revier.WindowController, error) {
	rt, view, err := selectTerminal(ctx, cfg, cfg.Hosts.Runtime)
	if err != nil {
		return nil, nil, nil, err
	}
	win, err := selectHost(ctx, "window", cfg.Hosts.Window, defaultWindowOrder, windowAdapters())
	if err != nil {
		return nil, nil, nil, err
	}
	return rt, view, win, nil
}

// selectTerminal picks the runtime of the want list, or of the defaults when
// it is empty, and the view [hosts] names for it.
func selectTerminal(ctx context.Context, cfg *config.Config, want []string) (revier.Runtime, revier.View, error) {
	rt, err := selectHost(ctx, "runtime", want, defaultRuntimeOrder, runtimeAdapters())
	if err != nil {
		return nil, nil, err
	}
	view, err := selectView(ctx, cfg.Hosts.View, rt)
	if err != nil {
		return nil, nil, err
	}
	return rt, view, nil
}

// views are the compiled-in views. kitty is the one: it shows the kitty
// runtime as that runtime, and hc in windows that attach its terminals.
var views = []string{"kitty"}

// viewOf is the view called name that shows rt. A variable, so a live test
// shows a real runtime in a view that opens no window.
var viewOf = showWith

// showWith is the view called name that shows rt, or an error when it
// cannot: a runtime that draws itself is its own view, which was probed as
// the runtime, and kitty shows a runtime whose panels a window attaches, once
// it probes usable.
func showWith(ctx context.Context, name string, rt revier.Runtime) (revier.View, error) {
	if view, ok := rt.(revier.View); ok && rt.Name() == name {
		return view, nil
	}
	attachable, ok := rt.(kitty.Attachable)
	if name != "kitty" || !ok {
		return nil, fmt.Errorf("%s: cannot show the %s runtime", name, rt.Name())
	}
	view := &kitty.View{Runtime: attachable}
	if err := view.Probe(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return view, nil
}

// selectView picks the view that shows rt. A configured list is a preference
// order, as selectHost reads one, and a view it names that cannot show rt is
// an error; among the defaults such a view is passed over, and finding none
// leaves the runtime unshown: hc with no kitty, or no display, as over ssh.
// With no runtime there is nothing to show, and no view is selected, whatever
// the list names.
func selectView(ctx context.Context, want []string, rt revier.Runtime) (revier.View, error) {
	if rt == nil {
		return nil, nil
	}
	if len(want) == 0 {
		for _, name := range views {
			if view, err := viewOf(ctx, name, rt); err == nil {
				return view, nil
			}
		}
		return nil, nil
	}
	var errs []error
	for _, name := range want {
		switch {
		case name == hostNone:
			return nil, nil
		case !slices.Contains(views, name):
			return nil, fmt.Errorf("hosts.view: %w", unknownHost("view", name, views))
		}
		view, err := viewOf(ctx, name, rt)
		if err == nil {
			return view, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("no configured view is usable: %w", errors.Join(errs...))
}

// hostProblems is every name in [hosts] that no compiled-in adapter carries,
// in any position of its list. selectHost stops at the first name that
// works, so a name after it is never checked there, and an unknown name it
// does reach stops every command but doctor; doctor reads these instead.
func hostProblems(cfg *config.Config) []error {
	var errs []error
	check := func(kind string, want, adapters []string) {
		for _, name := range want {
			if name != hostNone && !slices.Contains(adapters, name) {
				errs = append(errs, fmt.Errorf("hosts.%s: %w", kind, unknownHost(kind, name, adapters)))
			}
		}
	}
	check("runtime", cfg.Hosts.Runtime, slices.Sorted(maps.Keys(runtimeAdapters())))
	check("view", cfg.Hosts.View, views)
	check("window", cfg.Hosts.Window, slices.Sorted(maps.Keys(windowAdapters())))
	return errs
}

// probed is an adapter that selectHost can try: a runtime or a window host.
type probed interface {
	Name() string
	Probe(ctx context.Context) error
}

// selectHost picks one host of a kind, "runtime" or "window". Finding none
// among the defaults is survivable: without a runtime the window-only targets
// still work, and without a window host they report unavailable.
func selectHost[H probed](ctx context.Context, kind string, want, defaults []string, adapters map[string]H) (H, error) {
	var none H
	if len(want) > 0 {
		var errs []error
		for _, name := range want {
			if name == hostNone {
				return none, nil
			}
			h, ok := adapters[name]
			if !ok {
				return none, fmt.Errorf("hosts.%s: %w", kind, unknownHost(kind, name, slices.Sorted(maps.Keys(adapters))))
			}
			if err := h.Probe(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			return h, nil
		}
		return none, fmt.Errorf("no configured %s host is usable: %w", kind, errors.Join(errs...))
	}
	for _, name := range defaults {
		if h, ok := adapters[name]; ok && h.Probe(ctx) == nil {
			return h, nil
		}
	}
	return none, nil
}

// keyBinders are the desktops whose keyboard shortcuts revier can read. This
// is a shorter list than windowAdapters on purpose: reading a compositor's
// window list and reading a desktop's shortcut store are different jobs.
func keyBinders() map[string]revier.KeyBinder {
	return map[string]revier.KeyBinder{"gnome": &gnome.Keys{}}
}

// keyWriters are the desktops whose shortcuts revier can change. A writer is a
// separate type from the reader in the same adapter, so that the reader stays
// read-only and `revier setup status` cannot reach a write through it.
func keyWriters() map[string]revier.KeyBinder {
	return map[string]revier.KeyBinder{"gnome": &gnome.Writer{}}
}

// selectKeyBinder picks the desktop to read shortcuts from. Finding none is
// survivable: every other command works, and `revier setup` says so.
func selectKeyBinder(ctx context.Context, binders map[string]revier.KeyBinder) revier.KeyBinder {
	for _, name := range defaultWindowOrder {
		b, ok := binders[name].(interface {
			revier.KeyBinder
			Probe(context.Context) error
		})
		if ok && b.Probe(ctx) == nil {
			return b
		}
	}
	return nil
}

// selectKeyWriter is the same choice, refusing a desktop that can only be
// read: a command that changes keys must not silently do nothing.
func selectKeyWriter(ctx context.Context, binders map[string]revier.KeyBinder) revier.KeyBinder {
	b := selectKeyBinder(ctx, binders)
	if _, ok := b.(revier.KeyWriter); !ok {
		return nil
	}
	return b
}

// termKeys is the terminal whose keys revier writes, and termKeyWriter the
// same terminal behind the type that writes. kitty is the one there is, and
// its configuration directory is there to read on any machine, so nothing is
// probed. As with the desktop's keys, `revier setup status` is given only the
// reader.
func termKeys() revier.TermKeys { return kitty.TermKeys{} }

func termKeyWriter() revier.TermKeyWriter { return kitty.TermKeyWriter{} }

// sshRemote is the one remote adapter: the revier on a host, reached over
// ssh. A host is not probed: whether it answers is learned when it is asked,
// and reported on the projects that live there rather than refusing to start.
func sshRemote(host string) revier.Remote { return ssh.New(host) }

// popupRules reads the window host's rules where its extension reads the
// rules revier writes for the popup, so that a launch leaves the popup's
// place to them. Elsewhere it is nil, and the launch
// places the popup.
func popupRules(win revier.WindowController) revier.WindowRules {
	h, ok := win.(*gnome.Host)
	if !ok {
		return nil
	}
	rules := h.Rules()
	if rules.CanAdd() != nil {
		return nil
	}
	return rules
}

func newCore(cfg *config.Config, rt revier.Runtime, view revier.View, win revier.WindowController, keys revier.KeyBinder) *core.Core {
	return &core.Core{Runtime: rt, View: view, Window: win, Served: servedProcesses(), Self: proc.RunsUnder(), Run: runTargetCommand, SwitchPopup: switchPopup, Probes: probes(cfg), KeyBinder: keys, NewRemote: sshRemote}
}

// switchPopup tells the popup's surface to switch its list, over the socket
// it listens on.
func switchPopup(ctx context.Context) error {
	path, err := popupswitch.Path()
	if err != nil {
		return err
	}
	return popupswitch.Switch(ctx, path)
}

// servedProcesses lists what `revier agent exec` started here for a terminal
// on another machine (decisions/0084). It is not selected: it holds what no
// runtime does, beside whichever runtime was. It needs a /proc, and a machine
// without one serves panels that no survey sees.
func servedProcesses() revier.Host {
	h := &proc.Host{}
	if h.Probe(context.Background()) != nil {
		return nil
	}
	return h
}
