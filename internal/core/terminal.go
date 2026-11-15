package core

import (
	"context"

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
type tabs struct {
	revier.TabOpener
	view revier.View
}

func (t tabs) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	f, ok := t.view.(revier.PanelFocuser)
	if !ok {
		return nil
	}
	return f.FocusPanel(ctx, ref, panel)
}

func (t tabs) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	f, ok := t.view.(revier.PanelFocuser)
	if !ok {
		return "", nil
	}
	return f.FocusedPanel(ctx, ref)
}

// opener is the runtime's tabs when host is the runtime and it opens tabs.
func (c *Core) opener(host revier.Host) (panelOpener, bool) {
	if host == nil || c.Runtime == nil || host.Name() != c.Runtime.Name() {
		return nil, false
	}
	o, ok := c.Runtime.(revier.TabOpener)
	if !ok {
		return nil, false
	}
	return tabs{TabOpener: o, view: c.View}, true
}

// finder is the view's map from the panel id a terminal key passes to the
// instance that holds it, and false when the view's ids are the runtime's.
func (c *Core) finder() (revier.PanelFinder, bool) {
	f, ok := c.View.(revier.PanelFinder)
	return f, ok
}
