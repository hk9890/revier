package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	// popupWidth is the width of a centred popup, in the workarea's logical
	// pixels. It is fixed, so that on a wide screen the popup stays one spot
	// to read, and it holds the detail pane's 100 columns. A workarea
	// narrower than it gets a popup that fills it. The height is always 100%.
	popupWidth = 1800
	// popupPoll is how often the popup's launch asks for its window. Faster
	// than BindPoll, because the wait is between a keypress and a window that
	// moves into place as soon as it is found.
	popupPoll = 20 * time.Millisecond
)

// Popup raises the popup when it is open. Otherwise it launches argv, which
// must start a terminal of class PopupClass running the surface, then raises
// the new window and places it (decisions.md D76). The size is decided before
// the launch, so the window moves once and is never resized after it shows.
func (c *Core) Popup(ctx context.Context, argv []string) (revier.TargetRef, error) {
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
	if w, ok := popupWindow(before); ok {
		slog.Info("popup: raise", "ref", w.Ref)
		return w.Ref, c.Window.Focus(ctx, w.Ref)
	}
	width, err := area.WorkareaWidth(ctx)
	if err != nil {
		return revier.TargetRef{}, fmt.Errorf("%s: workarea: %w", c.Window.Name(), err)
	}
	if _, err := c.Window.Open(ctx, revier.Realization{
		Launch: argv,
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
	slog.Info("popup: launched", "ref", ref, "workarea_width", width, "place", geometry)
	// The raise goes first: the placement returns only once the frame has
	// settled, and the window must not wait that long for the keyboard.
	if err := c.Window.Focus(ctx, ref); err != nil {
		return ref, err
	}
	c.place(ctx, revier.Realization{Place: geometry}, ref)
	return ref, nil
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

// popupWindow is the popup among the windows listed: the first of its class.
func popupWindow(windows []revier.Instance) (revier.Instance, bool) {
	for _, w := range windows {
		if w.Class == PopupClass {
			return w, true
		}
	}
	return revier.Instance{}, false
}

// popupGeometry centres the popup at popupWidth when the workarea holds it,
// and fills the workarea when it does not.
func popupGeometry(workareaWidth int) string {
	if workareaWidth < popupWidth {
		return "left top 100% 100%"
	}
	return fmt.Sprintf("center top %d 100%%", popupWidth)
}
