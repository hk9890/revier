package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	usecase "github.com/hk9890/revier/internal/app"
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
	act, err := usecase.PlanAction(a.core, p, name, run)
	if err != nil {
		return err
	}
	err = runAction(a.out, act.Argv, act.Dir)
	act.Done(err)
	if err != nil {
		return fmt.Errorf("%w: %w", errActionFailed, err)
	}
	return nil
}

// errActionFailed marks an action's own failure, whose exit status revier
// passes on as its own.
var errActionFailed = errors.New("the action failed")

// runAction executes an argv in dir with stdin and stderr attached and its
// stdout on out, and returns the command's own error so its exit status
// survives. In production out is the terminal, handed to the action as its
// own file; a writer that is no file, a test's buffer, is a pipe the action
// writes and exec drains, so Run ends only once every process holding it has
// closed it.
// No shell: the argv is a list, so there is nothing to quote and nothing to
// inject into. No context either: the command's 30s deadline is for host
// calls, and an action - an editor, a long pull - runs as long as it runs.
func runAction(out io.Writer, argv []string, dir string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, out, os.Stderr
	return c.Run()
}

// runTargetCommand runs a command target's argv in dir to its end, for the
// core (core.Core.Run). The command is the user's launcher: it raises what it
// shows and detaches what lives on, so revier waits for it and reads its exit
// status.
//
// It runs in a session of its own, so a ctrl+c in the terminal of the TUI
// does not reach it. ctx ending kills the command alone, not what it started,
// so a browser it brought up outlives a run revier gave up on. stdin and
// stdout are the null device. stderr is an unlinked temporary file and no
// pipe, so the run ends when the command does, though a child it detached
// holds stderr open, and that child can write to it for as long as it lives.
// The error reports the last line of stderr, after ctx's error when ctx
// killed the command.
func runTargetCommand(ctx context.Context, argv []string, dir string) error {
	stderr, err := os.CreateTemp("", "revier-run-*")
	if err != nil {
		return fmt.Errorf("command did not start: %w", err)
	}
	defer func() { _ = stderr.Close() }()
	if err := os.Remove(stderr.Name()); err != nil {
		return fmt.Errorf("command did not start: %w", err)
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Stderr = stderr
	err = c.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case !errors.As(err, &exit):
		return fmt.Errorf("command did not start: %w", err)
	case ctx.Err() != nil && exit.ExitCode() < 0:
		// A signal ended the command after ctx ended: ctx killed it. A
		// command that exited with a status as ctx ended reports the status.
		if line := lastLine(stderr); line != "" {
			return fmt.Errorf("%w: %s", ctx.Err(), line)
		}
		return ctx.Err()
	}
	what := "command " + exit.String()
	if code := exit.ExitCode(); code >= 0 {
		what = fmt.Sprintf("command exited %d", code)
	}
	if line := lastLine(stderr); line != "" {
		return fmt.Errorf("%s: %s", what, line)
	}
	return errors.New(what)
}

// stderrCap is how much of the end of a command's stderr runTargetCommand
// reads.
const stderrCap = 4 << 10

// lastLine is the last line in the last stderrCap bytes of f that holds more
// than white space. A carriage return ends a line too, as a redrawn progress
// line does.
func lastLine(f *os.File) string {
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	off := max(0, info.Size()-stderrCap)
	b := make([]byte, info.Size()-off)
	n, _ := f.ReadAt(b, off)
	lines := strings.FieldsFunc(string(b[:n]), func(r rune) bool { return r == '\n' || r == '\r' })
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
