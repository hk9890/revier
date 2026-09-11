package main

import (
	"context"
	"errors"
	"fmt"
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
		core: newCore(cfg, rt, win, nil, projects),
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
// to no project. It has its own exit status so a desktop binding can fall
// back to the picker (contrib/gnome/revier-go).
var errNoProject = errors.New("no project for this directory, for the focused window, and none remembered")

func (a *app) resolveProject(ctx context.Context, explicit string) (core.Project, error) {
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
	// The focused window, before the remembered project: a desktop binding
	// has no useful working directory, and the project the user is looking at
	// beats the one revier last acted on.
	if p, ok, err := a.core.ProjectOfFocused(ctx, a.projects); err == nil && ok {
		return p, nil
	}
	if a.state.Current != "" {
		if p, ok := a.project(a.state.Current); ok {
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
		if p.Host != "" {
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

// update applies a change to state and saves it. It re-reads the file first:
// the TUI writes claims to it while a command runs, and a launch can take
// seconds waiting for a socket, so saving the state loaded at startup would
// overwrite them.
func (a *app) update(apply func(s *state.State)) {
	st, err := state.Load(a.stateRoot)
	if err != nil {
		st = a.state
	}
	apply(st)
	if err := st.Save(a.stateRoot); err != nil {
		// State is a convenience. Losing it costs the next keybinding a
		// fallback, not correctness, so it must not fail the command.
		fmt.Fprintf(os.Stderr, "revier: warning: could not save state: %v\n", err)
	}
}

// bindWait is how long a keypress process waits for the window a detached
// launch produces before giving up and leaving the rest to the TUI. Long,
// because an editor's cold start takes this long and the wait is invisible:
// the process lingers, the user sees the window come up.
const bindWait = 30 * time.Second

// goTarget is the whole run-or-raise for one target: Go with the project's
// bindings, then, for a detached launch, the wait that binds the window and
// raises it. Every ref it lands on is pinned in state, so the next press finds
// the target by id whatever the application has done to its title since.
//
// A second press during the wait must not launch again: the pending launch
// is recorded before waiting, and a press that finds one still inside
// core.BindWindow reports it rather than opening a second window. A window
// that has appeared by then is raised like any other.
func (a *app) goTarget(ctx context.Context, p core.Project, name revier.TargetName) (revier.TargetRef, error) {
	if l := a.state.Launch; l != nil && l.Project == p.Name && l.Target == name && time.Since(l.At) < core.BindWindow {
		// Every binding of a target consumes its launch, so a launch still on
		// record has not landed, whatever an older binding says.
		up, err := a.core.Running(ctx, p, name, a.state.Bound[p.Name])
		if err != nil {
			return revier.TargetRef{}, err
		}
		if !up {
			return revier.TargetRef{}, nil // still coming up; the first press is waiting for it
		}
	}
	res, err := a.core.Go(ctx, p, name, a.state.Bound[p.Name])
	if err != nil {
		return revier.TargetRef{}, err
	}
	landed, ref := res.Target, res.Ref
	if res.Launched && ref.IsZero() {
		at := time.Now()
		a.commit(p.Name, func(s *state.State) {
			s.Launch = &state.Launch{Project: p.Name, Target: landed, At: at}
		})
		inst, ok, err := a.core.Bind(ctx, p, landed, res.Before, bindWait)
		if err != nil {
			return revier.TargetRef{}, err
		}
		if !ok {
			return revier.TargetRef{}, nil // the TUI binds it if it appears later
		}
		ref = inst.Ref
	}
	a.commit(p.Name, func(s *state.State) {
		s.Bind(p.Name, landed, ref)
		if s.Launch != nil && s.Launch.Project == p.Name && s.Launch.Target == landed {
			s.Launch = nil
		}
	})
	return ref, nil
}

// launchedAction records that an action ran, so a window that appears within
// core.ClaimWindow and matches no declared target is attached to the project:
// the link opened from the terminal that claim-on-appear exists for.
func (a *app) launchedAction(p revier.ProjectName) {
	at := time.Now()
	a.commit(p, func(s *state.State) {
		s.Launch = &state.Launch{Project: p, At: at}
	})
}

// configRootForMessage is the config root, for a message that has nowhere to
// report an error to.
func configRootForMessage() (string, error) { return config.Root() }
