package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// curtainCmd is the revier command a curtain's window runs. It is no
// command for the user, and no usage names it.
const curtainCmd = "_curtain"

// cmdCurtain is one curtain: it draws the walk the surface at the socket
// sends, until the socket ends (decisions.md D192). A socket it cannot reach
// ends it at once, and its window with it.
func cmdCurtain(args []string) error {
	fs := flag.NewFlagSet(curtainCmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socket := fs.String("socket", "", "")
	isMain := fs.Bool("main", false, "")
	if err := fs.Parse(args); err != nil || *socket == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: revier %s --socket <path> [--main]", curtainCmd)
	}
	client, err := curtain.Dial(*socket)
	if err != nil {
		return err
	}
	defer client.Close()
	return runSurface(tui.NewCurtain(client, *isMain, curtainTheme()), colourOf(os.Getenv)...)
}

// curtainTheme is the theme config.toml names. A configuration that does not
// read gives the default: a curtain must not fail on it.
func curtainTheme() theme.Theme {
	root, err := config.Root()
	if err != nil {
		return theme.Default()
	}
	cfg, err := config.Read(root)
	if err != nil {
		return theme.Default()
	}
	th, err := cfg.Theme()
	if err != nil {
		return theme.Default()
	}
	return th
}

// curtainArgv is the kitty of the curtain on monitor m, which draws what the
// surface at socket sends; isMain is the curtain that lists the steps. Its
// class is the curtains' own, which no window rule names, and its title
// tells the curtains apart. It has no title bar, no kitty key, no control
// socket and no remote control, whatever kitty.conf says: nothing in it opens
// a window, nothing reaches it but its own socket, and no key of kitty's
// reaches past the curtain. It closes with no question when its program ends.
func curtainArgv(self string, m revier.Monitor, socket string, isMain bool) []string {
	argv := []string{"kitty", "--class", core.CurtainClass, "--title", core.CurtainTitle(m),
		"-o", "remember_window_size=no",
		"-o", "hide_window_decorations=yes",
		"-o", "confirm_os_window_close=0",
		"-o", "clear_all_shortcuts=yes",
		"-o", "background_opacity=1",
		"-o", "listen_on=none",
		"-o", "allow_remote_control=no",
		"-e", self, curtainCmd, "--socket", socket}
	if isMain {
		argv = append(argv, "--main")
	}
	return argv
}
