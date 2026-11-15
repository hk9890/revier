package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// terminal is the runtime and the view that shows it, as the one host a
// runtime realization resolves to (decisions.md D176). The runtime lists and
// starts; the view focuses and reports the focus. With no view nothing is
// focused and nothing reports the focus, so a press raises nothing and a
// second press does not go home.
type terminal struct {
	revier.Runtime
	view revier.View
}

func (t terminal) Focus(ctx context.Context, ref revier.TargetRef) error {
	if t.view == nil {
		return nil
	}
	return t.view.Focus(ctx, ref)
}

func (t terminal) Focused(ctx context.Context) (revier.TargetRef, error) {
	if t.view == nil {
		return revier.TargetRef{}, nil
	}
	return t.view.Focused(ctx)
}

// terminal is the configured runtime with its view as a host, and nil with no
// runtime.
func (c *Core) terminal() revier.Host {
	if c.Runtime == nil {
		return nil
	}
	return terminal{Runtime: c.Runtime, view: c.View}
}

// capability is the optional capability T of the host that holds ref. The
// host a runtime ref resolves to is the terminal, which wraps the runtime and
// carries none of its optional capabilities, so a runtime ref asks the
// runtime itself; every other ref asks its host.
func capability[T any](c *Core, ref revier.TargetRef) (T, bool) {
	var h any = c.Runtime
	if c.Runtime == nil || ref.Host != c.Runtime.Name() {
		named, ok := c.hostNamed(ref.Host)
		if !ok {
			var none T
			return none, false
		}
		h = named
	}
	got, ok := h.(T)
	return got, ok
}

// osWindows reports that the view gives each runtime instance an OS window of
// the title the window host lists (decisions.md D177).
func (c *Core) osWindows() bool {
	return c.View != nil && c.View.Capabilities().OSWindows
}

// panelOpener opens a tab in the runtime and focuses a panel in the view.
type panelOpener interface {
	revier.TabOpener
	revier.PanelFocuser
}

// tabs is the runtime's tabs as the view shows them. With a view that cannot
// focus a panel, a tab opens and stays where it is, and no panel is current.
// A view that opens its own windows draws a tab that opened in an instance
// it shows already; one it does not show it leaves alone, so a script's tab
// opens no window (decisions.md D118). A tab of an instance being opened is
// drawn by nothing: the launch draws the instance once every tab runs
// (D180). Until then no panel of it is current, and the launch makes its
// active tab current after.
type tabs struct {
	revier.TabOpener
	c         *Core
	launching bool
}

func (t tabs) OpenTab(ctx context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	panel, err := t.TabOpener.OpenTab(ctx, ref, r, vars)
	if err != nil || t.launching {
		return panel, err
	}
	// The tab runs, and the next press that shows the instance draws it.
	if _, err := t.c.show(ctx, ref, false); err != nil {
		slog.Warn("show the new tab", "view", t.c.View.Name(), "ref", ref, "err", err)
	}
	return panel, nil
}

func (t tabs) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	f, ok := t.c.View.(revier.PanelFocuser)
	if !ok {
		return nil
	}
	return f.FocusPanel(ctx, ref, panel)
}

func (t tabs) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	f, ok := t.c.View.(revier.PanelFocuser)
	if !ok {
		return "", nil
	}
	if _, draws := t.c.shower(ref); draws && t.launching {
		return "", nil
	}
	return f.FocusedPanel(ctx, ref)
}

// opener is the runtime's tabs when host is the runtime and it opens tabs.
func (c *Core) opener(host revier.Host) (panelOpener, bool) {
	return c.tabsOf(host, false)
}

// launchOpener is opener for the launch of an instance, whose tabs the view
// draws only once they all run.
func (c *Core) launchOpener(host revier.Host) (panelOpener, bool) {
	return c.tabsOf(host, true)
}

func (c *Core) tabsOf(host revier.Host, launching bool) (panelOpener, bool) {
	if host == nil || c.Runtime == nil || host.Name() != c.Runtime.Name() {
		return nil, false
	}
	o, ok := c.Runtime.(revier.TabOpener)
	if !ok {
		return nil, false
	}
	return tabs{TabOpener: o, c: c, launching: launching}, true
}

// show has a view that opens windows of its own draw the runtime instance
// ref names, from a listing of the runtime alone, and reports whether it
// opened a window now. With open it opens one where none shows the
// instance: a press, which raises it. Every other view shows every instance
// it is the runtime of, and nothing is done (decisions.md D180, D181).
func (c *Core) show(ctx context.Context, ref revier.TargetRef, open bool) (bool, error) {
	shower, ok := c.shower(ref)
	if !ok {
		return false, nil
	}
	instances, err := c.Runtime.Instances(ctx)
	if err != nil {
		return false, fmt.Errorf("%s: instances: %w", c.Runtime.Name(), err)
	}
	i := slices.IndexFunc(instances, func(in revier.Instance) bool { return key(in.Ref) == key(ref) })
	if i < 0 {
		return false, fmt.Errorf("%s: %s is not listed to show", c.Runtime.Name(), ref.ID)
	}
	return shower.Show(ctx, instances[i], open)
}

// showListed is show for an instance a listing of this press holds already,
// so the runtime is not listed again.
func (c *Core) showListed(ctx context.Context, inst revier.Instance, open bool) (bool, error) {
	shower, ok := c.shower(inst.Ref)
	if !ok {
		return false, nil
	}
	return shower.Show(ctx, inst, open)
}

// shower is the view that opens windows of its own, when ref is an
// instance of the runtime it shows.
func (c *Core) shower(ref revier.TargetRef) (revier.Shower, bool) {
	shower, ok := c.View.(revier.Shower)
	if !ok || c.Runtime == nil || ref.Host != c.Runtime.Name() {
		return nil, false
	}
	return shower, true
}

// raisableShown is raisable for a press on an instance that is open: a view
// that opens its own windows first opens one where none shows it, and the OS
// window is then the new one, which the window host lists only after a
// wait. A new window it never lists is not raised, as a launch's is not
// (raiseNew). place is where the instance's target puts a window it starts,
// "" for none: the window opened now is one revier starts, so it goes there
// as a launch's does (decisions.md D24, D186).
//
// A new window can leave the last tab it drew current, as kitty's does, so
// the instance's own tab, where a return home lands, is made current in it:
// the window opens again as a launch leaves it, with the active tab current.
// A press that goes to a panel makes that one current after.
func (c *Core) raisableShown(ctx context.Context, snap snapshot, failed hostErrs, inst revier.Instance, name revier.TargetName, place string) (revier.TargetRef, error) {
	opened, err := c.showListed(ctx, inst, true)
	if err != nil {
		return revier.TargetRef{}, fmt.Errorf("%s: show %s: %w", c.View.Name(), name, err)
	}
	if !opened {
		return c.raisable(snap, failed, inst, name)
	}
	return c.shownAgain(ctx, inst, name, place)
}

// shownAgain sets up the OS window a view opened again over the live
// instance inst, as raisableShown says, and returns it: zero when the window
// host never lists it, or cannot pair it with inst.
func (c *Core) shownAgain(ctx context.Context, inst revier.Instance, name revier.TargetName, place string) (revier.TargetRef, error) {
	slog.Info("view opened again over a live instance", "view", c.View.Name(), "ref", inst.Ref, "target", name)
	if opener, ok := c.opener(c.terminal()); ok {
		if own, ok := ownPanel(inst); ok {
			if err := opener.FocusPanel(ctx, inst.Ref, own); err != nil {
				return revier.TargetRef{}, fmt.Errorf("%s: focus the own tab of %s: %w", c.View.Name(), name, err)
			}
		}
	}
	if !c.bridged(inst) {
		return revier.TargetRef{}, nil
	}
	return c.placeShown(ctx, inst.Ref, name, place), nil
}

// placeShown waits for the OS window a view opened again over the live
// instance ref, puts it where place says, and returns it: zero when the
// window host never lists it.
func (c *Core) placeShown(ctx context.Context, ref revier.TargetRef, name revier.TargetName, place string) revier.TargetRef {
	osw, ok := c.windowOfNew(ctx, ref)
	if !ok {
		slog.Warn("raise: the window host never listed the new window", "ref", ref, "target", name)
		return osw
	}
	if placer, can := c.Window.(revier.WindowPlacer); can && place != "" {
		c.placeOn(ctx, placer, osw, place)
	}
	return osw
}

// placeOf is where the project's target name puts a window it starts, and ""
// when it names no place or does not resolve.
func (c *Core) placeOf(p Project, name revier.TargetName) string {
	i, ok := p.index(name)
	if !ok {
		return ""
	}
	_, real, _, err := c.resolveAt(p, i)
	if err != nil {
		return ""
	}
	return real.Place
}

// finder is the view's map from the panel id a terminal key passes to the
// instance that holds it, and false when the view's ids are the runtime's.
func (c *Core) finder() (revier.PanelFinder, bool) {
	f, ok := c.View.(revier.PanelFinder)
	return f, ok
}
