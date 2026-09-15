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

// ErrNoPopupHost means the window host cannot do what the popup needs: list
// windows, launch one, and measure and place it. It is a normal outcome on a
// machine without one, and the caller names the tools that are missing.
var ErrNoPopupHost = errors.New("no window host that can place the popup")

const (
	// popupPercent is the width and the height of a centred popup, as a
	// percentage of the workarea.
	popupPercent = 70
	// popupMinWidth is the narrowest centred popup that still shows the
	// detail pane: its 100 columns at about 10 px a cell, and the terminal's
	// padding. A workarea too narrow for it gets a popup that fills it.
	popupMinWidth = 1100
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
	for _, w := range before {
		if w.Class == PopupClass {
			slog.Info("popup: raise", "ref", w.Ref)
			return w.Ref, c.Window.Focus(ctx, w.Ref)
		}
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
		for _, w := range fresh {
			if w.Class == PopupClass {
				return w, true, false
			}
		}
		return revier.Instance{}, false, false
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

// popupGeometry centres the popup when its share of the workarea is wide
// enough for the detail pane, and fills the workarea when it is not.
func popupGeometry(workareaWidth int) string {
	if workareaWidth*popupPercent/100 < popupMinWidth {
		return "left top 100% 100%"
	}
	return fmt.Sprintf("center center %d%% %d%%", popupPercent, popupPercent)
}
