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
	if agent.Dir == "" {
		agent.Dir = real.Dir
	}
	tab := revier.Realization{Dir: agent.Dir, Panels: []revier.PanelSpec{agent}}
	if shell, ok := declared(real.Panels, revier.PanelShell); ok {
		shell.Dir = agent.Dir
		tab.Panels = append(tab.Panels, shell)
	}
	return tab, outcome
}

// harnessOf is the probe that claims what a declared agent panel runs, for a
// conversation asked for without its harness: `revier agent new --resume`.
// Without it, an opencode panel would be started on Claude Code's resume flag.
// A command no probe claims - a wrapper - is left to the first probe that can
// resume.
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
// current again: a restore leaves the workspace as it opened. A tab that
// fails stops the rest, which are named as not added, and the error is
// returned beside the outcomes: the workspace is open, so it is not the
// launch's failure.
func (c *Core) addAgents(ctx context.Context, host revier.Host, real revier.Realization, ref revier.TargetRef, resumes []Resume) ([]AgentOutcome, error) {
	tabs, outcomes := c.agentTabs(host, real, resumes)
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return outcomes, nil
	}
	var current revier.PanelID
	opened := false
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
			return notAdded(outcomes, n), fmt.Errorf("%s: agent tab: %w", host.Name(), err)
		}
	}
	if opened && current != "" {
		if err := opener.FocusPanel(ctx, ref, current); err != nil {
			return outcomes, fmt.Errorf("%s: focus the workspace after its agent tabs: %w", host.Name(), err)
		}
	}
	return outcomes, nil
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

// AgentWorkspace is the open instance of a project's target, for `revier
// agent new -p`.
func (c *Core) AgentWorkspace(ctx context.Context, p Project, name revier.TargetName, bound Bindings) (revier.TargetRef, error) {
	i, ok := p.index(name)
	if !ok {
		return revier.TargetRef{}, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return revier.TargetRef{}, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return revier.TargetRef{}, err
	}
	inst, found := c.locate(snap, p, i, host, m, bound[name])
	if !found {
		return revier.TargetRef{}, fmt.Errorf("%s:%s: %w", p.Name, name, ErrNotOpen)
	}
	return inst.Ref, nil
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
func (c *Core) PanelOwner(ctx context.Context, projects []Project, bound map[revier.ProjectName]Bindings, panel revier.PanelID) (Project, revier.TargetName, revier.TargetRef, error) {
	if c.Runtime == nil {
		return Project{}, "", revier.TargetRef{}, fmt.Errorf("%w: no runtime holds panels", ErrNoHost)
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Project{}, "", revier.TargetRef{}, err
	}
	var ref revier.TargetRef
	if finder, ok := c.Runtime.(revier.PanelFinder); ok {
		if ref, err = finder.FindPanel(ctx, panel); err != nil {
			return Project{}, "", revier.TargetRef{}, fmt.Errorf("%s: find panel %s: %w", c.Runtime.Name(), panel, err)
		}
	} else {
		var found []revier.TargetRef
		for _, inst := range snap[c.Runtime.Name()] {
			if holds(inst, panel) {
				found = append(found, inst.Ref)
			}
		}
		if len(found) > 1 {
			return Project{}, "", revier.TargetRef{}, fmt.Errorf("panel %s: %w: %d instances hold it", panel, ErrAmbiguous, len(found))
		}
		if len(found) == 1 {
			ref = found[0]
		}
	}
	if ref.IsZero() {
		return Project{}, "", revier.TargetRef{}, fmt.Errorf("%w %s", ErrNoPanel, panel)
	}
	for _, p := range projects {
		for _, h := range c.running(snap, p, bound[p.Name], "") {
			if key(h.inst.Ref) == key(ref) {
				return p, h.target, ref, nil
			}
		}
	}
	return Project{}, "", revier.TargetRef{}, fmt.Errorf("panel %s: its window is no project's open workspace", panel)
}
func holds(inst revier.Instance, panel revier.PanelID) bool {
	for _, p := range inst.Panels {
		if p.ID == panel {
			return true
		}
	}
	return false
}

// NewAgent opens an agent tab in the open instance of a project's target and
// makes the new agent current in it: what a key pressed in a workspace asks
// for. The agent starts as a restored one would, from r. An error comes with
// AgentNotAdded.
func (c *Core) NewAgent(ctx context.Context, p Project, name revier.TargetName, ref revier.TargetRef, r Resume) (AgentOutcome, error) {
	i, ok := p.index(name)
	if !ok {
		return AgentNotAdded, fmt.Errorf("%w: %s", ErrNoTarget, name)
	}
	host, real, _, err := c.resolveAt(p, i)
	if err != nil {
		return AgentNotAdded, err
	}
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return AgentNotAdded, fmt.Errorf("%s: %w", host.Name(), ErrNoAgentTabs)
	}
	tab, outcome := c.agentTab(real, r)
	if outcome == AgentDropped {
		return AgentNotAdded, fmt.Errorf("%s:%s: %w", p.Name, name, ErrNoAgent)
	}
	panel, err := opener.OpenTab(ctx, ref, tab, nil)
	if err != nil {
		return AgentNotAdded, fmt.Errorf("%s: agent tab: %w", host.Name(), err)
	}
	if err := opener.FocusPanel(ctx, ref, panel); err != nil {
		return AgentNotAdded, fmt.Errorf("%s: focus the agent tab: %w", host.Name(), err)
	}
	return outcome, nil
}
