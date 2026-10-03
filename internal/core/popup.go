package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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

// SwitchKey is the press the popup's surface switches its list on, as a
// terminal delivers alt+space. The desktop takes the trigger key before the
// popup's terminal sees it, so a press on the focused popup is typed into its
// panel instead (decisions.md D110).
const SwitchKey = "\x1b "

// Popup switches the list of the surface the focused window shows: the second
// press of the key on the popup, and the first on a revier the user runs in a
// terminal of their own (decisions.md D110). With no surface under the key it
// raises the popup when it is open. Otherwise it launches what argv
// returns, which must start a terminal of class PopupClass running the
// surface, then raises the new window and places it (D76). The
// size is decided before the launch, so the window moves once and is never
// resized after it shows. argv is a function, asked only for a launch: what
// it carries - the project the surface opens on - costs a listing the raise
// must not pay.
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
	if ref, ok := c.switchSurface(ctx, before); ok {
		slog.Info("popup: switch", "ref", ref)
		return ref, nil
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
	slog.Info("popup: launched", "ref", ref, "workarea_width", width, "place", geometry)
	// The raise goes first: the placement returns only once the frame has
	// settled, and the window must not wait that long for the keyboard.
	if err := c.Window.Focus(ctx, ref); err != nil {
		return ref, err
	}
	c.place(ctx, revier.Realization{Place: geometry}, ref)
	return ref, nil
}

// switchSurface types SwitchKey into the surface the focused window shows,
// and reports that window and whether it did. The surface is the popup, or a
// revier the user started in a terminal of their own: the key means the same
// on both, and the desktop takes it from both. windows is the window host's
// listing.
//
// The panel is the runtime's to find and to type into. A runtime that cannot
// type, a focused window that is no terminal of it, and a terminal whose
// current panel runs something else all leave the press what it was before
// the surface had a second list: the popup, raised or opened.
func (c *Core) switchSurface(ctx context.Context, windows []revier.Instance) (revier.TargetRef, bool) {
	w, ok := c.Runtime.(revier.PanelWriter)
	if !ok {
		return revier.TargetRef{}, false
	}
	focused, err := c.Window.Focused(ctx)
	if err != nil || focused.IsZero() {
		return revier.TargetRef{}, false
	}
	at := slices.IndexFunc(windows, func(in revier.Instance) bool { return in.Ref.ID == focused.ID })
	if at < 0 {
		return revier.TargetRef{}, false
	}
	instances, err := c.Runtime.Instances(ctx)
	if err != nil {
		slog.Warn("popup: switch", "err", err)
		return revier.TargetRef{}, false
	}
	inst, panel, ok := c.surfacePanel(ctx, instances, windows[at])
	if !ok {
		return revier.TargetRef{}, false
	}
	if err := w.SendText(ctx, inst.Ref, panel, SwitchKey); err != nil {
		slog.Warn("popup: switch", "ref", inst.Ref, "err", err)
		return revier.TargetRef{}, false
	}
	return focused, true
}

// surfacePanel is the panel of a window that runs the surface, and the
// runtime instance that holds it. The popup's terminal is found by its class
// and holds the surface alone. Any other window is paired with its terminal
// as an attachment is (runtimeOf), and counts only while the panel current in
// it runs revier with no command: a terminal with the surface in another tab
// is a terminal the user is doing something else in.
func (c *Core) surfacePanel(ctx context.Context, instances []revier.Instance, window revier.Instance) (revier.Instance, revier.PanelID, bool) {
	if window.Class == PopupClass {
		for _, inst := range instances {
			if inst.Class == PopupClass && len(inst.Panels) > 0 {
				return inst, inst.Panels[0].ID, true
			}
		}
		return revier.Instance{}, "", false
	}
	inst, ok := c.runtimeOf(instances, window)
	opener, canAsk := c.Runtime.(revier.PanelOpener)
	if !ok || !canAsk || !slices.ContainsFunc(inst.Panels, surface) {
		return revier.Instance{}, "", false
	}
	current, err := opener.FocusedPanel(ctx, inst.Ref)
	if err != nil {
		return revier.Instance{}, "", false
	}
	for _, p := range inst.Panels {
		if p.ID == current && surface(p) {
			return inst, p.ID, true
		}
	}
	return revier.Instance{}, "", false
}

// surface reports a panel whose foreground command is revier with no command
// of its own: the TUI. `revier agent wait` in a panel is not one.
func surface(p revier.Panel) bool {
	return len(p.Command) == 1 && p.Runs("revier")
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
