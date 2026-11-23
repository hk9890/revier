package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// An agent tab is one agent opened beside a workspace that is already open:
// the declared agent panel and the declared shell panel, opened as a tab of
// the instance. `revier agent new` opens one, and a restore opens one for
// every agent past the declared layout, through the same functions, so an
// agent opened by hand comes back the way it was opened.

var (
	// ErrNotOpen means the target has no instance to open an agent tab in.
	ErrNotOpen = errors.New("not open")

	// ErrNoPanel means no open target of any project holds the panel.
	ErrNoPanel = errors.New("no open target holds panel")

	// ErrNoTabsToOpen means the runtime cannot open a tab to add an agent or a
	// shell in.
	ErrNoTabsToOpen = errors.New("runtime has no tabs to open an agent or a shell in")

	// ErrUnknownAgent means no named agent of that name is declared: by the
	// project, or by config.toml for a key outside a workspace.
	ErrUnknownAgent = errors.New("no agent named")

	// ErrNoWorkspace means the runtime holds the panel, but no project's
	// open workspace does: a terminal window opened by hand.
	ErrNoWorkspace = errors.New("its window is no project's open workspace")
)

// agentTab is the tab an agent opens in: a copy of the first panel the
// workspace's layout declares as an agent, started as startAgent says, and a
// copy of the first declared shell panel beside it, in the same directory. A
// named agent's tab is that copy with the named agent's command, and its name
// as the title; a link's named agent is the host's, which the remote's panel
// command names to it. A layout with no agent panel has no tab to give, and
// the agent is dropped. A name the project does not declare has none either:
// another program in its place is not that agent.
func (c *Core) agentTab(p Project, layout []revier.PanelSpec, r Resume) (revier.Realization, AgentOutcome) {
	agent, ok := declared(layout, revier.PanelAgent)
	if !ok {
		return revier.Realization{}, AgentDropped
	}
	if r.Agent != "" {
		switch named, ok := p.namedAgent(r.Agent); {
		case p.Remote != nil:
			remote, err := c.remote(p.Remote.Host)
			if err != nil {
				return revier.Realization{}, AgentDropped
			}
			agent.Command = remote.PanelCommand(p.Remote.Project, revier.PanelAgent, r.Agent)
		case !ok:
			return revier.Realization{}, AgentUnknown
		default:
			agent.Command = named.Command
		}
		agent.Title = r.Agent
	}
	if r.Harness == "" {
		r.Harness = c.harnessOf(agent)
	}
	link := p.Remote != nil
	outcome := c.startAgent(&agent, r, link)
	tab := revier.Realization{Dir: agent.Dir, Panels: []revier.PanelSpec{agent}}
	if shell, ok := declared(layout, revier.PanelShell); ok {
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

// agentVars are the vars of an agent tab: a named agent's mark, and none for
// the default agent.
func agentVars(r Resume) map[string]string {
	if r.Agent == "" {
		return nil
	}
	return map[string]string{revier.PanelAgentVar: r.Agent}
}

// namedAgent is the project's named agent of that name.
func (p Project) namedAgent(name string) (NamedAgent, bool) {
	for _, a := range p.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return NamedAgent{}, false
}

// unknownAgent refuses a name the project does not declare, and names the
// ones it does.
func (p Project) unknownAgent(name string) error {
	if len(p.Agents) == 0 {
		return fmt.Errorf("%s: %w %q: the project declares no named agent", p.Name, ErrUnknownAgent, name)
	}
	known := make([]string, len(p.Agents))
	for i, a := range p.Agents {
		known[i] = a.Name
	}
	return fmt.Errorf("%s: %w %q: its agents are %s", p.Name, ErrUnknownAgent, name, strings.Join(known, ", "))
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
// what each agent comes to on host. A dropped agent has no tab: its
// workspace's layout declares no agent panel, or the runtime cannot open tabs.
// A launch opens the tabs and a dry run reports the outcomes, so the two
// cannot disagree.
//
// An agent recorded with a harness and no name comes back as the agent that
// runs that harness. The resumes returned carry its name, so its tab is
// marked as agent new marks it, and the next save records the name.
func (c *Core) agentTabs(host revier.Host, p Project, layout []revier.PanelSpec, resumes []Resume) ([]revier.Realization, []AgentOutcome, []Resume) {
	tabs := make([]revier.Realization, len(resumes))
	outcomes := make([]AgentOutcome, len(resumes))
	chosen := slices.Clone(resumes)
	_, opens := c.opener(host)
	for n, r := range resumes {
		if !opens {
			outcomes[n] = AgentDropped
			continue
		}
		r, ok := c.agentRunning(p, layout, r)
		if !ok {
			outcomes[n] = AgentUnknown
			continue
		}
		chosen[n] = r
		tabs[n], outcomes[n] = c.agentTab(p, layout, r)
	}
	return tabs, outcomes, chosen
}

// agentRunning is the agent a recorded agent with a harness and no name comes
// back as: the default agent when it runs that harness, else the first named
// agent that does. A command no probe claims - a wrapper such as
// `zsh -ilc "exec dtop"` - runs the harness its agent's name names. A default
// agent that is a wrapper can run any harness, so only a named agent whose
// name is the harness comes back in its place: an unmarked tab of the default
// agent comes back as the default, not as another agent of its harness. When
// no agent runs the harness, a default agent that is a wrapper is trusted, as
// startAgent trusts it; any other default agent runs another program, which
// is not the agent that ran. A link's agents are its
// host's to name.
func (c *Core) agentRunning(p Project, layout []revier.PanelSpec, r Resume) (Resume, bool) {
	agent, ok := declared(layout, revier.PanelAgent)
	if !ok || r.Agent != "" || r.Harness == "" || p.Remote != nil {
		return r, true
	}
	harness := c.harnessOf(agent)
	if c.runsHarness(agent, agent.Agent, r.Harness) {
		return r, true
	}
	for _, named := range p.Agents {
		spec := agent
		spec.Title, spec.Command = named.Name, named.Command
		if harness == "" && named.Name != r.Harness {
			continue
		}
		if c.runsHarness(spec, named.Name, r.Harness) {
			r.Agent = named.Name
			return r, true
		}
	}
	return r, harness == ""
}

// chosenAs is the named agent each agent recorded with no name comes back
// as, parallel to recorded: empty for one that keeps the name it was recorded
// with, or no name.
func chosenAs(recorded, chosen []Resume) []string {
	as := make([]string, len(recorded))
	for n, r := range recorded {
		if r.Agent == "" {
			as[n] = chosen[n].Agent
		}
	}
	return as
}

// runsHarness reports whether an agent of that name, whose panel is spec,
// runs the recorded harness: a save records the name of the probe that
// claimed the panel. With no probe to say, the name says.
func (c *Core) runsHarness(spec revier.PanelSpec, name, recorded string) bool {
	if probe, ok := c.probeFor(revier.Panel{Kind: spec.Kind, Title: spec.Title, Command: spec.Command}); ok {
		return probe.Name() == recorded
	}
	return name == recorded
}

// addAgents opens an agent tab in a workspace that has just opened, for each
// agent, in order, and then makes the panel that was current before them
// current again: a restore leaves the workspace as it opened, after a failed
// tab too. Under a view that draws the workspace only once its tabs run, no
// panel is current yet, and the launch makes its active tab current after
// the view draws it. A tab that fails stops the rest, which are named as not added, and
// the error is returned beside the outcomes: the workspace is open, so it is
// not the launch's failure. A failed OpenTab leaves no tab behind, so the
// agent it names is not running anywhere. as is the named agent each agent
// recorded with no name came back as, as chosenAs says.
func (c *Core) addAgents(ctx context.Context, host revier.Host, p Project, layout []revier.PanelSpec, ref revier.TargetRef, recorded []Resume) ([]AgentOutcome, []string, error) {
	tabs, outcomes, resumes := c.agentTabs(host, p, layout, recorded)
	as := chosenAs(recorded, resumes)
	opener, ok := c.launchOpener(host)
	if !ok {
		return outcomes, as, nil
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
				return notAdded(outcomes, n), as, fmt.Errorf("%s: agent tab: %w", host.Name(), err)
			}
			current, opened = cur, true
		}
		if _, err := opener.OpenTab(ctx, ref, tab, agentVars(resumes[n])); err != nil {
			outcomes, failed = notAdded(outcomes, n), fmt.Errorf("%s: agent tab: %w", host.Name(), err)
			break
		}
	}
	if opened && current != "" {
		if err := opener.FocusPanel(ctx, ref, current); err != nil {
			return outcomes, as, errors.Join(failed, fmt.Errorf("%s: focus the workspace after its agent tabs: %w", host.Name(), err))
		}
	}
	return outcomes, as, failed
}

// notAdded names the agent at n, and every later one that would have had a
// tab, as not added.
func notAdded(outcomes []AgentOutcome, n int) []AgentOutcome {
	for i := n; i < len(outcomes); i++ {
		if outcomes[i] != AgentDropped && outcomes[i] != AgentUnknown {
			outcomes[i] = AgentNotAdded
		}
	}
	return outcomes
}

// AgentTarget is the target of a project whose listed tabs declare an agent
// panel: the one `revier agent new -p <project>` opens its tab in. A
// workspace that lists two tabs with an agent panel is one target, and its
// agent is the one of the first of them.
func (c *Core) AgentTarget(p Project) (revier.TargetName, error) {
	var found []revier.TargetName
	for i, t := range p.Targets {
		if _, real, _, err := c.resolveAt(p, i); err == nil {
			if layout, err := c.layout(p, real); err == nil {
				if _, ok := declared(layout, revier.PanelAgent); ok {
					found = append(found, t.Name)
				}
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
	failed  hostErrs
	// panel is the runtime's id of the panel PanelOwner found the workspace
	// by, which is not the id a key passes where the view is not the runtime.
	panel revier.PanelID
	// beside says the id PanelOwner was handed is a window of a view that
	// opens tabs of its own beside its windows.
	beside bool
}

// AddressIn is the address of the panel a tab opened in the workspace w: the
// one the survey shows it by, whose target is the first of the project's
// that holds the workspace's instance.
func (c *Core) AddressIn(w Workspace, panel revier.PanelID) string {
	pl := c.places(w.snap, w.Project, c.state().Bound[w.Project.Name])
	return Address(w.Project.Name, pl.of(w.Ref, panel), panel)
}

// AgentWorkspace is the open instance of a project's target, for `revier
// agent new -p`. A listed tab that holds panels names the workspace it is
// inside: the tab is a part of that workspace's layout.
func (c *Core) AgentWorkspace(ctx context.Context, p Project, name revier.TargetName) (Workspace, error) {
	bound := c.state().Bound[p.Name]
	i, ok := p.index(name)
	if !ok {
		return Workspace{}, p.noTarget(name)
	}
	if t := p.Targets[i]; panelTab(p.Project, t) {
		name = t.Runtime.Inside
		i, _ = p.index(name)
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return Workspace{}, err
	}
	snap, failed := c.listing(ctx)
	if err := failed[host.Name()]; err != nil {
		return Workspace{}, err
	}
	inst, found := c.locate(snap, p, i, host, m, bound[name])
	if !found {
		return Workspace{}, fmt.Errorf("%s:%s: %w; revier go %s -p %s opens it", p.Name, name, ErrNotOpen, name, p.Name)
	}
	return Workspace{Project: p, Target: name, Ref: inst.Ref, snap: snap, failed: failed}, nil
}

// PanelOwner finds the project, target and open instance that hold a panel,
// which is how the kitty key that knows only the window it was pressed in
// reaches its workspace.
//
// A panel id can be one runtime process's - a kitty window id is - so the
// same id can name a window in each of two kitty processes. A runtime whose
// ids are like that says which instance the id means, as a PanelFinder; so
// does a view whose window ids are not the runtime's. Without one, and for an
// id that is no window of the view's, as an hc address a script passes, the
// id must be held by exactly one instance, and two are refused rather than
// guessed at: a guess opens the tab in a workspace the key was not pressed
// in.
//
// One instance can back two targets. The target is the first of them that
// declares an agent panel, because only that one has an agent tab to give; a
// shell tab opened through it takes that target's shell panel too.
//
// An instance that holds the panel but backs no project's target is
// ErrNoWorkspace, and the Workspace that comes with it holds only that
// instance: where PlaceTab opens a tab outside a workspace. So is a window
// of a view that opens tabs beside its own windows, which no instance holds,
// and its Workspace holds no instance.
func (c *Core) PanelOwner(ctx context.Context, projects []Project, panel revier.PanelID) (Workspace, error) {
	bound := c.state().Bound
	if c.Runtime == nil {
		return Workspace{}, fmt.Errorf("%w: no runtime holds panels", ErrNoHost)
	}
	snap, failed := c.listing(ctx)
	if err := failed[c.Runtime.Name()]; err != nil {
		return Workspace{}, err
	}
	instances := snap[c.Runtime.Name()]
	ref, own, viewed, err := c.findPanel(ctx, instances, panel)
	if err != nil {
		return Workspace{}, err
	}
	// A window of the view's own beside the runtime's gets a tab of the
	// view's, whatever instance the window is in.
	_, beside := c.View.(revier.BesideOpener)
	beside = beside && viewed
	if ref.IsZero() {
		if beside {
			return Workspace{snap: snap, failed: failed, beside: true}, fmt.Errorf("panel %s: %w", panel, ErrNoWorkspace)
		}
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
			w := Workspace{Project: p, Target: t.Name, Ref: ref, snap: snap, failed: failed, panel: own}
			layout, err := c.layout(p, real)
			if err != nil {
				continue
			}
			if _, ok := declared(layout, revier.PanelAgent); ok {
				return w, nil
			}
			if owner.Ref.IsZero() {
				owner = w
			}
		}
	}
	if owner.Ref.IsZero() {
		return Workspace{Ref: ref, snap: snap, failed: failed, panel: own, beside: beside}, fmt.Errorf("panel %s: %w", panel, ErrNoWorkspace)
	}
	return owner, nil
}

// findPanel is the instance that holds the panel id a terminal key or a
// script passes, the runtime's id of that panel, and whether the id named a
// window of the view's. The view's finder maps its own window ids; an id
// that is none of them, and every id with no finder, is the runtime's, held
// by at most one instance.
func (c *Core) findPanel(ctx context.Context, instances []revier.Instance, panel revier.PanelID) (ref revier.TargetRef, own revier.PanelID, viewed bool, err error) {
	if finder, ok := c.finder(); ok {
		ref, own, err = finder.FindPanel(ctx, instances, panel)
		if err == nil {
			return ref, own, true, nil
		}
		if !errors.Is(err, revier.ErrNoViewPanel) {
			return revier.TargetRef{}, "", false, fmt.Errorf("%s: find panel %s: %w", c.View.Name(), panel, err)
		}
	}
	var found []revier.TargetRef
	for _, inst := range instances {
		if holdsPanel(inst, panel) {
			found = append(found, inst.Ref)
		}
	}
	switch len(found) {
	case 0:
		return revier.TargetRef{}, panel, false, nil
	case 1:
		return found[0], panel, false, nil
	}
	return revier.TargetRef{}, "", false, fmt.Errorf("panel %s: %w: %d instances hold it", panel, ErrAmbiguous, len(found))
}

// Outside is what a terminal key does in a panel that no project's open
// workspace holds.
type Outside string

const (
	// OutsideTab opens a plain tab beside the panel: the runtime's own shell.
	OutsideTab Outside = "tab"
	// OutsideNone does nothing.
	OutsideNone Outside = "none"
	// OutsideAgent opens a tab beside the panel that runs the agent an agent
	// key asks for; a shell key opens the plain tab OutsideTab opens.
	OutsideAgent Outside = "agent"
)

// OutsideKey is what PlaceTab needs for a key pressed outside a workspace:
// what config says, the named agent the key asks for, empty for the default
// agent, and config.toml's named agents, their commands as written.
type OutsideKey struct {
	Outside Outside
	Agent   string
	Agents  []NamedAgent
}

// agent is the named agent a key outside a workspace runs: the one the key
// names, else config.toml's first. With none declared there is no agent to
// run, and the key opens the plain tab. A name config.toml does not declare is
// refused. A command that reads a project template is refused: outside a
// workspace there is no project to render it against.
// The panel's directory is no substitute: it would hand a program a path it
// never asked for.
func (o OutsideKey) agent() (NamedAgent, bool, error) {
	if o.Agent == "" && len(o.Agents) == 0 {
		return NamedAgent{}, false, nil
	}
	i := 0
	if o.Agent != "" {
		if i = slices.IndexFunc(o.Agents, func(a NamedAgent) bool { return a.Name == o.Agent }); i < 0 {
			return NamedAgent{}, false, fmt.Errorf("outside a workspace: %w %q in config.toml", ErrUnknownAgent, o.Agent)
		}
	}
	agent := o.Agents[i]
	if slices.ContainsFunc(agent.Command, readsTemplate) {
		return NamedAgent{}, false, fmt.Errorf("outside a workspace: agent %q: command %q reads a project template, and no project holds this window", agent.Name, agent.Command)
	}
	return agent, true, nil
}

// Refusal is why the key fails at every press outside a workspace, as agent
// refuses it, or nil: with outside "agent", the agent it runs there reads a
// project template, or config.toml does not declare it.
func (o OutsideKey) Refusal() error {
	if o.Outside != OutsideAgent {
		return nil
	}
	_, _, err := o.agent()
	return err
}

// panelDir is the directory of the panel in w's instance, or "" where the
// runtime reports none or it is no directory now: a worktree removed under
// the panel's shell.
func panelDir(w Workspace, panel revier.PanelID) string {
	inst, _ := byRef(w.snap, w.Ref)
	i := slices.IndexFunc(inst.Panels, func(p revier.Panel) bool { return p.ID == panel })
	if i < 0 || !dirExists(inst.Panels[i].Dir) {
		return ""
	}
	return inst.Panels[i].Dir
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
// opens, as a press refuses it: a focus with no raise is a GNOME "is ready"
// notice.
func (c *Core) NewAgent(ctx context.Context, w Workspace, r Resume) (AgentOutcome, error) {
	_, outcome, err := c.newAgent(ctx, w, r, true)
	return outcome, err
}

// AddAgent opens the tab NewAgent opens and leaves the keyboard where it is:
// what a script asks for, whose user is typing in another window. Nothing is
// focused and nothing is raised, so a workspace whose OS window cannot be
// raised takes the tab too; which tab that workspace shows afterwards is its
// view's (PanelFocuser). The panel is the new agent's, which is all the
// caller has to find it by.
func (c *Core) AddAgent(ctx context.Context, w Workspace, r Resume) (revier.PanelID, AgentOutcome, error) {
	return c.newAgent(ctx, w, r, false)
}

// newAgent records the tab it opened as one EventAgentNew, and the state
// record the tab failed or not. Both name the conversation the tab holds: one
// that was not resumed is not it.
func (c *Core) newAgent(ctx context.Context, w Workspace, r Resume, focus bool) (revier.PanelID, AgentOutcome, error) {
	panel, outcome, err := c.openAgent(ctx, w, r, focus)
	var resumed revier.SessionID
	if outcome == AgentResumed {
		resumed = r.Session
	}
	if err == nil {
		c.record(revier.Event{Kind: revier.EventAgentNew, Project: w.Project.Name, Target: w.Target, Agent: r.Agent, Dir: r.Dir, Session: resumed})
	}
	c.note(record.Line{
		Kind: record.KindAgentNew, Project: w.Project.Name, Target: w.Target, Panel: panel,
		Harness: r.Harness, Agent: r.Agent, Dir: r.Dir, Session: resumed,
	}.Failed(err))
	return panel, outcome, err
}

func (c *Core) openAgent(ctx context.Context, w Workspace, r Resume, focus bool) (revier.PanelID, AgentOutcome, error) {
	// A link's host resolves the name, so only a local project refuses it
	// here.
	if _, ok := w.Project.namedAgent(r.Agent); r.Agent != "" && !ok && w.Project.Remote == nil {
		return "", AgentNotAdded, w.Project.unknownAgent(r.Agent)
	}
	t, err := c.tabRuntime(w)
	if err != nil {
		return "", AgentNotAdded, err
	}
	tab, outcome := c.agentTab(w.Project, t.layout, r)
	if outcome == AgentDropped {
		return "", AgentNotAdded, fmt.Errorf("%s:%s: %w", w.Project.Name, w.Target, ErrNoAgent)
	}
	panel, err := c.openTab(ctx, w, t, tab, agentVars(r), "agent tab", focus)
	if panel == "" {
		return "", AgentNotAdded, err
	}
	return panel, outcome, err
}

// NewShell opens a shell tab in an open workspace, makes it current, and
// raises the OS window around it: a copy of the first shell panel its layout
// declares, or the runtime's own shell where it declares none, started in dir
// or else where the panel starts. A tab that opened is one EventShellNew; the
// state record has the tab failed or not.
func (c *Core) NewShell(ctx context.Context, w Workspace, dir string) error {
	t, err := c.tabRuntime(w)
	if err != nil {
		return err
	}
	shell, ok := declared(t.layout, revier.PanelShell)
	if !ok {
		shell = revier.PanelSpec{Kind: revier.PanelShell, Dir: t.real.Dir}
	}
	if dir != "" {
		shell.Dir = dir
	}
	_, err = c.openTab(ctx, w, t, revier.Realization{Dir: shell.Dir, Panels: []revier.PanelSpec{shell}}, nil, "shell tab", true)
	if err == nil {
		c.record(revier.Event{Kind: revier.EventShellNew, Project: w.Project.Name, Target: w.Target, Dir: dir})
	}
	c.note(record.Line{Kind: record.KindShellNew, Project: w.Project.Name, Target: w.Target, Dir: dir}.Failed(err))
	return err
}

// tabRuntime is the workspace's target as a tab opens in it: the runtime that
// holds it, which must open tabs, its rendered realization, and the layout
// its agent and shell panels are read from.
type tabRuntime struct {
	host   revier.Host
	opener panelOpener
	real   revier.Realization
	layout []revier.PanelSpec
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
	opener, ok := c.opener(host)
	if !ok {
		return tabRuntime{}, fmt.Errorf("%s: %w", host.Name(), ErrNoTabsToOpen)
	}
	layout, err := c.layout(w.Project, real)
	if err != nil {
		return tabRuntime{}, err
	}
	return tabRuntime{host: host, opener: opener, real: real, layout: layout}, nil
}

// openTab opens the tab, with vars on its panels, in the workspace and
// returns its first panel, which
// is empty while no tab is running, so an error after the tab opened is told
// from one before. With focus it refuses an OS window it cannot raise, then
// focuses the panel and raises the OS window; without, the focus stays where
// OpenTab left it. A tab of one panel whose command exited before its focus
// runs nothing, so its panel is empty and the error says the command exited.
func (c *Core) openTab(ctx context.Context, w Workspace, t tabRuntime, tab revier.Realization, vars map[string]string, what string, focus bool) (revier.PanelID, error) {
	host, opener := t.host, t.opener
	var osw revier.TargetRef
	if focus {
		inst, _ := byRef(w.snap, w.Ref)
		var err error
		if osw, err = c.raisableShown(ctx, w.snap, w.failed, inst, w.Target, t.real.Place); err != nil {
			return "", err
		}
	}
	panel, err := opener.OpenTab(ctx, w.Ref, tab, vars)
	if err != nil {
		return "", fmt.Errorf("%s: %s: %w", host.Name(), what, err)
	}
	if !focus {
		return panel, nil
	}
	if err := opener.FocusPanel(ctx, w.Ref, panel); err != nil {
		if ended(ctx, host, w.Ref, panel, tab.PanelSpecs()) {
			return "", fmt.Errorf("%s: %s: %w", host.Name(), what, panelstart.ExitedAtOnce(tab.PanelSpecs()[0]))
		}
		return panel, fmt.Errorf("%s: focus the %s: %w", host.Name(), what, err)
	}
	return panel, c.raise(ctx, osw, w.Target)
}
