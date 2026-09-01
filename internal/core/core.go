// Package core holds everything that is not tool-specific: matching instances
// against realizations, choosing which realization wins, run-or-raise, and
// toggle-back. Adapters implement five methods and hold no policy, so this
// package is where behaviour is pinned and where most tests point.
package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

var (
	// ErrNoHost is returned when no configured host can realize a target.
	// It is a normal outcome, not a failure: a window-only target on a machine
	// with no WindowController reports this rather than erroring at the
	// keystroke.
	ErrNoHost = errors.New("no host can realize this target")

	// ErrNoTarget is returned for a name the project does not declare.
	ErrNoTarget = errors.New("no such target")

	// ErrUnboundedMatch rejects a realization whose match constrains nothing.
	// Such a match would select the first instance the host happens to list.
	ErrUnboundedMatch = errors.New("realization match constrains nothing")
)

// Core orchestrates hosts and probes. Runtime may be nil on a machine with no
// usable terminal host, and Window may be nil on one with no window control;
// targets degrade individually rather than the product failing as a whole.
type Core struct {
	Runtime revier.Runtime
	Window  revier.WindowController
	Probes  []revier.AgentProbe
}

// hosts returns the configured hosts, window first so the default resolution
// rule and the focus question both read it first.
func (c *Core) hosts() []revier.Host {
	var out []revier.Host
	if c.Window != nil {
		out = append(out, c.Window)
	}
	if c.Runtime != nil {
		out = append(out, c.Runtime)
	}
	return out
}

// Resolve reports which host and realization serve a target. It returns
// ErrNoHost when the target declares no realization for any configured host.
func (c *Core) Resolve(t revier.Target) (revier.Host, revier.Realization, error) {
	host, real, _, err := c.resolve(t)
	return host, real, err
}

// resolve is Resolve plus which side of the host split answered, so the caller
// can pick the compiled match that belongs to the winning realization.
func (c *Core) resolve(t revier.Target) (revier.Host, revier.Realization, revier.HostKind, error) {
	window, hasWindow := revier.Host(nil), false
	if c.Window != nil && t.Window != nil {
		window, hasWindow = c.Window, true
	}
	runtime, hasRuntime := revier.Host(nil), false
	if c.Runtime != nil && t.Runtime != nil {
		runtime, hasRuntime = c.Runtime, true
	}

	switch {
	case t.Prefer == revier.HostRuntime && hasRuntime:
		return runtime, *t.Runtime, revier.HostRuntime, nil
	case t.Prefer == revier.HostWindow && hasWindow:
		return window, *t.Window, revier.HostWindow, nil
	case hasWindow:
		// The default rule. A separate window is what a desktop user expects;
		// `prefer = "runtime"` on the target overrides it.
		return window, *t.Window, revier.HostWindow, nil
	case hasRuntime:
		return runtime, *t.Runtime, revier.HostRuntime, nil
	}
	return nil, revier.Realization{}, "", ErrNoHost
}

// resolveAt resolves the i-th target of a prepared project, returning the
// compiled match of the realization that won.
func (c *Core) resolveAt(p Project, i int) (revier.Host, revier.Realization, revier.CompiledMatch, error) {
	host, real, kind, err := c.resolve(p.Targets[i])
	if err != nil {
		return nil, revier.Realization{}, revier.CompiledMatch{}, err
	}
	m := p.compiled[i].runtime
	if kind == revier.HostWindow {
		m = p.compiled[i].window
	}
	return host, real, m, nil
}

// snapshot is one bulk listing per host, taken once and matched against every
// project locally. This is why a refresh costs one call per host rather than
// one per project.
type snapshot map[string][]revier.Instance

func (c *Core) snapshot(ctx context.Context) (snapshot, error) {
	s := make(snapshot)
	for _, h := range c.hosts() {
		in, err := h.Instances(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: instances: %w", h.Name(), err)
		}
		s[h.Name()] = in
	}
	return s, nil
}

// find returns the first instance of the host that satisfies the match. The
// match was compiled at load, so this is a scan and nothing else.
func find(s snapshot, h revier.Host, m revier.CompiledMatch) (revier.Instance, bool) {
	for _, i := range s[h.Name()] {
		if m.Matches(i) {
			return i, true
		}
	}
	return revier.Instance{}, false
}

// Go runs-or-raises a target. Pressing the same key twice returns to the
// project's home target, which is what makes a binding a round trip rather than
// a one-way jump.
func (c *Core) Go(ctx context.Context, p Project, name revier.TargetName) (revier.TargetRef, error) {
	i, ok := p.index(name)
	if !ok {
		return revier.TargetRef{}, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	t := p.Targets[i]
	host, real, m, err := c.resolveAt(p, i)
	if err != nil {
		return revier.TargetRef{}, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return revier.TargetRef{}, err
	}

	inst, found := find(snap, host, m)
	if !found {
		ref, err := host.Open(ctx, real)
		if err != nil {
			return revier.TargetRef{}, fmt.Errorf("%s: open %s: %w", host.Name(), name, err)
		}
		// Focus explicitly. Some hosts focus what they launch and some do not,
		// so without this the raise half of run-or-raise holds only by
		// accident of the host - the window opens behind on the ones that do
		// not. Go always leaves the target focused.
		if err := host.Focus(ctx, ref); err != nil {
			return revier.TargetRef{}, fmt.Errorf("%s: focus new %s: %w", host.Name(), name, err)
		}
		return ref, nil
	}

	// Toggle back: the target is already where focus is, so the second press
	// returns home instead of doing nothing.
	if !t.Home && c.focusedOn(ctx, inst.Ref) {
		if home, ok := p.Home(); ok {
			return c.Go(ctx, p, home.Name)
		}
	}
	if err := host.Focus(ctx, inst.Ref); err != nil {
		return revier.TargetRef{}, fmt.Errorf("%s: focus %s: %w", host.Name(), name, err)
	}
	return inst.Ref, nil
}

// focusAuthority is the host whose Focused answer describes where the user
// actually is. OS focus is global, so a window host outranks a runtime, which
// knows only which of its own panes is current.
func (c *Core) focusAuthority() revier.Host {
	if c.Window != nil {
		return c.Window
	}
	if c.Runtime != nil {
		return c.Runtime
	}
	return nil
}

// focusedOn reports whether ref is certainly where the user is standing.
//
// Certainty matters more than coverage here, because the consequence of a
// false positive is a keypress that goes home when the user asked to go
// somewhere: strictly worse than a keypress that does nothing surprising.
//
// So a ref counts only when its own host is the focus authority. Ids are
// host-scoped - a GNOME window id and a tmux pane id are unrelated numbers -
// so comparing across hosts is meaningless, and comparing a runtime's "current
// pane" against global OS focus is wrong in the common case: a runtime target
// can be tmux's current window while the user is looking at the editor.
//
// The cost is that toggle-back does not fire for runtime targets on a machine
// that has a window host. Recovering it needs a mapping from a runtime
// instance to the OS window containing it, which the ports do not carry yet.
func (c *Core) focusedOn(ctx context.Context, ref revier.TargetRef) bool {
	auth := c.focusAuthority()
	if auth == nil || ref.ID == "" || ref.Host != auth.Name() {
		return false
	}
	cur, err := auth.Focused(ctx)
	if err != nil {
		return false
	}
	return cur.Host == ref.Host && cur.ID == ref.ID
}

// Survey builds the view every renderer reads: one bulk listing per host, then
// local matching for every project.
//
// Every project here is already rendered and compiled; a project that could
// not be was refused at load, so the survey has no per-project error path and
// does no work that a previous refresh did not also have to do.
func (c *Core) Survey(ctx context.Context, projects []Project) ([]revier.ProjectView, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]revier.ProjectView, 0, len(projects))
	for _, p := range projects {
		views = append(views, c.view(ctx, snap, p))
	}
	return views, nil
}

func (c *Core) view(ctx context.Context, snap snapshot, p Project) revier.ProjectView {
	v := revier.ProjectView{Project: p.Project}

	// Probe every matched instance, not only home. An agent is wherever the
	// user put it - a pane of the workspace, or a target of its own - and a
	// dashboard that only looked at home would miss exactly the agent that had
	// been given its own window.
	seen := map[string]bool{}

	for i, t := range p.Targets {
		tv := revier.TargetView{Name: t.Name, Key: t.Key}
		host, _, m, err := c.resolveAt(p, i)
		if err == nil {
			tv.Available = true
			tv.Host = host.Name()
			if inst, found := find(snap, host, m); found {
				tv.Ref = inst.Ref
				if t.Home {
					v.Running, v.Home = true, inst.Ref
				}
				// One instance can back two targets; probing it twice would
				// report the same agent twice.
				key := inst.Ref.Host + "\x00" + inst.Ref.ID
				if !seen[key] {
					seen[key] = true
					v.Agents = append(v.Agents, c.inspect(ctx, inst)...)
				}
			}
		}
		v.Targets = append(v.Targets, tv)
	}
	return v
}

// inspect runs the first matching probe over every agent panel of an instance.
func (c *Core) inspect(ctx context.Context, inst revier.Instance) []revier.AgentView {
	var out []revier.AgentView
	for _, panel := range inst.Panels {
		if panel.Kind != revier.PanelAgent {
			continue
		}
		for _, probe := range c.Probes {
			if !probe.Match(panel) {
				continue
			}
			state, err := probe.Inspect(ctx, panel)
			if err != nil {
				// A probe that fails reports unknown rather than failing the
				// survey: one broken harness must not blank the dashboard.
				state = revier.AgentState{Harness: probe.Name(), Status: revier.StatusUnknown}
			}
			out = append(out, revier.AgentView{Panel: panel.ID, State: state})
			break
		}
	}
	return out
}
