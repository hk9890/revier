package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

func cmdRun(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: revier run <action> [-p project]")
	}
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	name := pos[0]
	var run []string
	known := p.Remote != nil // a remote project's actions are its host's to know
	for _, act := range a.cfg.Actions {
		if act.Name == name {
			if act.Refused != nil {
				return act.Refused
			}
			run, known = act.Run, true
			break
		}
	}
	if !known {
		// A key bound to nothing must say so, not do nothing.
		return fmt.Errorf("no action named %q", name)
	}
	argv, dir, err := a.core.ActionCommand(p, name, run)
	if err != nil {
		return err
	}
	a.launchedAction(p.Name)
	return runAction(a.out, p.Name, name, argv, dir)
}

// errActionFailed marks an action's own failure, whose exit status revier
// passes on as its own.
var errActionFailed = errors.New("the action failed")

// runAction executes an argv in dir with the terminal attached and its output
// on out, and returns the command's own error so its exit status survives.
// No shell: the argv is a list, so there is nothing to quote and nothing to
// inject into. No context either: the command's 30s deadline is for host
// calls, and an action - an editor, a long pull - runs as long as it runs.
func runAction(out io.Writer, project revier.ProjectName, name string, argv []string, dir string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, out, os.Stderr
	start := time.Now()
	err := c.Run()
	logging.Op("action", start, err, "project", project, "action", name, "argv", argv)
	if err != nil {
		return fmt.Errorf("%w: %w", errActionFailed, err)
	}
	return nil
}
