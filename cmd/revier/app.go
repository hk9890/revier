package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// app is everything a command needs: configuration, the state root, and a
// core wired to the hosts this machine actually has and to the ledger of its
// state.
type app struct {
	cfg       *config.Config
	cfgRoot   string
	projects  []core.Project
	assistant core.Project // the built-in, listed or not: a file of its name may take its place
	stateRoot string
	core      *core.Core
	// out is where a command's own output goes: stdout, or a buffer under
	// test, so a test reads what was printed without swapping the process's
	// stdout. Warnings go to stderr as before.
	out io.Writer
}

func newApp(ctx context.Context, out io.Writer) (*app, error) {
	return newAppAt(ctx, out, "")
}

// newAppAt is newApp for a command a terminal key runs in panel: its runtime
// is the one that holds the panel, where the panel id says which that is.
func newAppAt(ctx context.Context, out io.Writer, panel string) (*app, error) {
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
	assistant := withAssistant(&projects, cfgRoot, stateRoot)
	rt, win, err := selectHosts(ctx, cfg, panel)
	if err != nil {
		return nil, err
	}
	// No keybinder: probing the desktop costs a process, and only
	// `revier keys` reads one. cmdKeys selects it when it is needed.
	c := newCore(cfg, rt, win, nil)
	c.Ledger = ledger.File{Root: stateRoot, Failed: warnState}
	return &app{
		cfg: cfg, cfgRoot: cfgRoot, projects: projects, assistant: assistant,
		stateRoot: stateRoot, out: out, core: c,
	}, nil
}

// withAssistant adds the built-in assistant to projects, unless a file of
// its name is there, and returns it either way: the surface takes it back
// into the list when that file goes. The assistant lives under the state
// root, which is a convenience: a root it cannot be made in costs the
// built-in, not the command, and the zero project comes back. alt+a then
// hands the terminal over, and `revier assist` says why.
func withAssistant(projects *[]core.Project, cfgRoot, stateRoot string) core.Project {
	builtin, err := assistProject(cfgRoot, stateRoot)
	if err != nil {
		slog.Warn("built-in assistant left out", "err", err)
		return core.Project{}
	}
	*projects = config.WithBuiltin(*projects, builtin)
	return builtin
}

// assistProject is the built-in assistant, with the directory it starts in
// made: core.Open refuses a project whose directory is not there.
//
// Its panel runs with both roots this command resolved. A runtime starts a
// panel in its own environment, not in the surface's, so a root the surface
// was given would otherwise be lost to the agent.
func assistProject(cfgRoot, stateRoot string) (core.Project, error) {
	self, err := os.Executable()
	if err != nil {
		return core.Project{}, err
	}
	// env reads a word with "=" in it as one more variable to set.
	if strings.Contains(self, "=") {
		return core.Project{}, fmt.Errorf("the revier binary's path %s has a \"=\" in it, which env cannot run", self)
	}
	// The panel starts in the directory, so a relative root would name
	// another place there.
	if cfgRoot, err = filepath.Abs(cfgRoot); err != nil {
		return core.Project{}, err
	}
	if stateRoot, err = filepath.Abs(stateRoot); err != nil {
		return core.Project{}, err
	}
	dir := filepath.Join(stateRoot, "assist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return core.Project{}, err
	}
	return config.Builtin(dir, []string{
		"env", "REVIER_CONFIG_HOME=" + cfgRoot, "REVIER_STATE_HOME=" + stateRoot,
		self, "assist", "--exec",
	}), nil
}

// warnState says that a change did not reach the state file. State is a
// convenience: losing it costs the next keybinding a fallback, not
// correctness, so it does not fail the command.
func warnState(err error) {
	fmt.Fprintf(os.Stderr, "revier: warning: could not save state: %v\n", err)
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

// resolveProject is the project a command acts on: the one -p names, else
// the one of the working directory, else the one of the focused window, else
// the last one (core.Core.ProjectFor).
func (a *app) resolveProject(ctx context.Context, explicit string) (core.Project, error) {
	return a.core.ProjectFor(ctx, a.projects, core.Ask{Name: revier.ProjectName(explicit), Dir: workingDir(), Away: true})
}

// workingDir is the directory the command runs in, or "" when it cannot be
// read: the step that needs it is skipped.
func workingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		slog.Warn("resolve: working directory", "err", err)
		return ""
	}
	return cwd
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

// update applies a change to the state through the core's ledger. The TUI
// writes claims to it while a command runs, and a launch can take seconds
// waiting for a socket, so the change goes to what is on disk now.
func (a *app) update(apply func(s *state.State)) {
	a.core.Ledger.Update(func(s *state.State) bool {
		apply(s)
		return true
	})
}

// goTarget is the whole press of one target, as core.ActivateWaiting walks
// it: the ref it landed on, and what the press did.
func (a *app) goTarget(ctx context.Context, p core.Project, name revier.TargetName) (revier.TargetRef, core.Result, error) {
	return a.core.ActivateWaiting(ctx, p, name, nil)
}

// configRootForMessage is the config root, for a message that has nowhere to
// report an error to.
func configRootForMessage() (string, error) { return config.Root() }
