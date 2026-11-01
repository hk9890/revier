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
	"time"

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
// so a browser it brought up outlives a run revier gave up on. A child that
// inherited stderr and keeps it open holds the run for WaitDelay at most, and
// a command that exited 0 before then ran well. stdin and stdout are the null
// device; stderr is kept to its last stderrCap bytes, for the line the error
// reports, after ctx's error too when ctx ended the run.
func runTargetCommand(ctx context.Context, argv []string, dir string) error {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.WaitDelay = time.Second
	var stderr tail
	c.Stderr = &stderr
	err := c.Run()
	var exit *exec.ExitError
	switch {
	case err == nil, errors.Is(err, exec.ErrWaitDelay):
		return nil
	case ctx.Err() != nil:
		if line := stderr.lastLine(); line != "" {
			return fmt.Errorf("%w: %s", ctx.Err(), line)
		}
		return ctx.Err()
	case !errors.As(err, &exit):
		return fmt.Errorf("command did not start: %w", err)
	}
	what := "command " + exit.String()
	if code := exit.ExitCode(); code >= 0 {
		what = fmt.Sprintf("command exited %d", code)
	}
	if line := stderr.lastLine(); line != "" {
		return fmt.Errorf("%s: %s", what, line)
	}
	return errors.New(what)
}

// stderrCap is how much of a command's stderr runTargetCommand keeps.
const stderrCap = 4 << 10

// tail is the last stderrCap bytes written to it.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if over := len(t.b) - stderrCap; over > 0 {
		t.b = t.b[over:]
	}
	return len(p), nil
}

// lastLine is the last line that holds more than white space.
func (t *tail) lastLine() string {
	lines := strings.Split(string(t.b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
