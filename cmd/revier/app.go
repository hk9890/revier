package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// app is everything a command needs: configuration, persisted state, and a
// core wired to the hosts this machine actually has.
type app struct {
	cfg       *config.Config
	projects  []core.Project
	state     *state.State
	stateRoot string
	core      *core.Core
}

func newApp(ctx context.Context) (*app, error) {
	cfgRoot, err := config.Root()
	if err != nil {
		return nil, err
	}
	cfg, projects, err := config.Load(cfgRoot)
	if err != nil {
		return nil, err
	}
	stateRoot, err := state.Root()
	if err != nil {
		return nil, err
	}
	st, err := state.Load(stateRoot)
	if err != nil {
		return nil, err
	}
	rt, win, err := selectHosts(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &app{
		cfg: cfg, projects: projects, state: st,
		stateRoot: stateRoot, core: newCore(rt, win),
	}, nil
}

func (a *app) project(name revier.ProjectName) (core.Project, bool) {
	for _, p := range a.projects {
		if p.Name == name {
			return p, true
		}
	}
	return core.Project{}, false
}

// resolveProject decides which project a command acts on, in the order a user
// would expect to be obeyed:
//
//  1. an explicit --project,
//  2. the project owning the working directory, which is what makes a command
//     typed in a terminal mean the obvious thing,
//  3. the project revier focused last.
//
// Step 3 is why state exists. A keybinding pressed while the editor is focused
// has no working directory and no focused workspace to read, so without a
// remembered project "go back to the terminal" could not be answered.
func (a *app) resolveProject(explicit string) (core.Project, error) {
	if explicit != "" {
		p, ok := a.project(revier.ProjectName(explicit))
		if !ok {
			return core.Project{}, fmt.Errorf("no project named %q", explicit)
		}
		return p, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if p, ok := a.projectForPath(cwd); ok {
			return p, nil
		}
	}
	if a.state.Current != "" {
		if p, ok := a.project(a.state.Current); ok {
			return p, nil
		}
	}
	return core.Project{}, fmt.Errorf("no project for this directory, and none remembered; pass --project")
}

// projectForPath returns the project whose path contains dir, preferring the
// longest match so a project nested inside another wins.
func (a *app) projectForPath(dir string) (core.Project, bool) {
	best, bestLen := core.Project{}, -1
	for _, p := range a.projects {
		root := filepath.Clean(p.Path)
		if root == "" {
			continue
		}
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			if len(root) > bestLen {
				best, bestLen = p, len(root)
			}
		}
	}
	return best, bestLen >= 0
}

// remember records the project a command acted on, so the next keybinding
// pressed away from a terminal still knows where it is.
func (a *app) remember(p revier.ProjectName) {
	a.state.Current = p
	if err := a.state.Save(a.stateRoot); err != nil {
		// State is a convenience. Losing it costs the next keybinding a
		// fallback, not correctness, so it must not fail the command.
		fmt.Fprintf(os.Stderr, "revier: warning: could not save state: %v\n", err)
	}
}

// configRootForMessage is the config root, for a message that has nowhere to
// report an error to.
func configRootForMessage() (string, error) { return config.Root() }
