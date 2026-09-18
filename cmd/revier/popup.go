package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdPopup raises the popup, or opens the TUI in a kitty window of its own and
// places it (decisions.md D76). The tools it needs are named when missing,
// because a desktop key that does nothing says nothing about why.
func cmdPopup(ctx context.Context, a *app) error {
	if _, err := exec.LookPath("kitty"); err != nil {
		return fmt.Errorf("popup: kitty is not on PATH; the popup is the TUI in a kitty window (https://sw.kovidgoyal.net/kitty/)")
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("popup: %w", err)
	}
	// The project the surface opens on is resolved here, at the keypress,
	// where the focused window is still the user's: once the popup is up it
	// is the focused window itself, and no target matches it. A launch alone
	// pays for it; a raise keeps the cursor where it was (decisions.md D86).
	_, err = a.core.Popup(ctx, func(ctx context.Context) []string {
		start := revier.ProjectName("")
		if p, err := a.resolveProject(ctx, ""); err == nil {
			start = p.Name
		}
		return popupArgv(self, start)
	})
	if !errors.Is(err, core.ErrNoPopupHost) {
		return err
	}
	if _, lookErr := exec.LookPath("wctl"); lookErr != nil {
		return fmt.Errorf("popup: wctl is not on PATH; the popup needs GNOME with the Window Control extension and its wctl (https://github.com/carlo9890/gnome-window-control)")
	}
	return fmt.Errorf("popup: %w; it needs GNOME with the Window Control extension enabled, and [hosts] window must allow gnome (https://github.com/carlo9890/gnome-window-control)", err)
}

// popupArgv is the kitty that runs the surface as the popup, opening on
// start when there is one.
//
// Without remember_window_size=no, the popup opens at the size of the last
// kitty window closed; one as large as the workarea is maximized, and a
// maximized window refuses the placement. The option also keeps the popup's
// own size out of what the user's next kitty window opens at. env(1) marks
// the surface as the popup's (core.PopupEnv) and names its project, and
// nothing else: kitty's own env option would reach every window launched
// into this kitty, which is where the surface's targets open.
func popupArgv(self string, start revier.ProjectName) []string {
	mark := core.PopupEnv + "=" + popupMark(start)
	return []string{"kitty", "--class", core.PopupClass, "--title", "revier",
		"-o", "remember_window_size=no", "-e", "env", mark, self}
}

// popupMark is the value of core.PopupEnv: the project the surface opens on,
// or popupNoStart when none resolved. Either way it is set, which is what
// makes the surface the popup.
func popupMark(start revier.ProjectName) string {
	if start == "" {
		return popupNoStart
	}
	return string(start)
}

const popupNoStart = "-"
