package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// app is everything a command needs: configuration, persisted state, and a
// core wired to the hosts this machine actually has.
type app struct {
	cfg       *config.Config
	cfgRoot   string
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
		cfg: cfg, cfgRoot: cfgRoot, projects: projects, state: st,
		stateRoot: stateRoot,
		// No keybinder: probing the desktop costs a process, and only
		// `revier keys` reads one. cmdKeys selects it when it is needed.
		core: newCore(cfg, rt, win, nil),
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
// errNoProject is the normal outcome of a keypress on a window that belongs
// to no project. It has its own exit status, and `go --picker` opens the
// popup instead.
var errNoProject = errors.New("no project for this directory, for the focused window, and none remembered")

func (a *app) resolveProject(ctx context.Context, explicit string) (core.Project, error) {
	if explicit != "" {
		p, ok := a.project(revier.ProjectName(explicit))
		if !ok {
			return core.Project{}, fmt.Errorf("no project named %q", explicit)
		}
		slog.Info("resolve", "project", p.Name, "by", "flag")
		return p, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("resolve: working directory", "err", err)
	} else if p, ok := a.projectForPath(cwd); ok {
		slog.Info("resolve", "project", p.Name, "by", "directory", "dir", cwd)
		return p, nil
	}
	// The focused window, before the remembered project: a desktop binding
	// has no useful working directory, and the project the user is looking at
	// beats the one revier last acted on.
	p, ok, err := a.core.ProjectOfFocused(ctx, a.projects)
	if err != nil {
		slog.Warn("resolve: focused window", "err", err)
	} else if ok {
		slog.Info("resolve", "project", p.Name, "by", "focused window")
		return p, nil
	}
	if a.state.Current != "" {
		if p, ok := a.project(a.state.Current); ok {
			slog.Info("resolve", "project", p.Name, "by", "last project")
			return p, nil
		}
	}
	return core.Project{}, errNoProject
}

// projectForPath returns the project whose path contains dir, preferring the
// longest match so a project nested inside another wins.
func (a *app) projectForPath(dir string) (core.Project, bool) {
	best, bestLen := core.Project{}, -1
	for _, p := range a.projects {
		// A remote project's path is on its host; a directory here of the
		// same name is some other checkout.
		if p.Remote != nil {
			continue
		}
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

// commit records the project a command acted on, so the next keybinding
// pressed away from a terminal still knows where it is, plus whatever else
// the command learned.
func (a *app) commit(p revier.ProjectName, apply func(s *state.State)) {
	a.update(func(s *state.State) {
		s.Current = p
		if apply != nil {
			apply(s)
		}
	})
}

// update applies a change to the state on disk. The TUI writes claims to it
// while a command runs, and a launch can take seconds waiting for a socket, so
// the change goes to what is on disk now, not to the state loaded at startup.
func (a *app) update(apply func(s *state.State)) {
	st, err := state.Update(a.stateRoot, func(s *state.State) bool {
		apply(s)
		return true
	})
	if err != nil {
		// State is a convenience. Losing it costs the next keybinding a
		// fallback, not correctness, so it must not fail the command.
		slog.Warn("state update", "err", err)
		fmt.Fprintf(os.Stderr, "revier: warning: could not save state: %v\n", err)
		return
	}
	a.state = st
}

// goTarget is the whole run-or-raise for one target: core.Activate with the
// project's bindings, then, for a detached launch, the wait that binds the
// window. Every ref it lands on is pinned in state, so the next press finds
// the target by id whatever the application has done to its title since.
func (a *app) goTarget(ctx context.Context, p core.Project, name revier.TargetName) (revier.TargetRef, error) {
	ref, _, err := a.goTargetResuming(ctx, p, name, nil)
	return ref, err
}

// goTargetResuming is goTarget with the agent panels of a launch started on
// the conversations a saved session recorded for them. It also returns what
// the launch did with each recorded agent, in the Result, also when the wait
// after the launch failed. A zero ref with no error is a target launched and
// not yet up.
func (a *app) goTargetResuming(ctx context.Context, p core.Project, name revier.TargetName, resumes []core.Resume) (revier.TargetRef, core.Result, error) {
	pending := a.state.Launch.Pending(p.Name, name, core.BindWindow)
	res, err := a.core.Activate(ctx, p, name, a.state.Bound[p.Name], pending, resumes)
	landed, ref := res.Target, res.Ref
	if res.Launched && ref.IsZero() && err == nil {
		// Recorded before the wait, so a second press during it does not
		// launch again.
		a.update(func(s *state.State) { s.Launched(p.Name, landed, time.Now()) })
		var inst revier.Instance
		var ok bool
		inst, ok, err = a.core.Bind(ctx, p, landed, res.Before, core.BindWait)
		if !ok {
			// With no error, the TUI binds the window if it appears later. With
			// one, the launch ran, and its agents came to res.Agents.
			return revier.TargetRef{}, res, err
		}
		ref = inst.Ref
	}
	if !ref.IsZero() {
		a.update(func(s *state.State) { s.Landed(p.Name, landed, ref) })
	}
	return ref, res, err
}

// launchedAction records that an action ran, so a window that appears within
// core.ClaimWindow and matches no declared target is attached to the project:
// the link opened from the terminal that claim-on-appear exists for.
func (a *app) launchedAction(p revier.ProjectName) {
	a.update(func(s *state.State) { s.Launched(p, "", time.Now()) })
}

// configRootForMessage is the config root, for a message that has nowhere to
// report an error to.
func configRootForMessage() (string, error) { return config.Root() }
