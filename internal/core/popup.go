package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// PopupClass is the class the popup's terminal is launched with. A press
// finds the popup already open by it, and a compositor rule can name it.
const PopupClass = "revier-popup"

// PopupEnv is the variable the popup's terminal starts the surface with, so
// the surface knows it is the popup and hides on Esc instead of exiting
// (decisions.md D86). A surface started in any other terminal has none.
const PopupEnv = "REVIER_POPUP"

// ErrNoPopupHost means the window host cannot do what the popup needs: list
// windows, launch one, and measure and place it. It is a normal outcome on a
// machine without one, and the caller names the tools that are missing.
var ErrNoPopupHost = errors.New("no window host that can place the popup")

const (
	// popupPoll is how often the popup's launch asks for its window. Faster
	// than BindPoll, because the wait is between a keypress and a window that
	// moves into place as soon as it is found.
	popupPoll = 20 * time.Millisecond
)

// Popup switches the list of the popup when the popup is the focused window:
// the second press of the key (decisions.md D182). Otherwise it raises the
// popup when it is open, or launches what argv returns, which must start a
// terminal of class PopupClass running the surface, then raises the new
// window and places it (D76). A revier the user runs in a terminal of their
// own is no popup: the key raises or opens the popup over it. The size is
// decided before the launch, so revier places the window once and never
// measures it to place it again. argv is a function, asked only for a launch:
// what it carries - the project the surface opens on - costs a listing the
// raise must not pay.
func (c *Core) Popup(ctx context.Context, argv func(context.Context) []string) (revier.TargetRef, error) {
	if c.Window == nil {
		return revier.TargetRef{}, ErrNoPopupHost
	}
	area, ok := c.Window.(revier.WorkareaReader)
	if _, placer := c.Window.(revier.WindowPlacer); !ok || !placer {
		return revier.TargetRef{}, fmt.Errorf("%s: %w", c.Window.Name(), ErrNoPopupHost)
	}
	before, err := c.Window.Instances(ctx)
	if err != nil {
		return revier.TargetRef{}, err
	}
	// The window the key was pressed over decides a switch, and is in a
	// launch's line by its id alone, as its title is the user's: a launch with
	// no window focused is what a popup that went away while it had the focus
	// leaves behind.
	over, err := c.Window.Focused(ctx)
	if err != nil {
		slog.Warn("popup: focused window", "err", err)
	}
	if c.switchPopup(ctx, before, over) {
		slog.Info("popup: switch", "ref", over)
		return over, nil
	}
	if w, ok := popupWindow(before); ok {
		slog.Info("popup: raise", "ref", w.Ref, "minimized", w.Minimized)
		// The surface reads it at the focus the raise gives it, so it is on
		// record first (decisions.md D169).
		marked := c.Ledger != nil && w.Minimized
		if marked {
			if err := c.Ledger.MarkPopupHidden(); err != nil {
				slog.Warn("popup: mark the raise", "err", err)
			}
		}
		err := c.Window.Focus(ctx, w.Ref)
		if err != nil && marked {
			// No focus comes to take it, and a later one must not.
			c.Ledger.TakePopupHidden()
		}
		return w.Ref, err
	}
	// A mark left by a popup that went away is no raise of the new one.
	c.TakePopupHidden()
	width, err := area.WorkareaWidth(ctx)
	if err != nil {
		return revier.TargetRef{}, fmt.Errorf("%s: workarea: %w", c.Window.Name(), err)
	}
	if _, err := c.Window.Open(ctx, revier.Realization{
		Launch: argv(ctx),
		Match:  revier.Match{Class: "^" + PopupClass + "$"},
	}); err != nil {
		return revier.TargetRef{}, err
	}
	w, _, err := c.awaitNew(ctx, before, BindWait, popupPoll, func(fresh []revier.Instance) (revier.Instance, bool, bool) {
		w, ok := popupWindow(fresh)
		return w, ok, false
	})
	if errors.Is(err, errNoNewWindow) {
		return revier.TargetRef{}, fmt.Errorf("popup: no window of class %s appeared in %s", PopupClass, BindWait)
	}
	if err != nil {
		return revier.TargetRef{}, err
	}
	ref := w.Ref
	geometry := popupGeometry(width)
	slog.Info("popup: launched", "ref", ref, "over", over.ID, "workarea_width", width, "place", geometry)
	// The raise goes first, so the keyboard reaches the window before it moves.
	if err := c.Window.Focus(ctx, ref); err != nil {
		return ref, err
	}
	c.place(ctx, revier.Realization{Place: geometry}, ref)
	return ref, nil
}

// switchPopup tells the popup to switch its list when it is the focused
// window, and reports whether it did. focused is the window host's focused
// window, zero for none, and windows its listing: the popup is found by its
// class alone, so no runtime is asked and the switch works under every
// runtime. A press the popup does not take - no SwitchPopup, or no surface
// that listens - leaves the press what it was before the surface had a
// second list: the popup, raised.
func (c *Core) switchPopup(ctx context.Context, windows []revier.Instance, focused revier.TargetRef) bool {
	if c.SwitchPopup == nil || focused.IsZero() {
		return false
	}
	if !slices.ContainsFunc(windows, func(w revier.Instance) bool { return w.Ref.ID == focused.ID && w.Class == PopupClass }) {
		return false
	}
	if err := c.SwitchPopup(ctx); err != nil {
		slog.Warn("popup: switch", "ref", focused, "err", err)
		return false
	}
	return true
}

// RaiseSelf brings the terminal this process runs in back to the front: its
// panel current, and its window raised, as a press on an agent brings that
// agent's. The surface calls it once a restore has ended, because each
// window the restore opened came up over it and hid the result. The terminal
// is the runtime's panel that c.Self names. A surface in a terminal no
// runtime here lists - or with no Self - is left where it is, and so is one
// whose window the window host cannot pair with it: raising a guess would
// raise somebody else's window. The popup is raised by RaisePopup instead.
func (c *Core) RaiseSelf(ctx context.Context) error {
	if c.Self == nil || c.Runtime == nil {
		return nil
	}
	start := time.Now()
	snap, failed := c.listing(ctx)
	if err := failed[c.Runtime.Name()]; err != nil {
		logging.Op("surface raise", start, err)
		return err
	}
	for _, inst := range snap[c.Runtime.Name()] {
		for _, p := range inst.Panels {
			if c.Self(p) {
				err := c.focusPanel(ctx, snap, failed, inst.Ref, p.ID, "revier")
				logging.Op("surface raise", start, err, "ref", inst.Ref, "panel", p.ID)
				return err
			}
		}
	}
	return nil
}

// RaisePopup brings the popup back to the front once a restore has ended, as
// RaiseSelf does for any other surface. The popup is found by its class
// through the window host, as the press that raises it is: no runtime is
// asked, so it is raised under every runtime (decisions.md D182). It is
// ErrNoPopupHost with no window host.
func (c *Core) RaisePopup(ctx context.Context) error {
	if c.Window == nil {
		return ErrNoPopupHost
	}
	start := time.Now()
	windows, err := c.Window.Instances(ctx)
	if err == nil {
		w, ok := popupWindow(windows)
		if !ok {
			err = fmt.Errorf("popup: no window of class %s to raise", PopupClass)
		} else {
			err = c.Window.Focus(ctx, w.Ref)
		}
	}
	logging.Op("surface raise", start, err, "popup", true)
	return err
}

// HidePopup takes the open popup off the screen and keeps it, for the next
// press to raise with everything it holds (decisions.md D86). It is
// ErrNoPopupHost where the window host cannot hide, and an error when no
// popup is open; the surface exits on either, as it did before.
func (c *Core) HidePopup(ctx context.Context) error {
	hider, ok := c.Window.(revier.Hider)
	if !ok {
		return ErrNoPopupHost
	}
	windows, err := c.Window.Instances(ctx)
	if err != nil {
		return err
	}
	w, ok := popupWindow(windows)
	if !ok {
		return fmt.Errorf("popup: no window of class %s to hide", PopupClass)
	}
	slog.Info("popup: hide", "ref", w.Ref)
	return hider.Hide(ctx, w.Ref)
}

// TakePopupHidden reports whether the press that raised the popup found it
// minimized, and takes it off the record: the surface asks at each focus, so
// the next focus, with no press, finds none (decisions.md D169).
func (c *Core) TakePopupHidden() bool {
	return c.Ledger != nil && c.Ledger.TakePopupHidden()
}

// PopupMinimized reports whether the survey listed the popup's window
// minimized: a popup without the focus finds the desktop's hide in it, and
// stops surveying (decisions.md D169).
func (r Report) PopupMinimized() bool {
	w, ok := popupWindow(r.Windows)
	return ok && w.Minimized
}

// popupWindow is the popup among the windows listed: the first of its class.
func popupWindow(windows []revier.Instance) (revier.Instance, bool) {
	for _, w := range windows {
		if w.Class == PopupClass {
			return w, true
		}
	}
	return revier.Instance{}, false
}

// popupTier is one size of the popup: the place it gets on a screen at
// least minWidth logical pixels wide.
type popupTier struct {
	minWidth int
	place    string
}

// popupTiers are the popup's sizes, widest screen first; the last holds on
// any screen. The height is always 100%. On a very wide screen the width is
// fixed, so that the popup stays one spot to read, and it holds the detail
// pane's 100 columns. On a 29 inch wide screen it is most of the screen, and
// on a laptop all of it.
//
// revier places the popup it launched by them, against the workarea, and
// writes them as the window host's rules, which place it against the
// monitor before its first frame (decisions.md D189). The two agree but on
// a screen whose dock takes it across a tier's width.
var popupTiers = []popupTier{
	{minWidth: 3400, place: "center top 1800 100%"},
	{minWidth: 2200, place: "center top 75% 100%"},
	{minWidth: 0, place: "left top 100% 100%"},
}

// popupGeometry is the place of the first tier the workarea is wide enough
// for.
func popupGeometry(workareaWidth int) string {
	for _, t := range popupTiers {
		if workareaWidth >= t.minWidth {
			return t.place
		}
	}
	return popupTiers[len(popupTiers)-1].place
}
