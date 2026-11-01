package main

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// The popup's kitty runs the surface through env(1), with the marker on the
// surface's command alone, and the marker names the project resolved at the
// keypress: once the popup is up, the focused window is the popup itself.
// With none resolved the marker is still set, empty, since being set is what
// makes the surface the popup.
func TestPopupArgvMarksTheSurfaceWithItsProject(t *testing.T) {
	argv := popupArgv("/usr/bin/revier", "demo", "")
	want := []string{"kitty", "--class", core.PopupClass, "--title", "revier",
		"-o", "remember_window_size=no",
		"-o", "hide_window_decorations=yes",
		"-o", "clear_all_shortcuts=yes",
		"-o", "map=kitty_mod+c copy_to_clipboard",
		"-o", "map=kitty_mod+v paste_from_clipboard",
		"-e", "env", core.PopupEnv + "=demo", "/usr/bin/revier"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if argv := popupArgv("/usr/bin/revier", "", ""); !slices.Contains(argv, core.PopupEnv+"=") {
		t.Errorf("argv with no project = %q, want the marker set and empty", argv)
	}
}

// revier reaches the popup through its kitty's control socket, so that socket
// is a socket file only the user can reach, whatever kitty.conf names, and
// remote control is on for it. With no socket file, kitty.conf decides both:
// remote control on a socket it names could be open to every user. The
// options are before -e: what follows -e is the surface's command.
func TestPopupArgvPutsThePopupOnASocketFile(t *testing.T) {
	argv := popupArgv("/usr/bin/revier", "demo", "unix:/run/user/1000/kitty-{kitty_pid}")
	i := slices.Index(argv, "--listen-on")
	if i < 0 || argv[i+1] != "unix:/run/user/1000/kitty-{kitty_pid}" || i > slices.Index(argv, "-e") {
		t.Errorf("argv = %q, want --listen-on with the socket file, before -e", argv)
	}
	if j := slices.Index(argv, "allow_remote_control=socket-only"); j < 0 || j > slices.Index(argv, "-e") {
		t.Errorf("argv = %q, want remote control on for the socket file, before -e", argv)
	}
	if argv := popupArgv("/usr/bin/revier", "demo", ""); slices.Contains(argv, "allow_remote_control=socket-only") {
		t.Errorf("argv with no socket file = %q, want remote control left to kitty.conf", argv)
	}
}
