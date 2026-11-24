package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A curtain's kitty has the curtains' class, which no window rule names. It has
// the title of its monitor and runs the curtain on the surface's socket, and
// the primary monitor's lists the steps.
func TestCurtainArgvRunsTheCurtainOfItsMonitor(t *testing.T) {
	primary := revier.Monitor{Index: 0, Primary: true}
	argv := curtainArgv("/usr/bin/revier", primary, "/run/user/1000/revier-curtain-7.sock", true)
	want := []string{"kitty", "--class", core.CurtainClass, "--title", core.CurtainTitle(primary),
		"-o", "remember_window_size=no",
		"-o", "hide_window_decorations=yes",
		"-o", "confirm_os_window_close=0",
		"-o", "clear_all_shortcuts=yes",
		"-o", "background_opacity=1",
		"-o", "listen_on=none",
		"-o", "allow_remote_control=no",
		"-e", "/usr/bin/revier", curtainCmd, "--socket", "/run/user/1000/revier-curtain-7.sock", "--main"}
	if !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	other := revier.Monitor{Index: 1}
	argv = curtainArgv("/usr/bin/revier", other, "/s.sock", false)
	if slices.Contains(argv, "--main") || argv[slices.Index(argv, "--title")+1] != core.CurtainTitle(other) {
		t.Errorf("argv of another monitor = %q, want its own title and no --main", argv)
	}
}

// A curtain's kitty has no control socket and no remote control, whatever
// kitty.conf says: nothing reaches it but its own socket.
func TestCurtainArgvHasNoRemoteControl(t *testing.T) {
	argv := curtainArgv("/usr/bin/revier", revier.Monitor{Primary: true}, "/s.sock", true)
	if slices.Contains(argv, "--listen-on") || slices.Contains(argv, "allow_remote_control=socket-only") {
		t.Errorf("argv = %q, want no control socket", argv)
	}
	for _, opt := range []string{"listen_on=none", "allow_remote_control=no"} {
		if i := slices.Index(argv, opt); i < 1 || argv[i-1] != "-o" {
			t.Errorf("argv = %q, want -o %s over kitty.conf", argv, opt)
		}
	}
}

// The curtain is no command for the user: no usage names it.
func TestNoUsageNamesTheCurtain(t *testing.T) {
	if strings.Contains(usage, curtainCmd) {
		t.Errorf("usage names %s", curtainCmd)
	}
}

// A curtain that reaches no socket ends at once, so its window closes.
func TestACurtainWithNoSocketFails(t *testing.T) {
	if err := cmdCurtain([]string{"--socket", t.TempDir() + "/none.sock"}); err == nil {
		t.Error("cmdCurtain with no socket returned no error")
	}
	if err := cmdCurtain(nil); err == nil {
		t.Error("cmdCurtain with no --socket returned no error")
	}
}
