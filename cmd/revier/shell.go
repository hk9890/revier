package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const shellUsage = `revier shell - open a shell in a workspace

usage:
  revier shell new [-p <project>[:<target>] | --panel <id>] [--dir <path>]

  --panel    the open workspace that holds this panel; kitty's
             @active-kitty-window-id is the window a key was pressed in
  --dir      start the shell here, not where the target's shell starts

new opens a tab in an open workspace: the target's shell panel, or the
runtime's shell where it declares none. The target is the home target unless
-p names one. For the pane onto a project on another machine the tab opens
there, and --dir, a path on this machine, is not sent.
`

func cmdShell(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		fmt.Print(shellUsage)
		return nil
	case "new":
		return cmdShellNew(args)
	default:
		fmt.Fprint(os.Stderr, shellUsage)
		return fmt.Errorf("unknown shell command %q", sub)
	}
}

// cmdShellNew adds a shell tab to an open workspace. Like `revier agent new`
// it is what a kitty key runs, so it prints nothing on success.
func cmdShellNew(args []string) error {
	fs := flag.NewFlagSet("shell new", flag.ContinueOnError)
	project := projectFlag(fs)
	panel := fs.String("panel", "", "the open workspace holding this panel")
	dir := fs.String("dir", "", "the directory the shell starts in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*project != "" && *panel != "") {
		return errors.New("usage: revier shell new [-p <project>[:<target>] | --panel <id>] [--dir <path>]")
	}
	if *dir != "" {
		if *dir, err = filepath.Abs(*dir); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	a, err := newApp(ctx)
	if err != nil {
		return err
	}
	return a.newShell(ctx, *project, *panel, *dir)
}

// newShell opens the shell tab where newTab puts it.
func (a *app) newShell(ctx context.Context, project, panel, dir string) error {
	there := func(r revier.Remote, address string) error { return r.NewShell(ctx, address) }
	here := func(w core.Workspace) error {
		err := a.core.NewShell(ctx, w, dir)
		slog.Info("shell new", "project", w.Project.Name, "target", w.Target, "ref", w.Ref, "dir", dir, "err", err)
		return err
	}
	return a.newTab(ctx, "shell new", project, panel, dir, homeTarget, there, here)
}

// homeTarget is the target `revier shell new -p <project>` opens its tab in.
// Every project has one: a load refuses a project without it.
func homeTarget(p core.Project) (revier.TargetName, error) {
	home, _ := p.Home()
	return home.Name, nil
}
