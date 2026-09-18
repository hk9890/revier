package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// An agent tab is one agent opened beside a workspace that is already open:
// the declared agent panel and the declared shell panel, opened as a tab of
// the instance. `revier agent new` opens one, and a restore opens one for
// every agent past the declared layout, through the same functions, so an
// agent opened by hand comes back the way it was opened (decisions.md D65).

var (
	// ErrNotOpen means the target has no instance to open an agent tab in.
	ErrNotOpen = errors.New("not open")

	// ErrNoPanel means no open target of any project holds the panel.
	ErrNoPanel = errors.New("no open target holds panel")

	// ErrNoTabsToOpen means the runtime cannot open a tab to add an agent or a
	// shell in.
	ErrNoTabsToOpen = errors.New("runtime has no tabs to open an agent or a shell in")
)

// agentTab is the tab an agent opens in: a copy of the first panel the
// realization declares as an agent, started as startAgent says, and a copy of
// the first declared shell panel beside it, in the same directory. A
// realization with no agent panel has no tab to give, and the agent is
// dropped.
func (c *Core) agentTab(real revier.Realization, r Resume, link bool) (revier.Realization, AgentOutcome) {
	agent, ok := declared(real.Panels, revier.PanelAgent)
	if !ok {
		return revier.Realization{}, AgentDropped
	}
	if r.Harness == "" {
		r.Harness = c.harnessOf(agent)
	}
	outcome := c.startAgent(&agent, r, link)
	tab := revier.Realization{Dir: agent.Dir, Panels: []revier.PanelSpec{agent}}
	if shell, ok := declared(real.Panels, revier.PanelShell); ok {
		shell.Dir = agent.Dir
		if link {
			// The shell starts on the host, where the directory is: it
			// carries the same argument the agent's ssh does.
			shell.Command = append(append([]string(nil), shell.Command...), linkDir(r)...)
		}
		tab.Panels = append(tab.Panels, shell)
	}
	return tab, outcome
}

// harnessOf is the probe that claims what a declared agent panel runs: the
// harness a conversation asked for without one goes to, and the one a
// recorded conversation must match to be resumed in the panel. A command no
// probe claims - a wrapper - has none.
func (c *Core) harnessOf(spec revier.PanelSpec) string {
	if probe, ok := c.probeFor(revier.Panel{Kind: spec.Kind, Title: spec.Title, Command: spec.Command}); ok {
		return probe.Name()
	}
	return ""
}

// declared is the first panel of that kind in a layout.
func declared(layout []revier.PanelSpec, kind revier.PanelKind) (revier.PanelSpec, bool) {
	for _, p := range layout {
		if p.Kind == kind {
			return p, true
		}
	}
	return revier.PanelSpec{}, false
}

// agentTabs is the agent tab for each agent past the declared layout, and
// what each agent comes to on host. A dropped agent has no tab: its target
// declares no agent panel, or the runtime cannot open tabs. A launch opens
// the tabs and a dry run reports the outcomes, so the two cannot disagree.
func (c *Core) agentTabs(host revier.Host, real revier.Realization, resumes []Resume, link bool) ([]revier.Realization, []AgentOutcome) {
	tabs := make([]revier.Realization, len(resumes))
	outcomes := make([]AgentOutcome, len(resumes))
	_, opens := host.(revier.PanelOpener)
	for n, r := range resumes {
		if !opens {
			outcomes[n] = AgentDropped
			continue
		}
		tabs[n], outcomes[n] = c.agentTab(real, r, link)
	}
	return tabs, outcomes
}

// addAgents opens an agent tab in a workspace that has just opened, for each
// agent, in order, and then makes the panel that was current before them
// current again: a restore leaves the workspace as it opened, after a failed
// tab too. A tab that fails stops the rest, which are named as not added, and
// the error is returned beside the outcomes: the workspace is open, so it is
// not the launch's failure. A failed OpenTab leaves no tab behind, so the
// agent it names is not running anywhere.
func (c *Core) addAgents(ctx context.Context, host revier.Host, real revier.Realization, ref revier.TargetRef, resumes []Resume, link bool) ([]AgentOutcome, error) {
	tabs, outcomes := c.agentTabs(host, real, resumes, link)
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return outcomes, nil
	}
	var current revier.PanelID
	opened := false
	var failed error
	for n, tab := range tabs {
		if tab.Panels == nil {
			continue
		}
		if !opened {
			cur, err := opener.FocusedPanel(ctx, ref)
			if err != nil {
				return notAdded(outcomes, n), fmt.Errorf("%s: agent tab: %w", host.Name(), err)
			}
			current, opened = cur, true
		}
		if _, err := opener.OpenTab(ctx, ref, tab, nil); err != nil {
			outcomes, failed = notAdded(outcomes, n), fmt.Errorf("%s: agent tab: %w", host.Name(), err)
			break
		}
	}
	if opened && current != "" {
		if err := opener.FocusPanel(ctx, ref, current); err != nil {
			return outcomes, errors.Join(failed, fmt.Errorf("%s: focus the workspace after its agent tabs: %w", host.Name(), err))
		}
	}
	return outcomes, failed
}

// notAdded names the agent at n, and every later one that would have had a
// tab, as not added.
func notAdded(outcomes []AgentOutcome, n int) []AgentOutcome {
	for i := n; i < len(outcomes); i++ {
		if outcomes[i] != AgentDropped {
			outcomes[i] = AgentNotAdded
		}
	}
	return outcomes
}

// AgentTarget is the target of a project whose realization declares an agent
// panel: the one `revier agent new -p <project>` opens its tab in.
func (c *Core) AgentTarget(p Project) (revier.TargetName, error) {
	var found []revier.TargetName
	for i, t := range p.Targets {
		if _, real, _, err := c.resolveAt(p, i); err == nil {
			if _, ok := declared(real.Panels, revier.PanelAgent); ok {
				found = append(found, t.Name)
			}
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%s: no target declares an agent panel", p.Name)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%s: targets %v declare an agent panel; name one as %s:<target>", p.Name, found, p.Name)
}

// Workspace is the open instance of a project's target that `revier agent
// new` opens its tab in, with the listing it was found in: the tab is raised
// from that listing, so one key press lists the hosts once.
type Workspace struct {
	Project Project
	Target  revier.TargetName
	Ref     revier.TargetRef
	snap    snapshot
}

// TabIn is the open workspace a tab for a project's target opens in. An empty
// target is the one pick chooses.
func (c *Core) TabIn(ctx context.Context, p Project, target revier.TargetName, bound Bindings, pick func(Project) (revier.TargetName, error)) (Workspace, error) {
	if target == "" {
		var err error
		if target, err = pick(p); err != nil {
			return Workspace{}, err
		}
	}
	return c.AgentWorkspace(ctx, p, target, bound)
}

// AgentWorkspace is the open instance of a project's target, for `revier
// agent new -p`.
func (c *Core) AgentWorkspace(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (Workspace, error) {
	i, ok := p.index(name)
	if !ok {
		return Workspace{}, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return Workspace{}, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Workspace{}, err
	}
	inst, found := c.locate(snap, p, i, host, m, bound[name])
	if !found {
		return Workspace{}, fmt.Errorf("%s:%s: %w", p.Name, name, ErrNotOpen)
	}
	return Workspace{Project: p, Target: name, Ref: inst.Ref, snap: snap}, nil
}

// PanelOwner finds the project, target and open instance that hold a panel,
// which is how the kitty key that knows only the window it was pressed in
// reaches its workspace.
//
// A panel id can be one runtime process's - a kitty window id is - so the
// same id can name a window in each of two kitty processes. A runtime whose
// ids are like that says which instance the id means, as a PanelFinder.
// Without one, the id must be held by exactly one instance, and two are
// refused rather than guessed at: a guess opens the tab in a workspace the key
// was not pressed in.
//
// One instance can back two targets. The target is the first of them that
// declares an agent panel, because only that one has an agent tab to give; a
// shell tab opened through it takes that target's shell panel too.
func (c *Core) PanelOwner(ctx context.Context, projects []Project, bound map[revier.ProjectName]Bindings, panel revier.PanelID) (Workspace, error) {
	if c.Runtime == nil {
		return Workspace{}, fmt.Errorf("%w: no runtime holds panels", ErrNoHost)
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Workspace{}, err
	}
	instances := snap[c.Runtime.Name()]
	var ref revier.TargetRef
	if finder, ok := c.Runtime.(revier.PanelFinder); ok {
		if ref, err = finder.FindPanel(instances, panel); err != nil {
			return Workspace{}, fmt.Errorf("%s: find panel %s: %w", c.Runtime.Name(), panel, err)
		}
	} else {
		var found []revier.TargetRef
		for _, inst := range instances {
			if holdsPanel(inst, panel) {
				found = append(found, inst.Ref)
			}
		}
		if len(found) > 1 {
			return Workspace{}, fmt.Errorf("panel %s: %w: %d instances hold it", panel, ErrAmbiguous, len(found))
		}
		if len(found) == 1 {
			ref = found[0]
		}
	}
	if ref.IsZero() {
		return Workspace{}, fmt.Errorf("%w %s", ErrNoPanel, panel)
	}
	var owner Workspace
	for _, p := range projects {
		for i, t := range p.Targets {
			host, real, m, err := c.resolveAt(p, i)
			if err != nil {
				continue
			}
			if inst, ok := c.locate(snap, p, i, host, m, bound[p.Name][t.Name]); !ok || key(inst.Ref) != key(ref) {
				continue
			}
			w := Workspace{Project: p, Target: t.Name, Ref: ref, snap: snap}
			if _, ok := declared(real.Panels, revier.PanelAgent); ok {
				return w, nil
			}
			if owner.Ref.IsZero() {
				owner = w
			}
		}
	}
	if owner.Ref.IsZero() {
		return Workspace{}, fmt.Errorf("panel %s: its window is no project's open workspace", panel)
	}
	return owner, nil
}

// holdsPanel reports whether one of the instance's panels has the id.
func holdsPanel(inst revier.Instance, panel revier.PanelID) bool {
	for _, p := range inst.Panels {
		if p.ID == panel {
			return true
		}
	}
	return false
}

// NewAgent opens an agent tab in an open workspace, makes the new agent
// current in it, and raises the OS window around it: what a key pressed in a
// workspace asks for. The agent starts as a restored one would, from r. An
// error before the tab opened comes with AgentNotAdded, and one after it with
// what the agent in the open tab came to.
//
// An OS window the window host does not list is refused before the tab
// opens, as Go refuses it: a focus with no raise is a GNOME "is ready" notice
// (decisions.md D63).
func (c *Core) NewAgent(ctx context.Context, w Workspace, r Resume) (AgentOutcome, error) {
	t, err := c.tabRuntime(w)
	if err != nil {
		return AgentNotAdded, err
	}
	tab, outcome := c.agentTab(t.real, r, w.Project.Remote != nil)
	if outcome == AgentDropped {
		return AgentNotAdded, fmt.Errorf("%s:%s: %w", w.Project.Name, w.Target, ErrNoAgent)
	}
	opened, err := c.openTab(ctx, w, t, tab, "agent tab")
	if !opened {
		return AgentNotAdded, err
	}
	return outcome, err
}

// NewShell opens a shell tab in an open workspace, makes it current, and
// raises the OS window around it: a copy of the first shell panel the target
// declares, or the runtime's own shell where it declares none, started in dir
// or else where the panel starts.
func (c *Core) NewShell(ctx context.Context, w Workspace, dir string) error {
	t, err := c.tabRuntime(w)
	if err != nil {
		return err
	}
	shell, ok := declared(t.real.Panels, revier.PanelShell)
	if !ok {
		shell = revier.PanelSpec{Kind: revier.PanelShell, Dir: t.real.Dir}
	}
	if dir != "" {
		shell.Dir = dir
	}
	_, err = c.openTab(ctx, w, t, revier.Realization{Dir: shell.Dir, Panels: []revier.PanelSpec{shell}}, "shell tab")
	return err
}

// tabRuntime is the workspace's target as a tab opens in it: the runtime that
// holds it, which must open tabs, and its rendered realization.
type tabRuntime struct {
	host   revier.Host
	opener revier.PanelOpener
	real   revier.Realization
}

func (c *Core) tabRuntime(w Workspace) (tabRuntime, error) {
	i, ok := w.Project.index(w.Target)
	if !ok {
		return tabRuntime{}, fmt.Errorf("%w: %s", ErrNoTarget, w.Target)
	}
	host, real, _, err := c.resolveAt(w.Project, i)
	if err != nil {
		return tabRuntime{}, err
	}
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return tabRuntime{}, fmt.Errorf("%s: %w", host.Name(), ErrNoTabsToOpen)
	}
	return tabRuntime{host: host, opener: opener, real: real}, nil
}

// openTab refuses an OS window it cannot raise, then opens the tab in the
// workspace, focuses its first panel and raises the OS window. opened reports
// whether the tab is running, so an error after it is told from one before.
func (c *Core) openTab(ctx context.Context, w Workspace, t tabRuntime, tab revier.Realization, what string) (opened bool, err error) {
	host, opener := t.host, t.opener
	inst, _ := byRef(w.snap, w.Ref)
	osw, err := c.raisable(w.snap, inst, w.Target)
	if err != nil {
		return false, err
	}
	panel, err := opener.OpenTab(ctx, w.Ref, tab, nil)
	if err != nil {
		return false, fmt.Errorf("%s: %s: %w", host.Name(), what, err)
	}
	if err := opener.FocusPanel(ctx, w.Ref, panel); err != nil {
		return true, fmt.Errorf("%s: focus the %s: %w", host.Name(), what, err)
	}
	return true, c.raise(ctx, osw, w.Target)
}
