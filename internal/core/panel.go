package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoTabs is returned for a target declared inside another on a runtime
// that cannot open a tab. It is a configuration the machine cannot serve, so
// the keypress says which runtime and which target, rather than opening a
// window the user did not ask for.
var ErrNoTabs = errors.New("cannot open a tab inside another target")

// PanelTargetVar is the panel variable that names the target a tab was
// opened for. It is the tab's whole identity: a title is the program's to
// change, and a position is not an identity (decisions.md D64).
const PanelTargetVar = "revier_target"

// isTab reports whether the i-th target is a tab inside another target.
func (p Project) isTab(i int) bool {
	r := p.Targets[i].Runtime
	return r != nil && r.Inside != ""
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
		return nil, fmt.Errorf("%w: target %q is inside %q, and the %s runtime has no tabs; remove inside to open it as a window of its own",
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
	if host.Name() != c.Runtime.Name() {
		return revier.Instance{}, false, "", false, fmt.Errorf("target %q is inside %q, which opens on %s, not on the runtime", t.Name, t.Runtime.Inside, host.Name())
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
func (c *Core) goTab(ctx context.Context, p Project, i int, bound Bindings) (Result, error) {
	t := p.Targets[i]
	opener, err := c.tabHost(p, i)
	if err != nil {
		return Result{}, err
	}
	snap, err := c.snapshot(ctx)
	if err != nil {
		return Result{}, err
	}
	in, found, tab, open, err := c.container(snap, p, i, bound)
	if err != nil {
		return Result{}, err
	}
	// An instance opened for the tab was focused by that Go, and the window
	// host may not list its OS window yet: it is raised by the launch, not here.
	opened := !found
	if opened {
		res, err := c.Go(ctx, p, t.Runtime.Inside, bound)
		if err != nil {
			return Result{}, err
		}
		if snap, err = c.snapshot(ctx); err != nil {
			return Result{}, err
		}
		if in, found = byRef(snap, res.Ref); !found {
			return Result{}, fmt.Errorf("%s: opened %s for tab %s, and it is not listed", c.Runtime.Name(), t.Runtime.Inside, t.Name)
		}
		tab, open = tabOf(in, t.Name)
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
	if !open {
		if tab, err = opener.OpenTab(ctx, in.Ref, *t.Runtime, map[string]string{PanelTargetVar: string(t.Name)}); err != nil {
			return Result{}, fmt.Errorf("%s: open tab %s: %w", c.Runtime.Name(), t.Name, err)
		}
		res.Launched = true
	}
	if err := opener.FocusPanel(ctx, in.Ref, tab); err != nil {
		return Result{}, fmt.Errorf("%s: focus tab %s: %w", c.Runtime.Name(), t.Name, err)
	}
	if err := c.raise(ctx, osw, t.Name); err != nil {
		return Result{}, err
	}
	return res, nil
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
	if !ok || p.isTab(i) {
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
func (c *Core) tabView(snap snapshot, p Project, i int, bound Bindings, tv revier.TargetView) revier.TargetView {
	if _, err := c.tabHost(p, i); err != nil {
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
