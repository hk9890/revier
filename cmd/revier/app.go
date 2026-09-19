package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// out is where a command's own output goes: stdout, or a buffer under
	// test, so a test reads what was printed without swapping the process's
	// stdout. Warnings go to stderr as before.
	out io.Writer
}

func newApp(ctx context.Context, out io.Writer) (*app, error) {
	cfgRoot, err := config.Root()
	if err != nil {
		return nil, err
	}
	cfg, projects, err := config.Load(cfgRoot)
	if err != nil {
		return nil, err
	}
	warnProblems(cfg, projects)
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
		stateRoot: stateRoot, out: out,
		// No keybinder: probing the desktop costs a process, and only
		// `revier keys` reads one. cmdKeys selects it when it is needed.
		core: newCore(cfg, rt, win, nil),
	}, nil
}

// warnProblems says on stderr that some of the configuration did not load,
// and writes each reason to the log. One line, naming the count and where to
// read the rest: the reasons themselves ran to twenty lines on this user's
// machine, and a command that prints them before every keypress is a command
// nobody reads.
//
// `list` is left out, for the reason openLog leaves it out of the log: a
// surface with a linked project runs it on that host every refresh
// (decisions.md D40), and a configuration problem is the same on every one of
// those runs. Its own output already marks the projects concerned.
func warnProblems(cfg *config.Config, projects []core.Project) {
	if invoked == "list" {
		return
	}
	files := 0
	if len(cfg.Problems) > 0 {
		files++
		for _, err := range cfg.Problems {
			slog.Warn("config problem", "file", "config.toml", "err", err)
		}
	}
	for _, p := range projects {
		probs := config.Problems(p)
		if len(probs) == 0 {
			continue
		}
		files++
		for _, err := range probs {
			slog.Warn("config problem", "project", p.Name, "file", p.File, "err", err)
		}
	}
	if files > 0 {
		fmt.Fprintf(os.Stderr, "revier: warning: %s with problems; run `revier doctor`\n", core.Count(files, "configuration file"))
	}
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
	if p, ok := a.resolveHere(); ok {
		return p, nil
	}
	if p, ok := a.resolveAway(ctx); ok {
		return p, nil
	}
	return core.Project{}, errNoProject
}

// resolveHere is step 2: the project owning the working directory, read from
// the files alone.
func (a *app) resolveHere() (core.Project, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("resolve: working directory", "err", err)
		return core.Project{}, false
	}
	p, ok := a.projectForPath(cwd)
	if ok {
		slog.Info("resolve", "project", p.Name, "by", "directory", "dir", cwd)
	}
	return p, ok
}

// resolveAway is step 3 for a command run away from every project
// directory: the focused window, before the remembered project, because a
// desktop binding has no useful working directory, and the project the user
// is looking at beats the one revier last acted on. It lists every host,
// which is why the TUI runs it after its first frame.
func (a *app) resolveAway(ctx context.Context) (core.Project, bool) {
	p, ok, err := a.core.ProjectOfFocused(ctx, a.projects)
	if err != nil {
		slog.Warn("resolve: focused window", "err", err)
	} else if ok {
		slog.Info("resolve", "project", p.Name, "by", "focused window")
		return p, true
	}
	if a.state.Current != "" {
		if p, ok := a.project(a.state.Current); ok {
			slog.Info("resolve", "project", p.Name, "by", "last project")
			return p, true
		}
	}
	return core.Project{}, false
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

// goTarget is the whole run-or-raise for one target, as core.ActivateWaiting
// walks it, with the state on disk as its ledger.
func (a *app) goTarget(ctx context.Context, p core.Project, name revier.TargetName) (revier.TargetRef, error) {
	ref, _, err := a.core.ActivateWaiting(ctx, p, name, nil, a.ledger())
	return ref, err
}

// ledger is the state on disk as an activation reads and writes it: what is
// there now, not what was loaded at startup, since the surface writes claims
// while a command runs.
func (a *app) ledger() core.StateLedger { return core.StateLedger{Root: a.stateRoot} }

// launchedAction records that an action ran, so a window that appears within
// core.ClaimWindow and matches no declared target is attached to the project:
// the link opened from the terminal that claim-on-appear exists for.
func (a *app) launchedAction(p revier.ProjectName) {
	a.update(func(s *state.State) { s.Launched(p, "", time.Now()) })
}

// configRootForMessage is the config root, for a message that has nowhere to
// report an error to.
func configRootForMessage() (string, error) { return config.Root() }
