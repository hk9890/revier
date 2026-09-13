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

	// ErrNoAgentTabs means the runtime cannot open a tab to add an agent in.
	ErrNoAgentTabs = errors.New("runtime has no tabs to open an agent in")
)

// agentTab is the tab an agent opens in: a copy of the first panel the
// realization declares as an agent, started as startAgent says, and a copy of
// the first declared shell panel beside it, in the same directory. A
// realization with no agent panel has no tab to give, and the agent is
// dropped.
func (c *Core) agentTab(real revier.Realization, r Resume) (revier.Realization, AgentOutcome) {
	agent, ok := declared(real.Panels, revier.PanelAgent)
	if !ok {
		return revier.Realization{}, AgentDropped
	}
	if r.Harness == "" {
		r.Harness = c.harnessOf(agent)
	}
	outcome := c.startAgent(&agent, r)
	tab := revier.Realization{Dir: agent.Dir, Panels: []revier.PanelSpec{agent}}
	if shell, ok := declared(real.Panels, revier.PanelShell); ok {
		shell.Dir = agent.Dir
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
func (c *Core) agentTabs(host revier.Host, real revier.Realization, resumes []Resume) ([]revier.Realization, []AgentOutcome) {
	tabs := make([]revier.Realization, len(resumes))
	outcomes := make([]AgentOutcome, len(resumes))
	_, opens := host.(revier.PanelOpener)
	for n, r := range resumes {
		if !opens {
			outcomes[n] = AgentDropped
			continue
		}
		tabs[n], outcomes[n] = c.agentTab(real, r)
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
func (c *Core) addAgents(ctx context.Context, host revier.Host, real revier.Realization, ref revier.TargetRef, resumes []Resume) ([]AgentOutcome, error) {
	tabs, outcomes := c.agentTabs(host, real, resumes)
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
// declares an agent panel, because only that one has a tab to give.
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
	i, ok := w.Project.index(w.Target)
	if !ok {
		return AgentNotAdded, fmt.Errorf("%w: %s", ErrNoTarget, w.Target)
	}
	host, real, _, err := c.resolveAt(w.Project, i)
	if err != nil {
		return AgentNotAdded, err
	}
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return AgentNotAdded, fmt.Errorf("%s: %w", host.Name(), ErrNoAgentTabs)
	}
	tab, outcome := c.agentTab(real, r)
	if outcome == AgentDropped {
		return AgentNotAdded, fmt.Errorf("%s:%s: %w", w.Project.Name, w.Target, ErrNoAgent)
	}
	inst, _ := byRef(w.snap, w.Ref)
	osw, err := c.raisable(w.snap, inst, w.Target)
	if err != nil {
		return AgentNotAdded, err
	}
	panel, err := opener.OpenTab(ctx, w.Ref, tab, nil)
	if err != nil {
		return AgentNotAdded, fmt.Errorf("%s: agent tab: %w", host.Name(), err)
	}
	if err := opener.FocusPanel(ctx, w.Ref, panel); err != nil {
		return outcome, fmt.Errorf("%s: focus the agent tab: %w", host.Name(), err)
	}
	return outcome, c.raise(ctx, osw, w.Target)
}
