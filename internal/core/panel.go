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
// change, and a position is not an identity (decisions.md D64).
const PanelTargetVar = "revier_target"

// isTab reports whether the i-th target is a tab inside another target.
func (p Project) isTab(i int) bool { return tabTarget(p.Targets[i]) }

// tabTarget reports whether t is a tab inside another target.
func tabTarget(t revier.Target) bool { return t.Runtime != nil && t.Runtime.Inside != "" }

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

// tabOf is the panel of an instance that was opened for the named target.
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
func ownTab(in revier.Instance, panel revier.PanelID) bool {
	own, ok := ownPanel(in)
	return ok && tabOfPanel(in, own) == tabOfPanel(in, panel)
}

// ownPanel is the first panel of an instance that no tab target claims: where
// a press that returns home from a tab lands.
func ownPanel(in revier.Instance) (revier.PanelID, bool) {
	for _, panel := range in.Panels {
		if panel.Vars[PanelTargetVar] == "" {
			return panel.ID, true
		}
	}
	return "", false
}

// goTab is Go for a target inside another. The tab is opened only when the
// instance holds none for this target, so a second press never makes a second
// tab; the instance is opened first when it is not there.
//
// Toggle-back holds as for a window: when the OS window has focus and the tab
// is current in it, the press goes home. Home that is the same instance is
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
	// An instance opened for the tab was focused by that Go, and the window
	// host may not list its OS window yet: it is raised by the launch, not here.
	// It holds no tab yet, so only its ref is needed.
	opened := !found
	if opened {
		res, err := c.Go(ctx, p, t.Runtime.Inside, bound)
		if err != nil {
			return res, err // an instance opened and not focused is still pinned
		}
		if res.Ref.IsZero() {
			return Result{}, fmt.Errorf("%s: opened %s for tab %s, and cannot name it", c.Runtime.Name(), t.Runtime.Inside, t.Name)
		}
		in = revier.Instance{Ref: res.Ref}
	}

	if open && c.focusedOn(ctx, snap, in) {
		if cur, err := opener.FocusedPanel(ctx, in.Ref); err == nil && cur == tab {
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
	res := Result{Target: t.Runtime.Inside, Ref: in.Ref}
	// An instance this press opened is reported with a failure after it, as
	// Go reports one it could not focus, so the caller still pins it.
	var failed Result
	if opened {
		failed = Result{Target: res.Target, Ref: res.Ref}
	}
	if !open {
		res.Agents = inTab(resumes)
		if tab, err = opener.OpenTab(ctx, in.Ref, *t.Runtime, map[string]string{PanelTargetVar: string(t.Name)}); err != nil {
			return failed, fmt.Errorf("%s: open tab %s: %w", c.Runtime.Name(), t.Name, err)
		}
		res.Launched = true
	}
	// An instance this press opened was focused by its Go. One that was there
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
// its own first panel back; any other home is an ordinary Go.
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
	return c.Go(ctx, p, home.Name, bound)
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
