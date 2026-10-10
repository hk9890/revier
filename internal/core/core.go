// Package core holds everything that is not tool-specific: matching instances
// against realizations, choosing which realization wins, run-or-raise, and
// toggle-back. Adapters implement five methods and hold no policy, so this
// package is where behaviour is pinned and where most tests point.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
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

	// Served lists what `revier agent exec` started here for a terminal on
	// another machine, where no runtime of this machine holds it
	// (decisions.md D84). Its instances are matched by a target's runtime
	// realization and probed beside the instance the runtime holds. It opens
	// and focuses nothing, and no target resolves to it. Nil without one.
	Served revier.Host

	// Self reports a panel this process runs under: the panel's process is
	// this process or one of its ancestors. Shutdown puts the steps that
	// would end this process last (CloseLast), so a shutdown run from a
	// terminal of a workspace closes everything else before its own
	// terminal. Reading a process's ancestors is a fact about this machine,
	// so the wiring supplies it; nil keeps a plan's order.
	Self func(revier.Panel) bool

	// Machine is this machine's name, as `uname -n` prints it: the first half
	// of the tag a link's panel gives what it starts on a host. Empty reads
	// it from the kernel.
	Machine string

	// Remotes are the revier installations on other machines, by host
	// (decisions.md D40). NewRemote makes one for a host not yet here - a
	// link written while the surface runs, or a host the link dialog asks
	// about - and the result is kept. With no NewRemote, a host with no
	// entry is reported unreachable, not refused.
	Remotes   map[string]revier.Remote
	NewRemote func(host string) revier.Remote
	remotesMu sync.Mutex

	// seen is the last listing of every host: where Details finds an agent's
	// panel, so a detail costs no listing of its own.
	seen   snapshot
	seenMu sync.Mutex

	// windows is the window host's listing in the last report Settle took,
	// and listedWindows whether there was one: what a window must be new
	// since to be claimed. Guarded by seenMu.
	windows       []revier.Instance
	listedWindows bool

	// Ledger is the state and the events of this installation: where every
	// operation reads what was learned at runtime, and where it writes
	// (decisions.md D120). Nil remembers nothing.
	Ledger Ledger

	// KeyBinder reads the desktop's keyboard shortcuts. It is not a Host: it
	// provides no instances and takes no part in run-or-raise, and a machine
	// with no desktop leaves it nil.
	KeyBinder revier.KeyBinder
}

// WithRuntime is the same core over another runtime host. It is a new core
// and not a changed field, so a survey still running on the old one reads
// the host it started with, and the two never share a mutable map: the
// remotes known so far are copied.
func (c *Core) WithRuntime(rt revier.Runtime) *Core {
	out := c.clone()
	out.Runtime = rt
	return out
}

// WithLedger is the same core over another ledger: a new core, as
// WithRuntime's is. A surface uses it for a restore whose writes it wants to
// hear of.
func (c *Core) WithLedger(l Ledger) *Core {
	out := c.clone()
	out.Ledger = l
	return out
}

// clone copies what a core is configured with and the window listing it last
// settled, which is of the window host and so stands for the copy too.
func (c *Core) clone() *Core {
	c.remotesMu.Lock()
	defer c.remotesMu.Unlock()
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	return &Core{
		Runtime:       c.Runtime,
		Window:        c.Window,
		Probes:        c.Probes,
		Served:        c.Served,
		Self:          c.Self,
		Machine:       c.Machine,
		Remotes:       maps.Clone(c.Remotes),
		NewRemote:     c.NewRemote,
		windows:       c.windows,
		listedWindows: c.listedWindows,
		Ledger:        c.Ledger,
		KeyBinder:     c.KeyBinder,
	}
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

// allHosts is every host a snapshot lists: the configured ones, and the served
// processes.
func (c *Core) allHosts() []revier.Host {
	out := c.hosts()
	if c.Served != nil {
		out = append(out, c.Served)
	}
	return out
}

// served is the instance of the served processes that the i-th target's
// runtime realization matches.
func (c *Core) served(snap snapshot, p Project, i int) (revier.Instance, bool) {
	if c.Served == nil || p.Targets[i].Runtime == nil {
		return revier.Instance{}, false
	}
	return find(snap, c.Served, p.compiled[i].runtime)
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
//
// A tab has no instance of its own to match, and is refused here. Every lookup
// that walks a project's targets resolves each one first, so a tab fails
// closed in all of them; the few that serve tabs branch before this.
func (c *Core) resolveAt(p Project, i int) (revier.Host, revier.Realization, revier.CompiledMatch, error) {
	// A project its file refused as a whole, and a target its own
	// configuration refused, resolve to nothing, here, where every lookup
	// that walks a project's targets passes. The keypress then fails loudly
	// with the reason instead of running something built from a launch argv
	// that did not render, or in a project with no path (decisions.md D85).
	if p.Invalid != nil {
		return nil, revier.Realization{}, revier.CompiledMatch{}, p.Invalid
	}
	if err := p.compiled[i].err; err != nil {
		return nil, revier.Realization{}, revier.CompiledMatch{}, err
	}
	if p.isTab(i) {
		return nil, revier.Realization{}, revier.CompiledMatch{}, fmt.Errorf("target %q: %w", p.Targets[i].Name, errTab)
	}
	host, real, kind, err := c.resolve(p.Targets[i])
	if err != nil {
		return nil, revier.Realization{}, revier.CompiledMatch{}, err
	}
	if kind == revier.HostRuntime && p.Remote != nil {
		if real, err = c.linkPanels(p, real); err != nil {
			return nil, revier.Realization{}, revier.CompiledMatch{}, err
		}
	}
	m := p.compiled[i].runtime
	if kind == revier.HostWindow {
		m = p.compiled[i].window
	}
	return host, real, m, nil
}

// linkPanels gives a link's panels the argv that reaches the host: the
// remote port's, asked here so that config, which derives the panels, knows
// no transport, and every consumer of the realization - a launch, a tab, a
// resume - sees one argv. A panel the link declared with a command of its own
// keeps it.
func (c *Core) linkPanels(p Project, real revier.Realization) (revier.Realization, error) {
	fill := false
	for _, spec := range real.Panels {
		if len(spec.Command) == 0 && (spec.Kind == revier.PanelAgent || spec.Kind == revier.PanelShell) {
			fill = true
		}
	}
	if !fill {
		return real, nil
	}
	remote, err := c.remote(p.Remote.Host)
	if err != nil {
		return revier.Realization{}, err
	}
	real.Panels = slices.Clone(real.Panels)
	for i, spec := range real.Panels {
		if len(spec.Command) == 0 && (spec.Kind == revier.PanelAgent || spec.Kind == revier.PanelShell) {
			real.Panels[i].Command = remote.PanelCommand(p.Remote.Project, spec.Kind)
		}
	}
	return real, nil
}

// snapshot is one bulk listing per host, taken once and matched against every
// project locally. This is why a refresh costs one call per host rather than
// one per project.
type snapshot map[string][]revier.Instance

func (c *Core) snapshot(ctx context.Context) (snapshot, error) {
	s, failed := c.listing(ctx)
	return s, failed.err()
}

// answered is a listing the named hosts answered. Another host's failure
// costs only what that host lists (decisions.md D89), so an operation names
// the hosts whose instances it reads, and no others. An empty name is no host.
func (c *Core) answered(ctx context.Context, hosts ...string) (snapshot, error) {
	snap, failed := c.listing(ctx)
	for _, h := range hosts {
		if err := failed[h]; err != nil {
			return nil, err
		}
	}
	return snap, nil
}

// nameOf is the host's name, or empty for no host.
func nameOf(h revier.Host) string {
	if h == nil {
		return ""
	}
	return h.Name()
}

// hostErrs is why each host that could not list is missing from a listing,
// by host name.
type hostErrs map[string]error

// err is every host's failure, by host name, or nil.
func (e hostErrs) err() error {
	if len(e) == 0 {
		return nil
	}
	hosts := slices.Sorted(maps.Keys(e))
	errs := make([]error, 0, len(hosts))
	for _, h := range hosts {
		errs = append(errs, e[h])
	}
	return errors.Join(errs...)
}

// listing is one bulk listing per host, with the hosts that could not list
// left out and their failures returned beside it. A host that cannot answer
// costs its own targets and no other's (decisions.md D89): a survey over the
// other hosts still stands, and a press on a target of the failed host is
// refused with its reason. The failure is logged here, once per host and
// cause (logging.Repeat): a survey that degraded is not a survey that
// failed, so nothing above logs it. The hosts are independent and listed at
// once, so a listing costs the slowest host and not the sum of them.
func (c *Core) listing(ctx context.Context) (snapshot, hostErrs) {
	hosts := c.allHosts()
	lists := make([][]revier.Instance, len(hosts))
	errs := make([]error, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Go(func() { lists[i], errs[i] = h.Instances(ctx) })
	}
	wg.Wait()
	s := make(snapshot)
	failed := hostErrs{}
	for i, h := range hosts {
		in, err := lists[i], errs[i]
		logging.Repeat("instances\x00"+h.Name(), "instances", err, "host", h.Name())
		if err != nil {
			failed[h.Name()] = fmt.Errorf("%s: instances: %w", h.Name(), err)
			continue
		}
		s[h.Name()] = in
	}
	c.identify(s)
	c.seenMu.Lock()
	c.seen = s
	c.seenMu.Unlock()
	return s, failed
}

// identify gives a runtime instance with no identity of its own the title the
// window host reports for the same process (decisions.md D22).
//
// It is policy, not an adapter's business: neither host can do it alone. The
// runtime knows the window has no name; only the window host knows what the
// window manager calls it.
//
// The pairing is by process, and it is refused unless that process owns
// exactly one unnamed window on the runtime side and exactly one window on the
// window host's side that no named runtime window already claims by title, and
// every named runtime window of the process is found there by its title, one
// window per named window, so two named windows of one title need two. The
// process id was rejected for pairing a pane to a window, because every OS
// window of one kitty process shares its pid (D19); that objection is exactly
// this refusal, so an ambiguous process is left as it was rather than guessed
// at (D63, D67). A named window the window host does not list could
// be the one left over, and its title would then be lent to the wrong window.
func (c *Core) identify(s snapshot) {
	if c.Runtime == nil || c.Window == nil || !c.Runtime.Capabilities().OSWindows {
		return
	}
	runtimes, windows := s[c.Runtime.Name()], s[c.Window.Name()]

	unnamed := map[int]int{}
	claimed := map[int]map[string]int{}
	for _, r := range runtimes {
		if r.PID == 0 {
			continue
		}
		if r.Title == "" {
			unnamed[r.PID]++
			continue
		}
		if claimed[r.PID] == nil {
			claimed[r.PID] = map[string]int{}
		}
		claimed[r.PID][r.Title]++
	}
	byPID := map[int]revier.Instance{}
	seen := map[int]int{}
	found := map[int]map[string]int{}
	for _, w := range windows {
		if w.PID == 0 {
			continue
		}
		if claimed[w.PID][w.Title] > 0 {
			if found[w.PID] == nil {
				found[w.PID] = map[string]int{}
			}
			found[w.PID][w.Title]++
			continue
		}
		seen[w.PID]++
		byPID[w.PID] = w
	}

	for i, r := range runtimes {
		if r.Title != "" || r.PID == 0 || unnamed[r.PID] != 1 || seen[r.PID] != 1 || !maps.Equal(found[r.PID], claimed[r.PID]) {
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
	snap, err := c.answered(ctx, ref.Host)
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
// is to survive a title the rule no longer matches (decisions.md D21): an
// editor window is bound while its title still says the file it opened with.
// A target that declares no class is trusted as before, and so is a runtime
// binding: a runtime id is never handed back out, because the kitty and the
// tmux host put the process or the server that assigned it into it.
func (c *Core) bindingHolds(p Project, i int, host revier.Host, inst revier.Instance) bool {
	if c.Window == nil || host.Name() != c.Window.Name() {
		return true
	}
	return c.classOK(p, i, inst)
}

// Result is what a press did. Target is the target the key landed on: the one
// asked for, the project's home when the press toggled back, or the target a
// tab is inside, and the one a caller pins Ref to. Ref is where the key
// landed, and it survives a failure to focus an instance the launch made.
// Launched reports that the run half ran; with a zero Ref the host could not
// name the window it started, and Before is the window listing from before
// the launch, which the wait for its window diffs against. Agents is what the launch did with
// each recorded agent. ComingUp reports a press that did nothing because
// the target's earlier launch has not produced its window yet.
type Result struct {
	Target revier.TargetName
	// Tab is the tab target the press made current inside Target. Target
	// stays the instance's, which is what a caller binds; Tab is what the
	// user pressed and reached, and Launched then says the tab was opened.
	Tab revier.TargetName
	// Tabs is the tab targets a launch of Target opened with the new
	// instance, in order (decisions.md D126). It is set also when the launch
	// then failed: the instance is open, and those tabs are in it.
	Tabs     []revier.TargetName
	Ref      revier.TargetRef
	Launched bool
	ComingUp bool
	Before   []revier.Instance
	Agents   []AgentOutcome
	// AgentErr is why an agent tab failed to open in a workspace that did
	// open: the launch succeeded, and the agents it names are AgentNotAdded.
	AgentErr error
}

// goTo runs-or-raises a target. Pressing the same key twice returns to the
// project's home target, which is what makes a binding a round trip rather than
// a one-way jump. bound is where the project's targets last landed; the
// activation records Result.Ref there afterwards, so the next press needs no
// rule.
func (c *Core) goTo(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (Result, error) {
	return c.goToResuming(ctx, p, name, bound, nil)
}

// goToResuming is goTo with the agent panels of a launch started on the
// conversations they held, which is what makes a restored workspace a
// continuation rather than an empty one. It is goTo in every other respect,
// toggle-back included, and resumes apply only to the run half: a target
// already up is raised as it stands, because the agent in it is already the
// one the recording named.
//
// Every call is one line of the log, and a goTo that calls goTo writes one
// more: a toggle back for the way home, a tab for its workspace.
func (c *Core) goToResuming(ctx context.Context, p Project, name revier.TargetName, bound Bindings, resumes []Resume) (res Result, err error) {
	start := time.Now()
	defer func() {
		logging.Op("go", start, err, "project", p.Name, "target", name, "landed", res.Target,
			"launched", res.Launched, "tabs", res.Tabs, "ref", res.Ref, "resumes", len(resumes), "agents", res.Agents, "agent_err", res.AgentErr)
	}()
	return c.goResuming(ctx, p, name, bound, resumes)
}

func (c *Core) goResuming(ctx context.Context, p Project, name revier.TargetName, bound Bindings, resumes []Resume) (Result, error) {
	i, ok := p.index(name)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	if p.isTab(i) {
		return c.goTab(ctx, p, i, bound, resumes)
	}
	t := p.Targets[i]
	host, real, m, err := c.resolveAt(p, i)
	if err != nil {
		return Result{}, err
	}
	opener, err := c.tabsHost(name, real, host)
	if err != nil {
		return Result{}, err
	}
	// The target's own host must have answered: a launch over a listing it
	// is missing from would open a second copy. Another host's failure costs
	// only what that host lists.
	snap, failed := c.listing(ctx)
	if err := failed[host.Name()]; err != nil {
		return Result{}, err
	}

	inst, found := c.locate(snap, p, i, host, m, bound[name])
	if !found {
		res := Result{Target: name, Launched: true}
		if c.Window != nil {
			res.Before = snap[c.Window.Name()]
		}
		// The agent tabs are copies of the realization as declared, not of
		// the launch the first agents were written into.
		launch, agents, extra := c.resuming(real, resumes, p.Remote != nil)
		// The instance's own panels are marked with the target it was opened
		// for, so a return home from a tab lands in the first of them rather
		// than in the first panel that happens to carry no tab mark
		// (decisions.md D100).
		launch.Vars = map[string]string{PanelHomeVar: string(name)}
		// A target that lists its tabs opens as the first of them, under that
		// tab's mark, and the others open after it: the order of the tabs is
		// the order they are opened in (decisions.md D126).
		var tabs opening
		if opener != nil {
			if tabs, err = p.opening(name, real); err != nil {
				return Result{}, err
			}
			launch = tabs.first(real)
		}
		ref, err := host.Open(ctx, launch)
		if err != nil {
			return Result{}, fmt.Errorf("%s: open %s: %w", host.Name(), name, err)
		}
		if opener != nil {
			res.Tabs = []revier.TargetName{tabs.tabs[0].Name}
		}
		if ref.IsZero() {
			// The host launched a process and cannot name the window it will
			// produce; a window host is like this. bindWindow waits for it, and
			// nothing can be added to a window not yet named.
			for range extra {
				agents = append(agents, AgentDropped)
			}
			res.Agents = agents
			return res, nil
		}
		res.Ref = ref
		var active revier.PanelID
		if opener != nil {
			if res.Tabs, active, err = tabs.rest(ctx, opener, ref); err != nil {
				// The instance stays open with the tabs it has, and is reported
				// with the failure, so the caller pins it and the next press
				// raises it instead of opening another. No agent started.
				res.Agents = notAdded(append(agents, make([]AgentOutcome, len(extra))...), 0)
				return res, fmt.Errorf("%s: %w", host.Name(), err)
			}
		}
		added, err := c.addAgents(ctx, host, real, ref, extra, p.Remote != nil)
		res.Agents, res.AgentErr = append(agents, added...), err
		// Focus explicitly. Some hosts focus what they launch and some do not,
		// so without this the raise half of run-or-raise holds only by
		// accident of the host - the window opens behind on the ones that do
		// not. A press always leaves the target focused.
		if err := host.Focus(ctx, ref); err != nil {
			// The launch ran and the instance exists: its ref and agents are
			// reported with the failure, so the caller pins it and the next
			// press raises it instead of opening another.
			return res, fmt.Errorf("%s: focus new %s: %w", host.Name(), name, err)
		}
		if opener != nil {
			// After every tab and every agent tab is open, so that neither
			// leaves another tab current.
			if err := c.focusActive(ctx, opener, tabs, ref, active); err != nil {
				return res, err
			}
		}
		c.place(ctx, real, ref)
		return res, nil
	}

	// Toggle back: the target is already where focus is, so the second press
	// returns home instead of doing nothing.
	if !t.Home && c.focusedOn(ctx, snap, inst) {
		if home, ok := p.Home(); ok {
			return c.goTo(ctx, p, home.Name, bound)
		}
	}
	osw, err := c.raisable(snap, inst, name)
	if err != nil {
		return Result{}, err
	}
	if err := host.Focus(ctx, inst.Ref); err != nil {
		return Result{}, fmt.Errorf("%s: focus %s: %w", host.Name(), name, err)
	}
	if err := c.raise(ctx, osw, name); err != nil {
		return Result{}, err
	}
	return Result{Target: name, Ref: inst.Ref}, nil
}

// ErrUnraisable is returned when a runtime instance's OS window cannot be
// found in the window host's listing, so it could be focused inside the
// terminal but not raised.
var ErrUnraisable = errors.New("cannot raise: the window host lists no window for it")

// raisable finds the OS window a raise of inst goes through: zero when inst
// needs none, and an error when it needs one and the window host lists none.
//
// A terminal cannot always raise the OS window it lives in - kitty on Wayland
// cannot - so the window host raises it. Focusing inside the terminal without
// that raise is refused rather than done: GNOME answers such a request with a
// "is ready" notification instead of the window, which reads as a raise that
// half worked. The error names the target, and nothing moves (decisions.md
// D63).
func (c *Core) raisable(snap snapshot, inst revier.Instance, name revier.TargetName) (revier.TargetRef, error) {
	if !c.bridged(inst) {
		return revier.TargetRef{}, nil
	}
	osw, ok := c.osWindowOf(snap, inst)
	if !ok {
		return revier.TargetRef{}, fmt.Errorf("%s: %s: %w", c.Window.Name(), name, ErrUnraisable)
	}
	return osw.Ref, nil
}

// raise activates the OS window raisable found, if it found one.
func (c *Core) raise(ctx context.Context, osw revier.TargetRef, name revier.TargetName) error {
	if osw.IsZero() {
		return nil
	}
	if err := c.Window.Focus(ctx, osw); err != nil {
		return fmt.Errorf("%s: raise %s: %w", c.Window.Name(), name, err)
	}
	return nil
}

// Running reports whether a target has an instance to raise: its binding while
// that is alive, else the first instance its rule matches. It is the raise
// half of a press without the run half, for a press that must not launch a
// second copy of a target still coming up, and must still reach it once it is
// there.
func (c *Core) Running(ctx context.Context, p Project, name revier.TargetName) (bool, error) {
	return c.isUp(ctx, p, name, c.state().Bound[p.Name])
}

func (c *Core) isUp(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (bool, error) {
	i, ok := p.index(name)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	if p.isTab(i) {
		snap, err := c.answered(ctx, nameOf(c.Runtime))
		if err != nil {
			return false, err
		}
		_, _, _, open, err := c.container(snap, p, i, bound)
		return open, err
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return false, err
	}
	snap, failed := c.listing(ctx)
	if err := failed[host.Name()]; err != nil {
		return false, err
	}
	_, found := c.locate(snap, p, i, host, m, bound[name])
	return found, nil
}

// BindPoll is how often the wait for a launch's window asks the window host.
const BindPoll = 250 * time.Millisecond

// bindWindow waits for the window a detached launch produces and binds it, then
// raises it. The window is the first one, new since before, that the target's
// rule accepts by class alone: the class is right from the first frame while
// a title settles later, which is exactly when a rule would miss it. A window
// the full rule matches is taken at once; otherwise a single class candidate
// is taken and two at once are left alone, because the launch does not say
// which. It gives up after wait and reports false. A window it found and
// could not raise is reported with the failure, as goTo reports an instance it
// launched and could not focus, so the activation pins it.
func (c *Core) bindWindow(ctx context.Context, p Project, name revier.TargetName, before []revier.Instance, wait time.Duration) (inst revier.Instance, ok bool, err error) {
	start := time.Now()
	defer func() {
		logging.Op("bind", start, err, "project", p.Name, "target", name, "bound", ok, "ref", inst.Ref)
	}()
	return c.bind(ctx, p, name, before, wait)
}

func (c *Core) bind(ctx context.Context, p Project, name revier.TargetName, before []revier.Instance, wait time.Duration) (revier.Instance, bool, error) {
	i, ok := p.index(name)
	if !ok || c.Window == nil || p.Targets[i].Window == nil {
		return revier.Instance{}, false, nil
	}
	w, found, err := c.awaitNew(ctx, before, wait, BindPoll, func(fresh []revier.Instance) (revier.Instance, bool, bool) {
		var candidates []revier.Instance
		for _, w := range fresh {
			if p.compiled[i].window.Matches(w) {
				return w, true, false
			}
			if c.classOK(p, i, w) {
				candidates = append(candidates, w)
			}
		}
		if len(candidates) > 1 {
			slog.Warn("bind: more than one new window of the class, none bound", "project", p.Name, "target", name, "candidates", len(candidates))
			return revier.Instance{}, false, true
		}
		if len(candidates) == 1 {
			return candidates[0], true, false
		}
		return revier.Instance{}, false, false
	})
	if errors.Is(err, errNoNewWindow) {
		slog.Warn("bind: no window appeared in the wait, left to the TUI", "project", p.Name, "target", name, "wait", wait.String())
		return revier.Instance{}, false, nil
	}
	if err != nil || !found {
		return revier.Instance{}, false, err
	}
	if err := c.Window.Focus(ctx, w.Ref); err != nil {
		return w, true, fmt.Errorf("%s: raise new %s: %w", c.Window.Name(), name, err)
	}
	c.place(ctx, *p.Targets[i].Window, w.Ref)
	return w, true, nil
}

// errNoNewWindow is awaitNew running out of time.
var errNoNewWindow = errors.New("no new window appeared")

// awaitNew asks the window host every poll, for at most wait, for the windows
// not listed in before, and hands them to pick. pick returns the window it
// settles on and found, or stop to end the wait without one. bindWindow and Popup
// both wait for the window a detached launch produces, and share this so the
// two waits cannot drift apart.
func (c *Core) awaitNew(ctx context.Context, before []revier.Instance, wait, poll time.Duration,
	pick func(fresh []revier.Instance) (w revier.Instance, found, stop bool),
) (revier.Instance, bool, error) {
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
		fresh := make([]revier.Instance, 0, len(windows))
		for _, w := range windows {
			if !seen[key(w.Ref)] {
				fresh = append(fresh, w)
			}
		}
		if w, found, stop := pick(fresh); found || stop {
			return w, found, nil
		}
		if !time.Now().Before(deadline) {
			return revier.Instance{}, false, errNoNewWindow
		}
		select {
		case <-ctx.Done():
			return revier.Instance{}, false, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// PlaceWait is how long placement waits for the window host to see the OS
// window of a runtime instance that was just opened. A window the compositor
// never maps holds the keypress this long, so the wait stays short.
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
			slog.Warn("place: the window host never listed the new window", "ref", ref, "place", real.Place)
			return
		}
		target = w
	}
	if err := placer.Place(ctx, target, strings.Fields(real.Place)); err != nil {
		slog.Warn("place", "ref", target, "place", real.Place, "err", err)
	}
}

// windowOfNew waits for the window host to report the OS window of a runtime
// instance that has just been opened. A terminal reports its window before the
// compositor has mapped it, so the first look often finds nothing.
func (c *Core) windowOfNew(ctx context.Context, ref revier.TargetRef) (revier.TargetRef, bool) {
	deadline := time.Now().Add(PlaceWait)
	for {
		snap, err := c.snapshot(ctx)
		if err == nil {
			if inst, ok := byRef(snap, ref); ok {
				if w, ok := c.osWindowOf(snap, inst); ok {
					return w.Ref, true
				}
			}
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				slog.Warn("place: listing the hosts", "err", err)
			}
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
//
// It refuses as soon as two windows answer to one terminal, as runtimeOf
// read the other way does and for the same reason (decisions.md D63, D67):
// the window that is raised, and the window a row is folded into, are both
// the wrong one when the title is shared and the pairing was a guess.
func (c *Core) osWindowOf(snap snapshot, inst revier.Instance) (revier.Instance, bool) {
	if !c.bridged(inst) || inst.Title == "" {
		return revier.Instance{}, false
	}
	var found revier.Instance
	n := 0
	for _, w := range snap[c.Window.Name()] {
		if w.Title != inst.Title {
			continue
		}
		if w.PID != 0 && inst.PID != 0 && w.PID != inst.PID {
			continue
		}
		found, n = w, n+1
	}
	return found, n == 1
}

// runtimeOf finds the runtime instance that is the terminal inside an OS
// window. It is osWindowOf read the other way, through the same identity -
// the title the two listings share, with the pid as a filter - and it refuses
// as soon as two runtime instances answer to it, because a guess here would
// attach a project to the wrong terminal (decisions.md D63, D67).
func (c *Core) runtimeOf(instances []revier.Instance, w revier.Instance) (revier.Instance, bool) {
	if c.Runtime == nil || c.Window == nil || !c.Runtime.Capabilities().OSWindows ||
		w.Ref.Host != c.Window.Name() || w.Title == "" {
		return revier.Instance{}, false
	}
	var found revier.Instance
	n := 0
	for _, r := range instances {
		if r.Ref.Host != c.Runtime.Name() || r.Title != w.Title {
			continue
		}
		if r.PID != 0 && w.PID != 0 && r.PID != w.PID {
			continue
		}
		found, n = r, n+1
	}
	return found, n == 1
}

// Attachment is what attaching an instance records: the ref the user pointed
// at, and the terminal inside it when that window is one this machine's
// runtime holds and the two listings pair beyond doubt (decisions.md D95). It
// takes its own listing; a host that could not answer costs the pairing and
// not the attachment, so the window alone is recorded (decisions.md D89).
func (c *Core) Attachment(ctx context.Context, ref revier.TargetRef) []revier.TargetRef {
	snap, _ := c.listing(ctx)
	var instances []revier.Instance
	for _, h := range c.allHosts() {
		instances = append(instances, snap[h.Name()]...)
	}
	return c.attachment(instances, ref)
}

// attachment is Attachment against a listing already taken.
func (c *Core) attachment(instances []revier.Instance, ref revier.TargetRef) []revier.TargetRef {
	refs := []revier.TargetRef{ref}
	i := slices.IndexFunc(instances, func(in revier.Instance) bool { return key(in.Ref) == key(ref) })
	if i < 0 {
		return refs
	}
	if rt, ok := c.runtimeOf(instances, instances[i]); ok {
		refs = append(refs, rt.Ref)
	}
	return refs
}

// bridged reports whether inst lives in an OS window the window host raises:
// it is the runtime's, and the runtime reports OSWindows.
func (c *Core) bridged(inst revier.Instance) bool {
	return c.Window != nil && c.Runtime != nil && inst.Ref.Host == c.Runtime.Name() && c.Runtime.Capabilities().OSWindows
}

// Focus activates a ref on the host that produced it and raises the OS window
// a terminal of it lives in. The picker uses it for an attached instance,
// which has no target to resolve: the ref is all revier knows about it, and
// since an attachment is the terminal as well as its window (decisions.md
// D95) that ref can be a terminal that cannot raise itself. A terminal whose
// window the window host does not list is not focused at all, as an
// activation of one is not (decisions.md D63).
func (c *Core) Focus(ctx context.Context, ref revier.TargetRef) error {
	snap, failed := c.listing(ctx)
	inst, ok := byRef(snap, ref)
	if !ok {
		return c.focus(ctx, ref)
	}
	name := revier.TargetName(ref.Title)
	// A window host that did not answer lists no window for anything, so the
	// refusal below would tell the user the window is gone. What went wrong
	// is named instead, as every refusal a host's silence causes is
	// (decisions.md D89).
	if c.bridged(inst) {
		if err := failed[c.Window.Name()]; err != nil {
			return fmt.Errorf("%s: %s: %w", c.Window.Name(), name, err)
		}
	}
	osw, err := c.raisable(snap, inst, name)
	if err != nil {
		return err
	}
	if err := c.focus(ctx, ref); err != nil {
		return err
	}
	return c.raise(ctx, osw, name)
}

// focus activates a bare ref on the host that produced it, and nothing else:
// the caller has already decided what raising it takes.
func (c *Core) focus(ctx context.Context, ref revier.TargetRef) error {
	h, ok := c.hostNamed(ref.Host)
	if !ok {
		return fmt.Errorf("%w: no host named %q", ErrNoHost, ref.Host)
	}
	return h.Focus(ctx, ref)
}

// hostNamed is the configured host of that name: the one that produced a ref.
func (c *Core) hostNamed(name string) (revier.Host, bool) {
	for _, h := range c.hosts() {
		if h.Name() == name {
			return h, true
		}
	}
	return nil, false
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
// pane" says nothing about OS focus: a tmux session can be current while the
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
		slog.Warn("toggle-back: focused, taken as not focused", "host", auth.Name(), "err", err)
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
	// Hosts are the hosts that listed. A host that could not is not here,
	// so nothing bound to it is taken as gone, and its failure is in Failed.
	Hosts []string
	// Failed is why each host that could not list is missing, by host name.
	// Its targets are in the views marked Unknown (decisions.md D89).
	Failed map[string]error
	// tags is where the runtime here shows each panel a linked host can name:
	// what places a host's agents, whenever its answer is laid over.
	tags map[revier.PanelID]shown
	// before is the state the survey started from: what Settle may judge. A
	// ref written since is to a window the listing may have missed.
	before *state.State
}

// HostErr is every failure a survey degraded over, joined, or nil: what a
// surface says beside the view it still has.
func (r Report) HostErr() error { return hostErrs(r.Failed).err() }

// Survey builds the view every renderer reads: one bulk listing per host, then
// local matching for every project, with what each linked host says laid over
// its projects. The ledger says where each project's targets last landed; a
// bound instance that is still listed is its target. It also says what was
// bound to each project by hand or by a claim; an attachment that is still
// listed follows the project's targets, marked Attached.
//
// Every project here is already rendered and compiled, so the survey does no
// work that a previous refresh did not also have to do. What could not be
// rendered or compiled arrives marked rather than missing: an invalid project
// and a refused target each carry their reason into the view, because the
// survey is the one place a user reads why something is not there.
//
// It answers when the slowest linked host has. A surface that refreshes takes
// the two parts apart instead, SurveyLocal and AskRemotes, and lays one over
// the other with Lay (decisions.md D114).
func (c *Core) Survey(ctx context.Context, projects []Project) (r Report, err error) {
	start := time.Now()
	defer func() {
		logging.Poll("survey", "survey", start, err, "projects", len(projects), "hosts", r.Hosts, "instances", len(r.Instances))
	}()
	// The remote hosts are asked while the local ones are listed and the
	// local agents probed: a round trip to another machine is the slow part,
	// and nothing local waits for it.
	remote := make(chan RemoteAnswers, 1)
	go func() { remote <- c.AskRemotes(ctx, projects) }()
	st := c.state()
	r = c.listed(ctx, projects, st, st.Attached)
	c.lay(&r, <-remote)
	return r, nil
}

// SurveyToClose is the survey a close draws its plan from: this machine
// listed, then only the linked hosts the close is about asked (decisions.md
// D115). A close of one project asks that project's host. A close of every
// project asks the hosts of the links with something open here: a link with
// nothing open has nothing a close could end, so its host is not waited for,
// and a host that is gone costs a close of something else nothing.
//
// An agent of a link in a panel of an instance the link no longer holds - its
// tab moved to another window - is therefore missing from a plan of every
// project. A close of its own project still finds it.
func (c *Core) SurveyToClose(ctx context.Context, projects []Project, only revier.ProjectName) Report {
	start := time.Now()
	r, answers := c.surveyAsking(ctx, projects, func(v revier.ProjectView) bool {
		if only != "" {
			return v.Project.Name == only
		}
		return v.Held()
	})
	logging.Op("shutdown survey", start, nil, "project", only, "asked", answers.hosts(), "unanswered", answers.failures())
	return r
}

// surveyAsking is a survey that asks only the hosts of the links ask picks
// from the listing of this machine, and returns what they said beside it.
// The other links keep the view of the window that reaches them, as
// SurveyLocal leaves it.
func (c *Core) surveyAsking(ctx context.Context, projects []Project, ask func(revier.ProjectView) bool) (Report, RemoteAnswers) {
	st := c.state()
	r := c.listed(ctx, projects, st, st.Attached)
	var links []Project
	for i, p := range projects {
		if p.Remote != nil && ask(r.Views[i]) {
			links = append(links, p)
		}
	}
	answers := c.AskRemotes(ctx, links)
	c.lay(&r, answers)
	return r, answers
}

// SurveyLocal is the part of a survey this machine answers by itself: the
// hosts here listed, every project matched, the agents here probed. A linked
// project's view is the one of the window that reaches it, with nothing of
// its host's yet. No linked host is asked, so it costs what the hosts here
// cost however a host elsewhere answers (decisions.md D114).
//
// It lists no attachment either: a surface lists them from the state Settle
// returns, which a claim changes between surveys, so a claimed window shows
// at once and not a refresh later.
//
// The TUI surveys every refresh, so a survey is logged only when it failed or
// was slow, and a failure that repeats only once (logging.Poll).
func (c *Core) SurveyLocal(ctx context.Context, projects []Project) (r Report, err error) {
	start := time.Now()
	defer func() {
		logging.Poll("local survey", "survey", start, err, "projects", len(projects), "hosts", r.Hosts, "instances", len(r.Instances))
	}()
	return c.listed(ctx, projects, c.state(), nil), nil
}

// Lay returns the report with what the linked hosts said laid over their
// projects. The report is left as it was, so answers that arrive later are
// laid over the same listing again.
func (c *Core) Lay(r Report, answers RemoteAnswers) Report {
	views := make([]revier.ProjectView, len(r.Views))
	copy(views, r.Views)
	for i := range views {
		// An agent appended into room the listing left would be written into
		// the listing's own slice.
		views[i].Agents = slices.Clip(views[i].Agents)
	}
	r.Views = views
	c.lay(&r, answers)
	return r
}

// lay lays each answer over the view of the link it is for, in place.
func (c *Core) lay(r *Report, answers RemoteAnswers) {
	for i := range r.Views {
		v := &r.Views[i]
		a, ok := answers.of(v.Project)
		if !ok {
			continue
		}
		at := len(v.Agents)
		c.localise(r.tags, merge(v, a))
		dropDoubles(v, at)
	}
}

// Unsurveyed is the view of every project before any host has answered:
// what the files alone say - the name, the path and whether it is here,
// each target and whether a host here could realize it - with nothing
// running and no agent. The TUI draws its first frame from it, so the names
// are on the screen while the hosts are listed. It lists nothing and asks nobody: with
// no listing there is no instance to bind to or to probe, so it takes no
// bindings and no context.
func (c *Core) Unsurveyed(projects []Project) []revier.ProjectView {
	views := make([]revier.ProjectView, 0, len(projects))
	probed := probeCache{}
	for _, p := range projects {
		views = append(views, c.view(context.Background(), nil, nil, p, nil, nil, probed))
	}
	return views
}

// listed is the local part of a survey, from the state st: its bindings, and
// the attachments the caller takes of it.
func (c *Core) listed(ctx context.Context, projects []Project, st *state.State, attached map[revier.ProjectName][]revier.TargetRef) Report {
	snap, failed := c.listing(ctx)
	bound := st.Bound
	r := Report{Views: make([]revier.ProjectView, 0, len(projects)), tags: c.tags(snap), before: st}
	if len(failed) > 0 {
		r.Failed = failed
	}
	probed := probeCache{}
	for _, p := range projects {
		r.Views = append(r.Views, c.view(ctx, snap, failed, p, bound[p.Name], attached[p.Name], probed))
	}
	for _, h := range c.hosts() {
		if failed[h.Name()] != nil {
			continue
		}
		r.Hosts = append(r.Hosts, h.Name())
		r.Instances = append(r.Instances, snap[h.Name()]...)
	}
	if c.Served != nil {
		r.Instances = append(r.Instances, snap[c.Served.Name()]...)
	}
	if c.Window != nil {
		r.Windows = snap[c.Window.Name()]
	}
	return r
}

// claim finds the window that is new since the previous survey and belongs to
// l, the last launch, which is of the project p. For a target's launch it is
// a binding: the window the target's rule accepts by class, which the
// activation may have missed because the window took longer than its wait.
// For an action's launch, which has no target, it is an attachment: a window
// no declared target of any project matches, since a declared one is reached
// by its key already. Either way it claims nothing rather than the wrong
// thing: nothing outside the window after the launch, and nothing when more
// than one candidate appeared at once.
func (c *Core) claim(before, after []revier.Instance, p Project, l *state.Launch, now time.Time, projects []Project) (revier.TargetRef, bool) {
	// A launch stamped after now was written after the listing was taken, so
	// no window in the listing is its.
	if !l.Pending(now) || l.At.After(now) {
		return revier.TargetRef{}, false
	}
	seen := map[string]bool{}
	for _, inst := range before {
		seen[key(inst.Ref)] = true
	}
	var candidates []revier.Instance
	for _, inst := range after {
		if !seen[key(inst.Ref)] && c.candidate(inst, p, l.Target, projects) {
			candidates = append(candidates, inst)
		}
	}
	if len(candidates) != 1 {
		return revier.TargetRef{}, false
	}
	return candidates[0].Ref, true
}

func (c *Core) candidate(inst revier.Instance, p Project, target revier.TargetName, projects []Project) bool {
	if target == "" {
		return !c.declared(inst, projects)
	}
	i, ok := p.index(target)
	if !ok || p.Targets[i].Window == nil {
		return false
	}
	return p.compiled[i].window.Matches(inst) || c.classOK(p, i, inst)
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

// elsewhere is the directory an agent works in as its project's view carries
// it: the directory when it is not the project's own, and nothing when it is
// (decisions.md D110). The two are compared where both are paths of one
// machine, the one that read the agent, and with symbolic links resolved: a
// project reached through a link and an agent that reports the physical
// directory are in the same place. A path that cannot be resolved is compared
// as written.
func elsewhere(project, dir string) string {
	if dir == "" || sameDir(project, dir) {
		return ""
	}
	return dir
}

func sameDir(a, b string) bool {
	if a, b = filepath.Clean(a), filepath.Clean(b); a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// probeCache is what the survey has already read of each instance, by ref.
// One terminal can be a target of one project and an attachment of another,
// and both views list its agents; reading it once per survey rather than once
// per view is what keeps the probe cost the instance count and not the
// project count.
type probeCache map[string][]revier.AgentView

// agentsOf is the instance's agents, probed on the first view that asks.
func (c *Core) agentsOf(ctx context.Context, probed probeCache, inst revier.Instance) []revier.AgentView {
	k := key(inst.Ref)
	if agents, ok := probed[k]; ok {
		return agents
	}
	agents := c.inspect(ctx, inst)
	probed[k] = agents
	return agents
}

func (c *Core) view(ctx context.Context, snap snapshot, failed hostErrs, p Project, bound Bindings, attached []revier.TargetRef, probed probeCache) revier.ProjectView {
	// A remote project's checkout and agents are its host's word, laid over
	// this view by merge; the path is in the host's terms, and the pane here
	// that reaches the project is not the agent in it.
	local := p.Remote == nil
	v := revier.ProjectView{Project: p.Project, PathExists: local && dirExists(p.Path)}
	if p.Invalid != nil {
		v.Invalid = p.Invalid.Error()
	}

	// Probe every matched instance, not only home. An agent is wherever the
	// user put it - a pane of the workspace, or a target of its own - and a
	// dashboard that only looked at home would miss exactly the agent that had
	// been given its own window. One instance can back two targets, or a
	// target and an attachment; listing it twice would report the same agent
	// twice, so each lands in a view once, and the read behind it is the
	// survey's, not this view's.
	seen := map[string]bool{}
	probe := func(inst revier.Instance) {
		if k := key(inst.Ref); !seen[k] {
			seen[k] = true
			for _, a := range c.agentsOf(ctx, probed, inst) {
				a.State.Dir = elsewhere(p.Path, a.State.Dir)
				v.Agents = append(v.Agents, a)
			}
		}
	}
	// A link's targets are panels running an ssh, and what the agent on the
	// far side is doing is its host's word, added by merge. Probing them
	// here would read the ssh.
	probeOnce := func(inst revier.Instance) {
		if local {
			probe(inst)
		}
	}

	for i, t := range p.Targets {
		tv := revier.TargetView{Name: t.Name, Key: t.Key}
		if p.isTab(i) {
			v.Targets = append(v.Targets, c.tabView(snap, failed, p, i, bound, tv))
			continue
		}
		// What this machine serves to a terminal elsewhere is probed whether
		// or not a host here can realize the target: a machine reached over
		// ssh alone has no runtime.
		if inst, ok := c.served(snap, p, i); ok {
			probeOnce(inst)
		}
		host, _, m, err := c.resolveAt(p, i)
		// A target no host here can realize is the expected headless result,
		// and Available already says it. A Reason is set only for the other
		// kind: a target its own configuration refused, which is a mistake in
		// a file and needs saying (decisions.md D85).
		if err != nil && !errors.Is(err, ErrNoHost) {
			tv.Reason = err.Error()
		}
		if err == nil {
			tv.Available = true
			tv.Host = host.Name()
			// The host could not list: whether the target is up is not
			// known, which is not the same as stopped.
			if ferr := failed[host.Name()]; ferr != nil {
				tv.Unknown = ferr.Error()
				v.Targets = append(v.Targets, tv)
				continue
			}
			if inst, found := c.locate(snap, p, i, host, m, bound[t.Name]); found {
				tv.Ref = inst.Ref
				if t.Home {
					v.Running, v.Home = true, inst.Ref
				}
				probeOnce(inst)
			}
		}
		v.Targets = append(v.Targets, tv)
	}
	// A gone attachment is left out rather than shown dead: the caller prunes
	// it from state against this same listing. A live one is probed like a
	// target, so a shutdown sees the agent in an attached terminal and keeps
	// it from ending unasked (decisions.md D78).
	//
	// A terminal attached by hand is recorded as the window and the terminal
	// inside it (decisions.md D95), and is one row here: the terminal's, the
	// side that holds the panels, so the agents of the row and the close that
	// ends them are the same instance's.
	//
	// The window is dropped by the pairing the attachment was recorded with,
	// read from the window's own side: runtimeOf refuses as soon as two
	// terminals answer to one window, and the terminal it names has to be
	// attached too. A window whose terminal cannot be named beyond doubt
	// keeps its row rather than being folded away against a guess.
	held := map[string]bool{}
	for _, ref := range attached {
		held[key(ref)] = true
	}
	var terminals []revier.Instance
	if c.Runtime != nil {
		terminals = snap[c.Runtime.Name()]
	}
	window := map[string]bool{}
	for _, ref := range attached {
		inst, ok := byRef(snap, ref)
		if !ok || c.Window == nil || ref.Host != c.Window.Name() {
			continue
		}
		if rt, ok := c.runtimeOf(terminals, inst); ok && held[key(rt.Ref)] {
			window[key(ref)] = true
		}
	}
	// An attachment is probed for a link too (decisions.md D101): it is a
	// terminal of this machine, and taking the host's word as the project's
	// whole answer ended an agent in it unasked. An agent both sides report
	// is dropped to one by dropDoubles, on the panel it landed on.
	for _, ref := range attached {
		if inst, ok := byRef(snap, ref); ok && !window[key(ref)] {
			v.Targets = append(v.Targets, revier.TargetView{Host: ref.Host, Ref: inst.Ref, Attached: true, Available: true})
			probe(inst)
		}
	}
	return v
}

// key is the identity of a ref within one survey.
func key(ref revier.TargetRef) string { return ref.Host + "\x00" + ref.ID }

// inspect runs the first matching probe over every panel of an instance. A
// probe's Match decides what an agent panel is, not the host's PanelKind: a
// host says shell or tool and names no harness (decisions.md D119). A shell
// in the foreground is the one thing that overrules a probe, as it does for
// `revier agent`: the marker a harness leaves outlives it in a --hold window.
func (c *Core) inspect(ctx context.Context, inst revier.Instance) []revier.AgentView {
	var out []revier.AgentView
	for _, panel := range inst.Panels {
		if probe, ok := c.agentProbe(panel); ok {
			out = append(out, revier.AgentView{Panel: panel.ID, Ref: inst.Ref, State: c.read(ctx, probe, inst.Ref, panel)})
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
//
// A failure is logged once per panel and cause. A panel id is unique
// only within its instance - a kitty window id within one kitty process - so
// the instance names the panel too.
func (c *Core) read(ctx context.Context, probe revier.AgentProbe, ref revier.TargetRef, panel revier.Panel) revier.AgentState {
	state, err := probe.Inspect(ctx, panel)
	logging.Repeat("probe\x00"+probe.Name()+"\x00"+key(ref)+"\x00"+string(panel.ID), "probe", err, "probe", probe.Name(), "ref", ref, "panel", panel.ID)
	if err != nil {
		return revier.AgentState{Harness: probe.Name(), Status: revier.StatusUnknown}
	}
	return state
}
