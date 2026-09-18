package main

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// The popup's kitty runs the surface through env(1), with the marker on the
// surface's command alone, and the marker names the project resolved at the
// keypress: once the popup is up, the focused window is the popup itself.
// With none resolved the marker is still set, since it is what makes the
// surface the popup.
func TestPopupArgvMarksTheSurfaceWithItsProject(t *testing.T) {
	argv := popupArgv("/usr/bin/revier", "demo")
	want := []string{"kitty", "--class", core.PopupClass, "--title", "revier",
		"-o", "remember_window_size=no", "-e", "env", core.PopupEnv + "=demo", "/usr/bin/revier"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if got := popupMark(""); got == "" || got == "demo" {
		t.Errorf("mark with no project = %q, want one that is set and names no project", got)
	}
}
