// Package core holds everything that is not tool-specific: matching instances
// against realizations, choosing which realization wins, run-or-raise, and
// toggle-back. Adapters implement five methods and hold no policy, so this
// package is where behaviour is pinned and where most tests point.
package core

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		if ref.IsZero() {
			// The host launched a process and cannot name the window it will
			// produce; a window host is like this. The compositor focuses a
			// new window itself, and the next keypress finds it through Match.
			return ref, nil
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
	if !t.Home && c.focusedOn(ctx, snap, inst) {
		if home, ok := p.Home(); ok {
			return c.Go(ctx, p, home.Name)
		}
	}
	if err := host.Focus(ctx, inst.Ref); err != nil {
		return revier.TargetRef{}, fmt.Errorf("%s: focus %s: %w", host.Name(), name, err)
	}
	// A terminal cannot always raise the OS window it lives in - kitty on
	// Wayland cannot - so when the window host sees that window, it raises it.
	if osw, ok := c.osWindowOf(snap, inst); ok {
		if err := c.Window.Focus(ctx, osw.Ref); err != nil {
			return revier.TargetRef{}, fmt.Errorf("%s: raise %s: %w", c.Window.Name(), name, err)
		}
	}
	return inst.Ref, nil
}

// osWindowOf finds the window-host instance that is the OS window of a runtime
// instance. Only a runtime that reports OSWindows takes part: it gives each
// instance the title a window host reports for the same window, so the two
// listings describe one window from two sides. The pid is a filter on top -
// every OS window of one kitty process shares it - never the identity.
func (c *Core) osWindowOf(snap snapshot, inst revier.Instance) (revier.Instance, bool) {
	if c.Window == nil || c.Runtime == nil || inst.Ref.Host != c.Runtime.Name() || inst.Title == "" {
		return revier.Instance{}, false
	}
	if !c.Runtime.Capabilities().OSWindows {
		return revier.Instance{}, false
	}
	for _, w := range snap[c.Window.Name()] {
		if w.Title != inst.Title {
			continue
		}
		if w.PID != 0 && inst.PID != 0 && w.PID != inst.PID {
			continue
		}
		return w, true
	}
	return revier.Instance{}, false
}

// Focus activates a bare ref on the host that produced it. The picker uses it
// for an attached instance, which has no target to resolve: the ref is all
// revier knows about it.
func (c *Core) Focus(ctx context.Context, ref revier.TargetRef) error {
	for _, h := range c.hosts() {
		if h.Name() == ref.Host {
			return h.Focus(ctx, ref)
		}
	}
	return fmt.Errorf("%w: no host named %q", ErrNoHost, ref.Host)
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

// focusedOn reports whether inst is certainly where the user is standing.
//
// Certainty matters more than coverage here, because the consequence of a
// false positive is a keypress that goes home when the user asked to go
// somewhere: strictly worse than a keypress that does nothing surprising.
//
// So only the focus authority's answer counts, and only about its own ids.
// Ids are host-scoped - a GNOME window id and a tmux pane id are unrelated
// numbers - so comparing across hosts is meaningless, and a runtime's "current
// pane" says nothing about OS focus: a tmux window can be current while the
// user is looking at the editor.
//
// A runtime instance is therefore judged through the OS window that holds it,
// when the runtime can name one (osWindowOf). A multiplexer cannot, and for it
// toggle-back does not fire on a machine that has a window host.
func (c *Core) focusedOn(ctx context.Context, snap snapshot, inst revier.Instance) bool {
	auth := c.focusAuthority()
	if auth == nil || inst.Ref.ID == "" {
		return false
	}
	want := inst.Ref
	if want.Host != auth.Name() {
		osw, ok := c.osWindowOf(snap, inst)
		if !ok {
			return false
		}
		want = osw.Ref
	}
	cur, err := auth.Focused(ctx)
	if err != nil {
		return false
	}
	return cur.Host == want.Host && cur.ID == want.ID
}

// Report is one survey: the view every renderer reads, and the window host's
// listing it was built from, which claim-on-appear diffs between refreshes.
// Carrying the listing out keeps a refresh at one Instances call per host.
type Report struct {
	Views   []revier.ProjectView
	Windows []revier.Instance
}

// Survey builds the view every renderer reads: one bulk listing per host, then
// local matching for every project.
//
// Every project here is already rendered and compiled; a project that could
// not be was refused at load, so the survey has no per-project error path and
// does no work that a previous refresh did not also have to do.
func (c *Core) Survey(ctx context.Context, projects []Project) (Report, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	r := Report{Views: make([]revier.ProjectView, 0, len(projects))}
	for _, p := range projects {
		r.Views = append(r.Views, c.view(ctx, snap, p))
	}
	if c.Window != nil {
		r.Windows = snap[c.Window.Name()]
	}
	return r, nil
}

// ClaimWindow is how long after a detached launch a window that appears is
// attributed to the project that launched. Short on purpose: a wrong claim
// binds an unrelated window to a project and is only visible later, when a
// key goes somewhere surprising.
const ClaimWindow = 5 * time.Second

// Claim decides claim-on-appear on the polling path: among the windows in
// after that were not in before, the one to attach to the project that
// launched at launchedAt. It claims nothing rather than the wrong thing:
//
//   - nothing outside ClaimWindow after the launch,
//   - never a window a declared target of any project matches; that window
//     is reached by its key already and is not what this exists for,
//   - nothing when more than one candidate appeared at once, because then the
//     launch does not say which.
func (c *Core) Claim(before, after []revier.Instance, launchedAt, now time.Time, projects []Project) (revier.TargetRef, bool) {
	if !c.withinClaimWindow(launchedAt, now) {
		return revier.TargetRef{}, false
	}
	seen := map[string]bool{}
	for _, inst := range before {
		seen[inst.Ref.Host+"\x00"+inst.Ref.ID] = true
	}
	var candidates []revier.Instance
	for _, inst := range after {
		if seen[inst.Ref.Host+"\x00"+inst.Ref.ID] || c.declared(inst, projects) {
			continue
		}
		candidates = append(candidates, inst)
	}
	if len(candidates) != 1 {
		return revier.TargetRef{}, false
	}
	return candidates[0].Ref, true
}

// ClaimEvent decides claim-on-appear on the event path: whether a window that
// just appeared is the one to attach to the project that launched at
// launchedAt. The same bounds as Claim, for one window at a time.
func (c *Core) ClaimEvent(inst revier.Instance, launchedAt, now time.Time, projects []Project) bool {
	return c.withinClaimWindow(launchedAt, now) && !c.declared(inst, projects)
}

func (c *Core) withinClaimWindow(launchedAt, now time.Time) bool {
	if launchedAt.IsZero() {
		return false
	}
	age := now.Sub(launchedAt)
	return age >= 0 && age <= ClaimWindow
}

// declared reports whether any project's window realization matches the
// instance: it is then a declared target, not a stray.
func (c *Core) declared(inst revier.Instance, projects []Project) bool {
	for _, p := range projects {
		for i, t := range p.Targets {
			if t.Window != nil && p.compiled[i].window.Matches(inst) {
				return true
			}
		}
	}
	return false
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
