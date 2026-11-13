// Package panelstart words what revier says about a panel whose command does
// not start, so that the tmux, herdr and hc hosts and the core say it the same
// way: a program not on the PATH the runtime starts it with, a command the
// runtime could not start, or a command that exited as soon as it started.
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
	_, err := CommandsOn(panels, path)
	return err
}

// CommandsOn is each panel's command with its program as the file on path
// that StartableOn finds, or nil for a panel with no command, and refuses
// panels as StartableOn does. A program named by a path is kept as written.
func CommandsOn(panels []revier.PanelSpec, path string) ([][]string, error) {
	commands := make([][]string, len(panels))
	for i, p := range panels {
		switch {
		case len(p.Command) == 0:
		case strings.Contains(p.Command[0], "/"):
			commands[i] = p.Command
		default:
			file := lookPath(p.Command[0], path)
			if file == "" {
				return nil, fmt.Errorf("%s command %q is not on PATH", whose(p), p.Command[0])
			}
			commands[i] = append([]string{file}, p.Command[1:]...)
		}
	}
	return commands, nil
}

// lookPath is the first file named program in a directory of path that this
// user may run, or "" when there is none. A relative directory is passed over:
// it names another place in the panel's directory than in this one.
func lookPath(program, path string) string {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		file := filepath.Join(dir, program)
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() && unix.Faccessat(unix.AT_FDCWD, file, unix.X_OK, unix.AT_EACCESS) == nil {
			return file
		}
	}
	return ""
}

// ExitedAtOnce says the panel's command ended as soon as it started.
func ExitedAtOnce(p revier.PanelSpec) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("%s shell exited as soon as it started", whose(p))
	}
	return fmt.Errorf("%s command %q exited as soon as it started", whose(p), strings.Join(p.Command, " "))
}

// DidNotStart says the runtime could not start the panel's command, for the
// reason err gives.
func DidNotStart(p revier.PanelSpec, err error) error {
	if len(p.Command) == 0 {
		return fmt.Errorf("%s shell did not start: %w", whose(p), err)
	}
	return fmt.Errorf("%s command %q did not start: %w", whose(p), strings.Join(p.Command, " "), err)
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
