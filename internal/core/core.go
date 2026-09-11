// Package core holds everything that is not tool-specific: matching instances
// against realizations, choosing which realization wins, run-or-raise, and
// toggle-back. Adapters implement five methods and hold no policy, so this
// package is where behaviour is pinned and where most tests point.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

	// KeyBinder reads the desktop's keyboard shortcuts. It is not a Host: it
	// provides no instances and takes no part in run-or-raise, and a machine
	// with no desktop leaves it nil.
	KeyBinder revier.KeyBinder
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
	c.identify(s)
	return s, nil
}

// identify gives a runtime instance with no identity of its own the title the
// window host reports for the same process (decisions.md D22).
//
// It is policy, not an adapter's business: neither host can do it alone. The
// runtime knows the window has no name; only the window host knows what the
// window manager calls it.
//
// The pairing is by process, and it is refused unless that process owns
// exactly one window on each side. D19 rejected the process id for pairing a
// pane to a window, because every OS window of one kitty process shares its
// pid; that objection is exactly this refusal, so an ambiguous process is left
// as it was rather than guessed at.
func (c *Core) identify(s snapshot) {
	if c.Runtime == nil || c.Window == nil || !c.Runtime.Capabilities().OSWindows {
		return
	}
	runtimes, windows := s[c.Runtime.Name()], s[c.Window.Name()]

	perPID := map[int]int{}
	for _, r := range runtimes {
		if r.PID != 0 {
			perPID[r.PID]++
		}
	}
	byPID := map[int]revier.Instance{}
	seen := map[int]int{}
	for _, w := range windows {
		if w.PID == 0 {
			continue
		}
		seen[w.PID]++
		byPID[w.PID] = w
	}

	for i, r := range runtimes {
		if r.Title != "" || r.PID == 0 || perPID[r.PID] != 1 || seen[r.PID] != 1 {
			continue
		}
		w := byPID[r.PID]
		runtimes[i].Title = w.Title
		runtimes[i].Ref.Title = w.Title
		if runtimes[i].Class == "" {
			runtimes[i].Class = w.Class
		}
	}
}

// ProjectOfFocused reports the project the focused window belongs to.
//
// The match rules a project already declares are the mapping: a kitty window
// titled `session:revier`, an editor window whose title carries the project
// name, a browser window with the class the project gave it. Nothing is
// declared twice.
//
// The host that owns the focused instance is not required to be the host that
// would realize the target. The question here is which project a window
// belongs to, not which host provides it: the focused window of a running
// session is reported by the window host, while its home target is realized by
// the runtime, and requiring agreement would answer nothing.
func (c *Core) ProjectOfFocused(ctx context.Context, projects []Project) (Project, bool, error) {
	h := c.focusAuthority()
	if h == nil {
		return Project{}, false, nil
	}
	ref, err := h.Focused(ctx)
	if err != nil {
		return Project{}, false, fmt.Errorf("%s: focused: %w", h.Name(), err)
	}
	if ref.IsZero() {
		return Project{}, false, nil
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Project{}, false, err
	}
	inst, ok := byRef(snap, ref)
	if !ok {
		return Project{}, false, nil
	}
	for _, p := range projects {
		for i := range p.Targets {
			_, _, m, err := c.resolveAt(p, i)
			if err != nil {
				continue
			}
			if m.Matches(inst) {
				return p, true, nil
			}
		}
	}
	return Project{}, false, nil
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

// byRef returns the instance a ref points at, if the host still lists it.
func byRef(s snapshot, ref revier.TargetRef) (revier.Instance, bool) {
	if ref.IsZero() {
		return revier.Instance{}, false
	}
	for _, i := range s[ref.Host] {
		if i.Ref.ID == ref.ID {
			return i, true
		}
	}
	return revier.Instance{}, false
}

// Bindings is where a project's targets last landed, by instance id: the
// state a caller keeps between keypresses. A bound instance that is still
// listed is the target, whatever its title says now; the rule is consulted
// only when there is no binding, which is after a restart or for a window
// revier did not launch. This is what makes a key stable across the title
// changes an application makes after it opens.
type Bindings = map[revier.TargetName]revier.TargetRef

// locate finds the instance backing a target: its binding when alive, else
// the first instance the rule matches.
func (c *Core) locate(snap snapshot, p Project, i int, host revier.Host, m revier.CompiledMatch, bound revier.TargetRef) (revier.Instance, bool) {
	if bound.Host == host.Name() {
		if inst, ok := byRef(snap, bound); ok && c.bindingHolds(p, i, host, inst) {
			return inst, true
		}
	}
	return find(snap, host, m)
}

// bindingHolds re-checks a remembered instance against the class the target
// declares, before a keypress is sent to it.
//
// Pruning already drops a binding whose instance is gone. What is left is a
// binding that is alive and wrong: a window manager reuses window ids, so the
// id a target was bound to can come back as an unrelated window, and the
// keypress then raises that. Nothing about the failure is visible - the wrong
// window simply comes forward - which is why it is checked rather than left to
// be noticed.
//
// Only the class is re-checked, never the title. The whole point of a binding
// is to survive a title the rule no longer matches, which is what D21 exists
// for: an editor window is bound while its title still says the file it opened
// with. A target that declares no class is trusted as before, and so is a
// runtime binding: a pane id and a kitty window id are not handed back out.
func (c *Core) bindingHolds(p Project, i int, host revier.Host, inst revier.Instance) bool {
	if c.Window == nil || host.Name() != c.Window.Name() {
		return true
	}
	return c.classOK(p, i, inst)
}

// Result is what Go did. Target is the target the key landed on: the one
// asked for, or the project's home when the press toggled back, and the one a
// caller pins Ref to. Ref is where the key landed. Launched reports that the
// run half ran; with a zero Ref the host could not name the window it
// started, and Before is the window listing from before the launch, which
// Bind diffs against.
type Result struct {
	Target   revier.TargetName
	Ref      revier.TargetRef
	Launched bool
	Before   []revier.Instance
}

// Go runs-or-raises a target. Pressing the same key twice returns to the
// project's home target, which is what makes a binding a round trip rather than
// a one-way jump. bound is where the project's targets last landed; the caller
// records Result.Ref there afterwards, so the next press needs no rule.
func (c *Core) Go(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (Result, error) {
	i, ok := p.index(name)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	t := p.Targets[i]
	host, real, m, err := c.resolveAt(p, i)
	if err != nil {
		return Result{}, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Result{}, err
	}

	inst, found := c.locate(snap, p, i, host, m, bound[name])
	if !found {
		res := Result{Target: name, Launched: true}
		if c.Window != nil {
			res.Before = snap[c.Window.Name()]
		}
		ref, err := host.Open(ctx, real)
		if err != nil {
			return Result{}, fmt.Errorf("%s: open %s: %w", host.Name(), name, err)
		}
		if ref.IsZero() {
			// The host launched a process and cannot name the window it will
			// produce; a window host is like this. Bind waits for it.
			return res, nil
		}
		// Focus explicitly. Some hosts focus what they launch and some do not,
		// so without this the raise half of run-or-raise holds only by
		// accident of the host - the window opens behind on the ones that do
		// not. Go always leaves the target focused.
		if err := host.Focus(ctx, ref); err != nil {
			return Result{}, fmt.Errorf("%s: focus new %s: %w", host.Name(), name, err)
		}
		res.Ref = ref
		c.place(ctx, real, ref)
		return res, nil
	}

	// Toggle back: the target is already where focus is, so the second press
	// returns home instead of doing nothing.
	if !t.Home && c.focusedOn(ctx, snap, inst) {
		if home, ok := p.Home(); ok {
			return c.Go(ctx, p, home.Name, bound)
		}
	}
	if err := host.Focus(ctx, inst.Ref); err != nil {
		return Result{}, fmt.Errorf("%s: focus %s: %w", host.Name(), name, err)
	}
	// A terminal cannot always raise the OS window it lives in - kitty on
	// Wayland cannot - so when the window host sees that window, it raises it.
	if osw, ok := c.osWindowOf(snap, inst); ok {
		if err := c.Window.Focus(ctx, osw.Ref); err != nil {
			return Result{}, fmt.Errorf("%s: raise %s: %w", c.Window.Name(), name, err)
		}
	}
	return Result{Target: name, Ref: inst.Ref}, nil
}

// Running reports whether a target has an instance to raise: its binding while
// that is alive, else the first instance its rule matches. It is Go's raise
// half without the run half, for a press that must not launch a second copy
// of a target still coming up, and must still reach it once it is there.
func (c *Core) Running(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (bool, error) {
	i, ok := p.index(name)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return false, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return false, err
	}
	_, found := c.locate(snap, p, i, host, m, bound[name])
	return found, nil
}

// BindPoll is how often Bind asks the window host while waiting.
const BindPoll = 250 * time.Millisecond

// Bind waits for the window a detached launch produces and binds it, then
// raises it. The window is the first one, new since before, that the target's
// rule accepts by class alone: the class is right from the first frame while
// a title settles later, which is exactly when a rule would miss it. A window
// the full rule matches is taken at once; otherwise a single class candidate
// is taken and two at once are left alone, because the launch does not say
// which. It gives up after wait and reports false.
func (c *Core) Bind(ctx context.Context, p Project, name revier.TargetName, before []revier.Instance, wait time.Duration) (revier.Instance, bool, error) {
	i, ok := p.index(name)
	if !ok || c.Window == nil || p.Targets[i].Window == nil {
		return revier.Instance{}, false, nil
	}
	seen := map[string]bool{}
	for _, inst := range before {
		seen[key(inst.Ref)] = true
	}
	deadline := time.Now().Add(wait)
	for {
		windows, err := c.Window.Instances(ctx)
		if err != nil {
			return revier.Instance{}, false, err
		}
		var candidates []revier.Instance
		for _, w := range windows {
			if seen[key(w.Ref)] {
				continue
			}
			if p.compiled[i].window.Matches(w) {
				candidates = []revier.Instance{w}
				break
			}
			if c.classOK(p, i, w) {
				candidates = append(candidates, w)
			}
		}
		if len(candidates) == 1 {
			w := candidates[0]
			if err := c.Window.Focus(ctx, w.Ref); err != nil {
				return revier.Instance{}, false, fmt.Errorf("%s: raise new %s: %w", c.Window.Name(), name, err)
			}
			c.place(ctx, *p.Targets[i].Window, w.Ref)
			return w, true, nil
		}
		if len(candidates) > 1 || !time.Now().Before(deadline) {
			return revier.Instance{}, false, nil
		}
		select {
		case <-ctx.Done():
			return revier.Instance{}, false, ctx.Err()
		case <-time.After(BindPoll):
		}
	}
}

// PlaceWait is how long placement waits for the window host to see the OS
// window of a runtime instance that was just opened. It matches the wait the
// shell tool uses for the same purpose.
const PlaceWait = 2 * time.Second

// place positions a window a launch has just produced. It applies to a launch
// and never to a raise: moving a window the user has already put somewhere is
// not revier's business (decisions.md D24).
//
// A failure is not returned. The window is open and focused either way, and
// the alternative - failing the keypress because a geometry was refused -
// would turn a cosmetic problem into a broken key. A window pinned by
// maximize or tiling is refused by the host as a matter of course.
func (c *Core) place(ctx context.Context, real revier.Realization, ref revier.TargetRef) {
	if real.Place == "" || c.Window == nil || ref.IsZero() {
		return
	}
	placer, ok := c.Window.(revier.WindowPlacer)
	if !ok {
		return
	}
	target := ref
	if ref.Host != c.Window.Name() {
		w, ok := c.windowOfNew(ctx, ref)
		if !ok {
			return
		}
		target = w
	}
	_ = placer.Place(ctx, target, strings.Fields(real.Place))
}

// windowOfNew waits for the window host to report the OS window of a runtime
// instance that has just been opened. A terminal reports its window before the
// compositor has mapped it, so the first look often finds nothing.
func (c *Core) windowOfNew(ctx context.Context, ref revier.TargetRef) (revier.TargetRef, bool) {
	deadline := time.Now().Add(PlaceWait)
	for {
		if snap, err := c.snapshot(ctx); err == nil {
			if inst, ok := byRef(snap, ref); ok {
				if w, ok := c.osWindowOf(snap, inst); ok {
					return w.Ref, true
				}
			}
		}
		if !time.Now().Before(deadline) {
			return revier.TargetRef{}, false
		}
		select {
		case <-ctx.Done():
			return revier.TargetRef{}, false
		case <-time.After(BindPoll):
		}
	}
}

// classOK reports whether a window could be the i-th target's by class: the
// rule's class matches, or the rule constrains none.
func (c *Core) classOK(p Project, i int, w revier.Instance) bool {
	if !p.compiled[i].hasClass {
		return true
	}
	return p.compiled[i].windowClass.Matches(w)
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

// Report is one survey: the view every renderer reads, every instance it was
// built from, and the window host's part of that, which claim-on-appear diffs
// between refreshes. Carrying the listings out keeps a refresh at one
// Instances call per host, and gives state the live set to prune against.
// Hosts names the hosts listed, because a ref on any other host is absent
// from Instances whether or not it is alive.
type Report struct {
	Views     []revier.ProjectView
	Instances []revier.Instance
	Windows   []revier.Instance
	Hosts     []string
}

// Survey builds the view every renderer reads: one bulk listing per host, then
// local matching for every project. bound is where each project's targets
// last landed; a bound instance that is still listed is its target.
//
// Every project here is already rendered and compiled; a project that could
// not be was refused at load, so the survey has no per-project error path and
// does no work that a previous refresh did not also have to do.
func (c *Core) Survey(ctx context.Context, projects []Project, bound map[revier.ProjectName]Bindings) (Report, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Report{}, err
	}
	r := Report{Views: make([]revier.ProjectView, 0, len(projects))}
	for _, p := range projects {
		r.Views = append(r.Views, c.view(ctx, snap, p, bound[p.Name]))
	}
	for _, h := range c.hosts() {
		r.Hosts = append(r.Hosts, h.Name())
		r.Instances = append(r.Instances, snap[h.Name()]...)
	}
	if c.Window != nil {
		r.Windows = snap[c.Window.Name()]
	}
	return r, nil
}

// Launch is what a window that appears is attributed to: the project that
// launched, the target if the launch was one (else an action's, which may
// open anything), and when.
type Launch struct {
	Project Project
	Target  revier.TargetName
	At      time.Time
}

// Claimed is the outcome of a claim: the window, and the target it was bound
// to, or none for an attachment.
type Claimed struct {
	Ref    revier.TargetRef
	Target revier.TargetName
}

// ClaimWindow is how long after an action's launch a window that appears is
// attached to the project. Short on purpose: a wrong claim binds an unrelated
// window to a project and is only visible later, when a key goes somewhere
// surprising.
const ClaimWindow = 5 * time.Second

// BindWindow is how long after a target's launch a window of its class is
// still bound to it. Longer than ClaimWindow because the class filter makes
// a wrong binding unlikely and an editor's cold start takes this long.
const BindWindow = 60 * time.Second

// Claim decides what a window that is new since the previous survey means
// for the last launch. For a target's launch it is a binding: the window the
// target's rule accepts by class, which Bind may have missed because the
// window took longer than its wait. For an action's launch it is an
// attachment: a window no declared target of any project matches, since a
// declared one is reached by its key already. Either way it claims nothing
// rather than the wrong thing: nothing outside the window after the launch,
// and nothing when more than one candidate appeared at once.
func (c *Core) Claim(before, after []revier.Instance, l Launch, now time.Time, projects []Project) (Claimed, bool) {
	if !l.Pending(now) {
		return Claimed{}, false
	}
	seen := map[string]bool{}
	for _, inst := range before {
		seen[key(inst.Ref)] = true
	}
	var candidates []revier.Instance
	for _, inst := range after {
		if !seen[key(inst.Ref)] && c.candidate(inst, l, projects) {
			candidates = append(candidates, inst)
		}
	}
	if len(candidates) != 1 {
		return Claimed{}, false
	}
	return Claimed{Ref: candidates[0].Ref, Target: l.Target}, true
}

// ClaimEvent is Claim for one window a watching host just reported.
func (c *Core) ClaimEvent(inst revier.Instance, l Launch, now time.Time, projects []Project) (Claimed, bool) {
	if !l.Pending(now) || !c.candidate(inst, l, projects) {
		return Claimed{}, false
	}
	return Claimed{Ref: inst.Ref, Target: l.Target}, true
}

func (c *Core) candidate(inst revier.Instance, l Launch, projects []Project) bool {
	if l.Target == "" {
		return !c.declared(inst, projects)
	}
	i, ok := l.Project.index(l.Target)
	if !ok || l.Project.Targets[i].Window == nil {
		return false
	}
	return l.Project.compiled[i].window.Matches(inst) || c.classOK(l.Project, i, inst)
}

// Pending reports whether now is still inside the launch's window: a window
// that appears can still be claimed for it, and until then it is not over.
func (l Launch) Pending(now time.Time) bool {
	if l.At.IsZero() {
		return false
	}
	limit := ClaimWindow
	if l.Target != "" {
		limit = BindWindow
	}
	age := now.Sub(l.At)
	return age >= 0 && age <= limit
}

// declared reports whether any project's rule matches the instance: it is
// then a declared target, not a stray. A runtime rule counts as well as a
// window rule: a terminal's OS window carries the title its runtime rule
// matches, and the window host lists it (decisions.md D19).
func (c *Core) declared(inst revier.Instance, projects []Project) bool {
	for _, p := range projects {
		for i, t := range p.Targets {
			if t.Window != nil && p.compiled[i].window.Matches(inst) {
				return true
			}
			if t.Runtime != nil && p.compiled[i].runtime.Matches(inst) {
				return true
			}
		}
	}
	return false
}

// dirExists is one stat per project per survey. It scales with the project
// count, which the Instances rule forbids for host calls; a stat is not a host
// call and costs microseconds on a local filesystem, which is where a project
// directory is.
func dirExists(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func (c *Core) view(ctx context.Context, snap snapshot, p Project, bound Bindings) revier.ProjectView {
	v := revier.ProjectView{Project: p.Project, PathExists: dirExists(p.Path)}

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
			if inst, found := c.locate(snap, p, i, host, m, bound[t.Name]); found {
				tv.Ref = inst.Ref
				if t.Home {
					v.Running, v.Home = true, inst.Ref
				}
				// One instance can back two targets; probing it twice would
				// report the same agent twice.
				if k := key(inst.Ref); !seen[k] {
					seen[k] = true
					v.Agents = append(v.Agents, c.inspect(ctx, inst)...)
				}
			}
		}
		v.Targets = append(v.Targets, tv)
	}
	return v
}

// key is the identity of a ref within one survey.
func key(ref revier.TargetRef) string { return ref.Host + "\x00" + ref.ID }

// inspect runs the first matching probe over every panel of an instance. A
// probe's Match decides what an agent panel is, not the host's PanelKind: a
// host knows only the harnesses it was written with, and a probe declared in
// config exists for the one it was not. A shell in the foreground is the one
// thing that overrules a probe, as it does for `revier agent`: the marker a
// harness leaves outlives it in a --hold window.
func (c *Core) inspect(ctx context.Context, inst revier.Instance) []revier.AgentView {
	var out []revier.AgentView
	for _, panel := range inst.Panels {
		if probe, ok := c.agentProbe(panel); ok {
			out = append(out, revier.AgentView{Panel: panel.ID, State: c.read(ctx, probe, panel)})
		}
	}
	return out
}

// probeFor returns the first probe that claims the panel.
func (c *Core) probeFor(panel revier.Panel) (revier.AgentProbe, bool) {
	for _, probe := range c.Probes {
		if probe.Match(panel) {
			return probe, true
		}
	}
	return nil, false
}

// read runs a probe over a panel. A probe that fails reports unknown rather
// than failing the survey: one broken harness must not blank the dashboard.
func (c *Core) read(ctx context.Context, probe revier.AgentProbe, panel revier.Panel) revier.AgentState {
	state, err := probe.Inspect(ctx, panel)
	if err != nil {
		return revier.AgentState{Harness: probe.Name(), Status: revier.StatusUnknown}
	}
	return state
}
