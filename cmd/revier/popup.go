package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/hk9890/revier/internal/core"
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
	// Without remember_window_size=no, the popup opens at the size of the last
	// kitty window closed; one as large as the workarea is maximized, and a
	// maximized window refuses the placement. The option also keeps the
	// popup's own size out of what the user's next kitty window opens at.
	argv := []string{"kitty", "--class", core.PopupClass, "--title", "revier",
		"-o", "remember_window_size=no", "-e", self}
	_, err = a.core.Popup(ctx, argv)
	if !errors.Is(err, core.ErrNoPopupHost) {
		return err
	}
	if _, lookErr := exec.LookPath("wctl"); lookErr != nil {
		return fmt.Errorf("popup: wctl is not on PATH; the popup needs GNOME with the Window Control extension and its wctl (https://github.com/carlo9890/gnome-window-control)")
	}
	return fmt.Errorf("popup: %w; it needs GNOME with the Window Control extension enabled, and [hosts] window must allow gnome (https://github.com/carlo9890/gnome-window-control)", err)
}
