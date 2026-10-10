package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoTabs is returned for a target declared inside another on a runtime
// that cannot open a tab. It is a configuration the machine cannot serve, so
// the keypress says which runtime and which target, rather than opening a
// window the user did not ask for.
var ErrNoTabs = errors.New("cannot open a tab inside another target")

// errTab is resolveAt's refusal of a tab: it is reached through the target it
// is inside, never matched on its own.
var errTab = errors.New("a tab has no instance of its own")

// PanelTargetVar is the panel variable that names the target a tab was
// opened for. It is the tab's whole identity: a title is the program's to
// change, and a position is not an identity. Every panel of the tab carries
// it, so the tab is found while any of them is open (decisions.md D64).
const PanelTargetVar = "revier_target"

// PanelHomeVar is the panel variable that names the target an instance was
// opened for. It marks the panels of the instance's own tab, and the first of
// them is the one a press that returns home from a tab lands in, so that panel
// is identified rather than guessed at (decisions.md D100). In an instance
// that opens with its tabs they are the panels of the active tab.
const PanelHomeVar = "revier_home"

// isTab reports whether the i-th target is a tab inside another target.
func (p Project) isTab(i int) bool { return tabTarget(p.Targets[i]) }

// tabTarget reports whether t is a tab inside another target.
func tabTarget(t revier.Target) bool { return t.Runtime != nil && t.Runtime.Inside != "" }

// tabsHost is the runtime that opens the tabs the realization lists, and nil
// when it lists none. A runtime that cannot open a tab refuses the target at
// the keypress, as it refuses a tab.
func (c *Core) tabsHost(name revier.TargetName, real revier.Realization, host revier.Host) (revier.PanelOpener, error) {
	if len(real.Tabs) == 0 {
		return nil, nil
	}
	opener, ok := host.(revier.PanelOpener)
	if !ok {
		return nil, fmt.Errorf("%w: target %q lists tabs, and the %s runtime has no tabs; to open it without them, remove tabs and give it a launch",
			ErrNoTabs, name, host.Name())
	}
	return opener, nil
}

// opening is how a new instance of a target that lists its tabs comes up
// (decisions.md D126): the tabs in the order they open, and the one that has
// the focus afterwards.
type opening struct {
	target revier.TargetName
	tabs   []revier.Target
	active revier.TargetName
	// laid counts the recorded agents laid over each tab and the tabs before
	// it, so a tab that fails leaves the agents of the opened ones as they
	// started.
	laid []int
}

// listed is the tab targets a realization lists, in its order, without the
// ones this project refuses.
func (p Project) listed(real revier.Realization) []revier.Target {
	var tabs []revier.Target
	for _, entry := range real.Tabs {
		if i, ok := p.index(entry); ok && p.compiled[i].err == nil {
			tabs = append(tabs, p.Targets[i])
		}
	}
	return tabs
}

// listedTabs is the listed tabs as this machine opens them: the panels of a
// link's tabs carry the argv that reaches the host (linkPanels). Every reader
// of a listed tab that starts a panel here takes it from this one place, so a
// launch, an agent tab, a shell tab and a restore see one argv. The tabs are
// copied before one is filled: they arrive sharing the prepared project's
// realizations.
func (c *Core) listedTabs(p Project, real revier.Realization) ([]revier.Target, error) {
	tabs := p.listed(real)
	if p.Remote == nil {
		return tabs, nil
	}
	tabs = slices.Clone(tabs)
	for i := range tabs {
		filled, err := c.linkPanels(p, *tabs[i].Runtime)
		if err != nil {
			return nil, err
		}
		tabs[i].Runtime = &filled
	}
	return tabs, nil
}

// layout is the panels a workspace declares, which is where its agent panel
// and its shell panel are read from: the panels of the tabs it lists, in
// their order (decisions.md D128). So the first panel of a kind is the one of
// the first listed tab that declares it, and a target that lists no tab has
// no layout: panels are on a tab alone. Every reader of a workspace's
// declared panels that starts one here takes them from here: an agent tab, a
// shell tab, a restore.
func (c *Core) layout(p Project, real revier.Realization) ([]revier.PanelSpec, error) {
	tabs, err := c.listedTabs(p, real)
	if err != nil {
		return nil, err
	}
	return panelsOf(tabs), nil
}

// layout is the workspace's layout as its file declares it, with no argv of a
// link filled in: what a panel served to a link is read from, on the host,
// where the project is not a link.
func (p Project) layout(real revier.Realization) []revier.PanelSpec {
	return panelsOf(p.listed(real))
}

// panelsOf is the panels of the tabs, in the order of the tabs.
func panelsOf(tabs []revier.Target) []revier.PanelSpec {
	var panels []revier.PanelSpec
	for _, tab := range tabs {
		panels = append(panels, tab.Runtime.Panels...)
	}
	return panels
}

// listedTab reports whether the target it is inside lists the tab t.
func listedTab(p revier.Project, t revier.Target) bool {
	in, ok := p.Target(t.Runtime.Inside)
	return ok && in.Runtime != nil && slices.Contains(in.Runtime.Tabs, t.Name)
}

// panelTab reports whether t is a listed tab that holds panels: a part of its
// workspace's layout (decisions.md D128), and not a tab that runs a launch.
func panelTab(p revier.Project, t revier.Target) bool {
	return tabTarget(t) && len(t.Runtime.Panels) > 0 && listedTab(p, t)
}

// resumingTabs is the opening with the recorded agents laid over the agent
// panels of its tabs, in the order of the tabs, what became of each, and the
// recorded agents past them, which the launch adds once the instance is open.
// The tabs are copied before a resume is written into one: they arrive sharing
// the prepared project's realizations, and a restore must not edit the project
// every later keypress reads.
func (c *Core) resumingTabs(o opening, resumes []Resume, link bool) (opening, []AgentOutcome, []Resume) {
	o.tabs = slices.Clone(o.tabs)
	o.laid = make([]int, len(o.tabs))
	var outcomes []AgentOutcome
	for i := range o.tabs {
		real := *o.tabs[i].Runtime
		var laid []AgentOutcome
		real.Panels, laid, resumes = c.layAgents(real.Panels, resumes, link)
		o.tabs[i].Runtime = &real
		outcomes = append(outcomes, laid...)
		o.laid[i] = len(outcomes)
	}
	return o, outcomes, resumes
}

// opening is the tabs a new instance of the named target opens with. An entry
// that is refused in this project is skipped. The active tab is the declared
// one, and the first that opens when none is declared or the declared one is
// skipped.
func (c *Core) opening(p Project, name revier.TargetName, real revier.Realization) (opening, error) {
	tabs, err := c.listedTabs(p, real)
	if err != nil {
		return opening{}, err
	}
	o := opening{target: name, tabs: tabs}
	if len(o.tabs) == 0 {
		return o, fmt.Errorf("target %q: every tab it lists is refused, so it has nothing to open", name)
	}
	o.active = o.tabs[0].Name
	if slices.ContainsFunc(o.tabs, func(t revier.Target) bool { return t.Name == real.Active }) {
		o.active = real.Active
	}
	return o, nil
}

// vars is the mark on the panels of a tab: the tab's own, and on the active
// tab the home mark beside it, so a return home lands there
// (decisions.md D100).
func (o opening) vars(tab revier.TargetName) map[string]string {
	vars := map[string]string{PanelTargetVar: string(tab)}
	if tab == o.active {
		vars[PanelHomeVar] = string(o.target)
	}
	return vars
}

// first is the realization that creates the instance: the target's own name,
// match and place, around what the first tab runs, under that tab's mark.
func (o opening) first(real revier.Realization) revier.Realization {
	tab := o.tabs[0]
	real.Launch, real.Panels, real.Dir = tab.Runtime.Launch, tab.Runtime.Panels, tab.Runtime.Dir
	real.Vars = o.vars(tab.Name)
	return real
}

// rest opens every tab after the first in the instance, in order, and stops
// at one that fails. It returns the tabs the instance now holds, the first
// included, and the first panel of the active tab when that is one it opened.
func (o opening) rest(ctx context.Context, opener revier.PanelOpener, ref revier.TargetRef) (opened []revier.TargetName, active revier.PanelID, err error) {
	opened = []revier.TargetName{o.tabs[0].Name}
	for _, tab := range o.tabs[1:] {
		panel, err := opener.OpenTab(ctx, ref, *tab.Runtime, o.vars(tab.Name))
		if err != nil {
			return opened, "", fmt.Errorf("open tab %s of %s: %w", tab.Name, o.target, err)
		}
		opened = append(opened, tab.Name)
		if tab.Name == o.active {
			active = panel
		}
	}
	return opened, active, nil
}

// focusActive makes the first panel of the active tab current. panel is that
// panel when OpenTab returned it. The tab that created the instance has only
// the instance's ref, so the instance is listed once and the panel found
// there.
func (c *Core) focusActive(ctx context.Context, opener revier.PanelOpener, o opening, ref revier.TargetRef, panel revier.PanelID) error {
	if panel == "" {
		snap, err := c.answered(ctx, ref.Host)
		if err != nil {
			return err
		}
		in, _ := byRef(snap, ref)
		found, ok := createdTab(in, o.active)
		if !ok {
			return fmt.Errorf("%s: %s opened with tab %s, and the listing has no panel for it", ref.Host, o.target, o.active)
		}
		panel = found
	}
	if err := opener.FocusPanel(ctx, ref, panel); err != nil {
		return fmt.Errorf("%s: focus tab %s of %s: %w", ref.Host, o.active, o.target, err)
	}
	return nil
}

// createdTab is the first panel of the tab that created the instance. A
// runtime sets the mark of Open as best effort, so the first panel can be
// without it while a later one has it: the tab of the marked one is taken
// from its start. A tab that lost every mark is the first panel no mark
// names: every tab opened after it carries one, or it would not have opened.
func createdTab(in revier.Instance, name revier.TargetName) (revier.PanelID, bool) {
	if id, ok := tabOf(in, name); ok {
		return tabAt(in, id)[0], true
	}
	for _, panel := range in.Panels {
		if panel.Vars[PanelTargetVar] == "" && panel.Vars[PanelHomeVar] == "" {
			return panel.ID, true
		}
	}
	return "", false
}

// tabHost is the runtime that opens the i-th target's tab, when there is one
// that can.
func (c *Core) tabHost(p Project, i int) (revier.PanelOpener, error) {
	t := p.Targets[i]
	if c.Runtime == nil {
		return nil, fmt.Errorf("%w: target %q is a tab, and no runtime is configured", ErrNoHost, t.Name)
	}
	opener, ok := c.Runtime.(revier.PanelOpener)
	if !ok {
		return nil, fmt.Errorf("%w: target %q is inside %q, and the %s runtime has no tabs; to open it as a window of its own, remove inside and give it a name and a match",
			ErrNoTabs, t.Name, t.Runtime.Inside, c.Runtime.Name())
	}
	return opener, nil
}

// container finds the instance the i-th target's tab lives in, and the tab
// in it when it is open.
func (c *Core) container(snap snapshot, p Project, i int, bound Bindings) (in revier.Instance, found bool, tab revier.PanelID, open bool, err error) {
	t := p.Targets[i]
	j, ok := p.index(t.Runtime.Inside)
	if !ok {
		return revier.Instance{}, false, "", false, fmt.Errorf("%w: target %q is inside %q", ErrNoTarget, t.Name, t.Runtime.Inside)
	}
	host, _, m, err := c.resolveAt(p, j)
	if err != nil {
		return revier.Instance{}, false, "", false, err
	}
	in, found = c.locate(snap, p, j, host, m, bound[t.Runtime.Inside])
	if !found {
		return revier.Instance{}, false, "", false, nil
	}
	tab, open = tabOf(in, t.Name)
	return in, true, tab, open, nil
}

// tabOf is the first panel of an instance, in the order the runtime lists
// them, that was opened for the named target: the tab's first panel, and the
// first that is left when that one has ended.
func tabOf(in revier.Instance, name revier.TargetName) (revier.PanelID, bool) {
	for _, panel := range in.Panels {
		if panel.Vars[PanelTargetVar] == string(name) {
			return panel.ID, true
		}
	}
	return "", false
}

// tabAt is every panel of the tab that holds a panel. On a runtime with no
// tabs it is the panel alone.
func tabAt(in revier.Instance, panel revier.PanelID) []revier.PanelID {
	tab := tabOfPanel(in, panel)
	if tab == "" {
		return []revier.PanelID{panel}
	}
	var panels []revier.PanelID
	for _, p := range in.Panels {
		if p.Tab == tab {
			panels = append(panels, p.ID)
		}
	}
	return panels
}

// tabOfPanel is the tab that holds the panel, empty when the instance lists
// no such panel or its runtime has no tabs.
func tabOfPanel(in revier.Instance, panel revier.PanelID) string {
	i := slices.IndexFunc(in.Panels, func(p revier.Panel) bool { return p.ID == panel })
	if i < 0 {
		return ""
	}
	return in.Panels[i].Tab
}

// ownTab reports whether the panel sits in the workspace's own tab, the one
// that holds ownPanel. A tab the workspace opened beside it - the agent and
// shell of `revier agent new` - is not it, and closes whole; a runtime with
// no tabs has only its own.
//
// known says whether the instance names its own panel at all. It does not
// when every panel carries a target mark and none carries the home one, and
// then neither answer is the truth: the caller decides what to do with an
// own tab nobody can point at.
func ownTab(in revier.Instance, panel revier.PanelID) (own, known bool) {
	p, ok := ownPanel(in)
	if !ok {
		return false, false
	}
	return tabOfPanel(in, p) == tabOfPanel(in, panel), true
}

// ownPanel is the instance's own first panel, where a press that returns home
// from a tab lands: the first panel of the tab revier marked when it opened
// the instance. The tab is taken from its start, because the mark of Open is
// best effort and the first panel can be without it.
//
// An instance opened before the mark existed carries none, and falls back to
// the first panel no tab target claims. That guess can land in a shell inside
// a tab, which is what the mark exists to stop (decisions.md D100).
func ownPanel(in revier.Instance) (revier.PanelID, bool) {
	for _, panel := range in.Panels {
		if panel.Vars[PanelHomeVar] != "" {
			return tabAt(in, panel.ID)[0], true
		}
	}
	for _, panel := range in.Panels {
		if panel.Vars[PanelTargetVar] == "" {
			return panel.ID, true
		}
	}
	return "", false
}

// goTab is goTo for a target inside another. The tab is opened only when the
// instance holds none for this target, so a second press never makes a second
// tab; the instance is opened first when it is not there.
//
// Toggle-back holds as for a window: when the OS window has focus and any
// panel of the tab is current in it, the press goes home. Home that is the same instance is
// reached by making its own first panel current, because focusing the
// instance alone would leave the tab where it is.
func (c *Core) goTab(ctx context.Context, p Project, i int, bound Bindings, resumes []Resume) (Result, error) {
	t := p.Targets[i]
	opener, err := c.tabHost(p, i)
	if err != nil {
		return Result{}, err
	}
	snap, err := c.answered(ctx, nameOf(c.Runtime))
	if err != nil {
		return Result{}, err
	}
	in, found, tab, open, err := c.container(snap, p, i, bound)
	if err != nil {
		return Result{}, err
	}
	// An instance opened for the tab was focused by that goTo, and the window
	// host may not list its OS window yet: it is raised by the launch, not here.
	// It holds no tab yet, unless this tab is one it opened with, so only its
	// ref is needed.
	opened := !found
	// withInstance says the tab opened with the instance, as a tab it lists.
	withInstance := false
	if opened {
		res, err := c.goTo(ctx, p, t.Runtime.Inside, bound)
		if err != nil {
			return res, err // an instance opened and not focused is still pinned
		}
		if res.Ref.IsZero() {
			return Result{}, fmt.Errorf("%s: opened %s for tab %s, and cannot name it", c.Runtime.Name(), t.Runtime.Inside, t.Name)
		}
		in = revier.Instance{Ref: res.Ref}
		if slices.Contains(res.Tabs, t.Name) {
			// The instance opened with this tab in it. It is read again to
			// name the tab, which is then focused and not opened a second time.
			if snap, err = c.answered(ctx, nameOf(c.Runtime)); err != nil {
				return Result{Target: res.Target, Ref: res.Ref}, err
			}
			if listed, ok := byRef(snap, res.Ref); ok {
				find := tabOf
				if res.Tabs[0] == t.Name {
					find = createdTab
				}
				if id, isOpen := find(listed, t.Name); isOpen {
					tab, open, withInstance = id, true, true
				}
			}
		}
	}

	if !opened && open && c.focusedOn(ctx, snap, in) {
		if cur, err := opener.FocusedPanel(ctx, in.Ref); err == nil && slices.Contains(tabAt(in, tab), cur) {
			return c.goHomeFromTab(ctx, p, snap, in, bound, opener)
		}
	}

	var osw revier.TargetRef
	if !opened {
		if osw, err = c.raisable(snap, in, t.Name); err != nil {
			return Result{}, err
		}
	}
	// The ref is the instance that holds the tab, so the result names that
	// instance's target: a caller binds the two, and a tab bound to it would
	// keep raising the instance after inside is removed from the file.
	res := Result{Target: t.Runtime.Inside, Tab: t.Name, Ref: in.Ref, Launched: withInstance}
	// An instance this press opened is reported with a failure after it, as
	// goTo reports one it could not focus, so the activation still pins it.
	var failed Result
	if opened {
		failed = Result{Target: res.Target, Ref: res.Ref}
	}
	if !open {
		res.Agents = inTab(resumes)
		// A tab its key opens is not read through the listed tabs, so the
		// panels of a link's tab get their argv here.
		real, err := c.linkPanels(p, *t.Runtime)
		if err != nil {
			return failed, err
		}
		if tab, err = opener.OpenTab(ctx, in.Ref, real, c.tabVars(p, t, in, opened)); err != nil {
			return failed, fmt.Errorf("%s: open tab %s: %w", c.Runtime.Name(), t.Name, err)
		}
		res.Launched = true
	}
	// An instance this press opened was focused by its goTo. One that was there
	// is focused before its tab: on tmux, focusing the session is what brings
	// a terminal showing another session to the tab.
	if !opened {
		if err := c.focus(ctx, in.Ref); err != nil {
			return Result{}, err
		}
	}
	if err := opener.FocusPanel(ctx, in.Ref, tab); err != nil {
		return failed, fmt.Errorf("%s: focus tab %s: %w", c.Runtime.Name(), t.Name, err)
	}
	if err := c.raise(ctx, osw, t.Name); err != nil {
		return Result{}, err
	}
	return res, nil
}

// tabVars is the mark on the panels of a tab its key opens: the tab's own. The
// active tab of an instance that was there, and that has lost every panel
// with the home mark, gets that mark again: every other tab of such an
// instance carries a target mark, so nothing else names where a return home
// lands (decisions.md D100). An instance this press opened has its mark.
func (c *Core) tabVars(p Project, t revier.Target, in revier.Instance, opened bool) map[string]string {
	vars := map[string]string{PanelTargetVar: string(t.Name)}
	if opened || slices.ContainsFunc(in.Panels, func(panel revier.Panel) bool { return panel.Vars[PanelHomeVar] != "" }) {
		return vars
	}
	j, _ := p.index(t.Runtime.Inside)
	if _, real, _, err := c.resolveAt(p, j); err == nil {
		if o, err := c.opening(p, t.Runtime.Inside, real); err == nil {
			return o.vars(t.Name)
		}
	}
	return vars
}

// inTab is what a tab target's restore does with its recorded agent: nothing.
// The tab runs its own launch argv, and a resume flag added to an argv that is
// not the agent starts a broken command, so the save warns instead.
func inTab(resumes []Resume) []AgentOutcome {
	if len(resumes) == 0 {
		return nil
	}
	outcomes := make([]AgentOutcome, len(resumes))
	for n := range outcomes {
		outcomes[n] = AgentInTab
	}
	return outcomes
}

// goHomeFromTab is the second press on a tab. A home that holds the tab gets
// its own first panel back; any other home is an ordinary goTo.
func (c *Core) goHomeFromTab(ctx context.Context, p Project, snap snapshot, in revier.Instance, bound Bindings, opener revier.PanelOpener) (Result, error) {
	home, _ := p.Home()
	if c.holds(snap, p, home.Name, bound, in) {
		if panel, ok := ownPanel(in); ok {
			if err := opener.FocusPanel(ctx, in.Ref, panel); err != nil {
				return Result{}, fmt.Errorf("%s: focus %s: %w", c.Runtime.Name(), home.Name, err)
			}
			return Result{Target: home.Name, Ref: in.Ref}, nil
		}
	}
	return c.goTo(ctx, p, home.Name, bound)
}

// holds reports whether the named target's instance is in.
func (c *Core) holds(snap snapshot, p Project, name revier.TargetName, bound Bindings, in revier.Instance) bool {
	i, ok := p.index(name)
	if !ok {
		return false
	}
	host, _, m, err := c.resolveAt(p, i)
	if err != nil {
		return false
	}
	inst, found := c.locate(snap, p, i, host, m, bound[name])
	return found && key(inst.Ref) == key(in.Ref)
}

// tabView is a tab target's row in the survey. It is available where the
// runtime can open it, and its ref is the instance that holds it while the tab
// is open. The instance's agents are its own target's to report.
func (c *Core) tabView(snap snapshot, failed hostErrs, p Project, i int, bound Bindings, tv revier.TargetView) revier.TargetView {
	// A tab never reaches resolveAt, so its own refusal is read here.
	if err := p.compiled[i].err; err != nil {
		tv.Reason = err.Error()
		return tv
	}
	if _, ok := c.Runtime.(revier.PanelOpener); !ok {
		return tv
	}
	// The runtime could not list: whether the tab is open is not known,
	// which is not the same as closed.
	if ferr := failed[c.Runtime.Name()]; ferr != nil {
		tv.Available, tv.Host, tv.Unknown = true, c.Runtime.Name(), ferr.Error()
		return tv
	}
	in, _, _, open, err := c.container(snap, p, i, bound)
	if err != nil {
		return tv
	}
	tv.Available, tv.Host = true, c.Runtime.Name()
	if open {
		tv.Ref = in.Ref
	}
	return tv
}
