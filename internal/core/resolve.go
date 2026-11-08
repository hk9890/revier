package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// ErrNoProject is the normal outcome of a keypress on a window that belongs
// to no project, and of a tab asked for from a directory in no project. It
// has its own exit status, and `go --picker` opens the popup instead.
var ErrNoProject = errors.New("no project for this directory, for the focused window, and none remembered")

// Ask is what a command knows when it asks which project it acts on.
type Ask struct {
	// Name is the project named by the user, and when set the only step.
	Name revier.ProjectName
	// Dir is a directory on this machine: the project is the one whose path
	// holds it. The caller reads it, so the core reads no working directory.
	Dir string
	// Away lets a command go on past Dir: to the project of the focused
	// window, then to the project revier focused last.
	Away bool
}

// ProjectFor decides which project a command acts on, in the order a user
// would expect to be obeyed:
//
//  1. the project the user named,
//  2. the project owning the directory, which is what makes a command typed
//     in a terminal mean the obvious thing,
//  3. with Away, the project of the focused window, before the project revier
//     focused last.
//
// Step 3 is why state exists. A keybinding pressed while the editor is focused
// has no working directory, so without the focused window or a remembered
// project "go back to the terminal" could not be answered. The project the
// user is looking at beats the one revier last acted on. The focused window
// lists every host, which is why the TUI asks after its first frame.
func (c *Core) ProjectFor(ctx context.Context, projects []Project, ask Ask) (Project, error) {
	if ask.Name != "" {
		p, ok := projectNamed(projects, ask.Name)
		if !ok {
			return Project{}, fmt.Errorf("no project named %q", ask.Name)
		}
		slog.Info("resolve", "project", p.Name, "by", "flag")
		return p, nil
	}
	if ask.Dir != "" {
		if p, ok := projectAt(projects, ask.Dir); ok {
			slog.Info("resolve", "project", p.Name, "by", "directory", "dir", ask.Dir)
			return p, nil
		}
	}
	if !ask.Away {
		if ask.Dir == "" {
			return Project{}, ErrNoProject
		}
		return Project{}, noProjectAt(ask.Dir)
	}
	p, ok, err := c.ProjectOfFocused(ctx, projects)
	if err != nil {
		slog.Warn("resolve: focused window", "err", err)
	} else if ok {
		slog.Info("resolve", "project", p.Name, "by", "focused window")
		return p, nil
	}
	if current := c.state().Current; current != "" {
		if p, ok := projectNamed(projects, current); ok {
			slog.Info("resolve", "project", p.Name, "by", "last project")
			return p, nil
		}
	}
	return Project{}, ErrNoProject
}

// noProjectAt is a directory in no project, asked for without Away: a tab is
// opened from a terminal or a script, which has a directory, so one in no
// project fails, and never opens the tab in the project of the focused window
// or the last one (decisions.md D160). It is ErrNoProject, so it exits with
// that status, but says which directory and what to give instead.
type noProjectAt string

func (d noProjectAt) Error() string {
	return string(d) + " is in no project; name one with -p <project>"
}

func (d noProjectAt) Unwrap() error { return ErrNoProject }

// projectAt returns the project whose path contains dir, preferring the
// longest match so a project nested inside another wins. It compares the paths
// with every symlink resolved: a shell keeps the path through a link, a tmux
// window starts in the one behind it. Where that finds none, it compares them
// as text, which still places a directory that does not exist yet.
func projectAt(projects []Project, dir string) (Project, bool) {
	if p, ok := projectContaining(projects, realPath(dir), realPath); ok {
		return p, true
	}
	return projectContaining(projects, filepath.Clean(dir), filepath.Clean)
}

// projectContaining is projectAt with every project's path in one form.
func projectContaining(projects []Project, dir string, form func(string) string) (Project, bool) {
	best, bestLen := Project{}, -1
	for _, p := range projects {
		// A remote project's path is on its host; a directory here of the
		// same name is some other checkout.
		if p.Remote != nil {
			continue
		}
		root := form(p.Path)
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			if len(root) > bestLen {
				best, bestLen = p, len(root)
			}
		}
	}
	return best, bestLen >= 0
}

// realPath is path with every symlink resolved, or path cleaned when it
// cannot be resolved.
func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
