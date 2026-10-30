// Package panelstart words what revier says about a panel whose command does
// not start, so that the tmux and herdr hosts and the core say it the same
// way: a program not on the PATH the runtime starts it with, or a command that
// exited as soon as it started.
package panelstart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/hk9890/revier/pkg/revier"
)

// Startable is StartableOn with this process's PATH, which is the one tmux
// starts a pane with: a pane takes the PATH of the client that asks for it.
func Startable(panels []revier.PanelSpec) error {
	return StartableOn(panels, os.Getenv("PATH"))
}

// StartableOn refuses panels whose program is not on path, the PATH the
// runtime starts them with, before it starts any of them. The runtime starts
// it anyway, the pane closes at once, and what the runtime reports names
// neither the panel nor its command. A program named by a path is left to the
// runtime: it is read against the panel's directory, not this one.
func StartableOn(panels []revier.PanelSpec, path string) error {
	for _, p := range panels {
		if len(p.Command) == 0 || strings.Contains(p.Command[0], "/") {
			continue
		}
		if !onPath(p.Command[0], path) {
			return fmt.Errorf("%s command %q is not on PATH", whose(p), p.Command[0])
		}
	}
	return nil
}

// onPath reports whether a directory of path holds program as a file this
// user may run. A relative directory is passed over: it names another place in
// the panel's directory than in this one.
func onPath(program, path string) bool {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		file := filepath.Join(dir, program)
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() && unix.Faccessat(unix.AT_FDCWD, file, unix.X_OK, unix.AT_EACCESS) == nil {
			return true
		}
	}
	return false
}

// ExitedAtOnce says the panel's command ended as soon as it started.
func ExitedAtOnce(p revier.PanelSpec) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("%s shell exited as soon as it started", whose(p))
	}
	return fmt.Errorf("%s command %q exited as soon as it started", whose(p), strings.Join(p.Command, " "))
}

// whose names what a command belongs to: a panel by its kind. A realization's
// launch has no kind, and neither has a panel whose kind was left out, so
// either is named by its command alone.
func whose(p revier.PanelSpec) string {
	if p.Kind == "" {
		return "the"
	}
	return "the " + string(p.Kind) + " panel's"
}
