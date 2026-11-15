package main

import (
	"os"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/popupswitch"
)

// The popup's surface listens where the press of the core revier builds
// sends the switch, and the socket's file goes with the surface
// (decisions.md D182).
func TestThePopupListensWhereThePressSendsTheSwitch(t *testing.T) {
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	ln := listenForSwitch()
	if ln == nil {
		t.Fatal("listenForSwitch: no socket in a runtime directory")
	}
	defer func() { _ = ln.Close() }()
	c := newCore(&config.Config{}, nil, nil, nil)
	if err := c.SwitchPopup(t.Context()); err != nil {
		t.Fatalf("SwitchPopup: %v", err)
	}
	if err := popupswitch.Await(ln); err != nil {
		t.Fatalf("Await: %v", err)
	}
	_ = ln.Close()
	if err := c.SwitchPopup(t.Context()); err == nil {
		t.Error("SwitchPopup with the surface gone returned no error")
	}
	if entries, _ := os.ReadDir(run); len(entries) != 0 {
		t.Errorf("runtime directory after the close holds %v, want nothing", entries)
	}
}

// With no runtime directory the popup has no socket, and runs as before.
func TestThePopupHasNoSocketWithoutARuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	if ln := listenForSwitch(); ln != nil {
		_ = ln.Close()
		t.Error("listenForSwitch bound a socket with no runtime directory")
	}
}

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
		"-o", "map=ctrl+c copy_and_clear_or_interrupt",
		"-o", "map=ctrl+v paste_from_clipboard",
		"-e", "env", core.PopupEnv + "=demo", "/usr/bin/revier"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if argv := popupArgv("/usr/bin/revier", "", ""); !slices.Contains(argv, core.PopupEnv+"=") {
		t.Errorf("argv with no project = %q, want the marker set and empty", argv)
	}
}

// The popup's kitty has a control socket file only the user can reach,
// whatever kitty.conf names, and remote control is on for it alone. With no
// socket file, kitty.conf decides both:
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
