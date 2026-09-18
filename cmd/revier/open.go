package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/pkg/revier"
)

func cmdOpen(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	attach := fs.Bool("attach", false, "end with this terminal on the workspace")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: revier open [name] [--attach]")
	}
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	// A name revier does not know, typed in a directory, is a project being
	// started: the file is written and the workspace opened in one step, as
	// `os open` does. Without a name there is nothing to call it, and the
	// usual resolution applies.
	if _, known := a.project(revier.ProjectName(name)); name != "" && !known {
		p, err := createProject(a.out, a.cfgRoot, a.projects, revier.ProjectName(name))
		if err != nil {
			return err
		}
		a.projects = append(a.projects, p)
	}
	p, err := a.resolveProject(ctx, name)
	if err != nil {
		return err
	}
	// A project its file refused as a whole is neither cloned nor opened: a
	// git_url the load refused must not reach git (decisions.md D85).
	if p.Invalid != nil {
		return p.Invalid
	}
	home, ok := p.Home()
	if !ok {
		return fmt.Errorf("project %q has no home target", p.Name)
	}
	// A remote project's checkout is its host's to clone: the agent panel
	// opened here runs `revier agent exec` there, and that one clones
	// (decisions.md D84).
	if p.Remote == nil {
		cloned, err := checkout.Ensure(p.Project, os.Stderr)
		if err != nil {
			return err
		}
		if cloned {
			// The clone ran without a deadline. The host calls still need
			// one, and the one set at startup may have been spent waiting
			// for git.
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()
		}
	}
	ref, err := a.goTarget(ctx, p, home.Name)
	if err != nil {
		return err
	}
	if *attach {
		return a.attach(ref)
	}
	fmt.Fprintf(a.out, "%s: %s\n", p.Name, describe(ref))
	return nil
}

// attach ends the command with this terminal on the instance: the process
// becomes the runtime's attach, so the pane that ran `revier open --attach`
// is the workspace from here on. It is what the ssh pane of a remote
// project runs on the host (decisions.md D40). Only a runtime that can
// attach a terminal offers it; a runtime whose instances are windows of
// their own has been raised already, and there is nothing to become.
func (a *app) attach(ref revier.TargetRef) error {
	att, ok := a.core.Runtime.(revier.Attacher)
	if !ok {
		name := "none"
		if a.core.Runtime != nil {
			name = a.core.Runtime.Name()
		}
		return fmt.Errorf("--attach: the %s runtime cannot put a terminal on a workspace; it takes tmux", name)
	}
	if ref.IsZero() {
		return errors.New("--attach: the workspace has not come up yet, so there is nothing to attach to")
	}
	argv, err := att.AttachCommand(ref)
	if err != nil {
		return err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	// The exec replaces the process, so the command's own line is never
	// written: this one stands for it.
	slog.Info("attach", "ref", ref, "argv", argv)
	return syscall.Exec(path, argv, os.Environ())
}

// describe is how a command names the instance it landed on.
func describe(ref revier.TargetRef) string {
	if ref.IsZero() {
		return "launching"
	}
	if ref.Title == "" {
		return ref.Host + "/" + ref.ID
	}
	return fmt.Sprintf("%s (%s/%s)", ref.Title, ref.Host, ref.ID)
}
