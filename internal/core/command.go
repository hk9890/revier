package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// RunWait is how long a command target's run may take when its
// configuration names no timeout. The command raises what is already there
// and detaches anything long-lived itself, so a run that takes longer is one
// that hangs.
const RunWait = 10 * time.Second

// MaxRunWait is the longest timeout a command target may name. A press runs
// inside the bound of a CLI command and the TUI's wait for a press, both
// longer than this, so a run that times out is reported as the run's failure
// and not as theirs.
const MaxRunWait = 30 * time.Second

// RunSettle is how long the core keeps reading focus after a run that left
// it where it was. A command that asks the window host to raise a window can
// exit before the raise lands.
const RunSettle = 500 * time.Millisecond

// errCommand is resolveAt's refusal of a command target: it runs a command
// and has no instance, so every walk over targets fails closed on it.
var errCommand = errors.New("a command target has no instance")

// isCommand reports whether the i-th target runs a command.
func (p Project) isCommand(i int) bool { return p.Targets[i].Command != nil }

// RunTimeout is how long a run of the command may take: its timeout, or
// RunWait when it names none. A timeout that does not parse, is not
// positive, or is longer than MaxRunWait is refused.
func RunTimeout(cmd revier.Command) (time.Duration, error) {
	if cmd.Timeout == "" {
		return RunWait, nil
	}
	d, err := time.ParseDuration(cmd.Timeout)
	switch {
	case err != nil:
		return 0, fmt.Errorf("command timeout %q is no duration; write it as \"10s\"", cmd.Timeout)
	case d <= 0:
		return 0, fmt.Errorf("command timeout %q is not more than zero", cmd.Timeout)
	case d > MaxRunWait:
		return 0, fmt.Errorf("command timeout %q is longer than %s", cmd.Timeout, MaxRunWait)
	}
	return d, nil
}

// runCommand is one press of a command target (decisions.md D166). A second
// press goes home while focus is still on the window the last run of this
// target left it on; any other press runs the command, to its end or its
// timeout. The Result of a run that moved focus names the window that has
// focus now, which the caller records for that second press. A home whose
// launch is still on record is coming up, and that press waits for it as a
// press of home does.
//
// A run writes no launch, waits for no window, and binds nothing, so it is
// its own line of the log: goToResuming, which writes that line for the
// other targets, is not on this path.
func (c *Core) runCommand(ctx context.Context, p Project, i int, bound Bindings, st *state.State) (res Result, err error) {
	name := p.Targets[i].Name
	start := time.Now()
	defer func() {
		logging.Op("go", start, err, "project", p.Name, "target", name, "landed", res.Target, "ran", res.Ran, "focused", res.Focused)
	}()
	// Refused as resolveAt refuses any other target, before anything runs
	// (decisions.md D85).
	if p.Invalid != nil {
		return Result{}, p.Invalid
	}
	if err := p.compiled[i].err; err != nil {
		return Result{}, err
	}
	before, read := c.focusedNow(ctx)
	if home, ok := p.Home(); ok && onRun(st.LastRun, p.Name, name, before) {
		if coming, err := c.comingUp(ctx, p, home.Name, bound, st.Pending(p.Name, home.Name, time.Now())); err != nil || coming {
			return Result{Target: home.Name, ComingUp: coming}, err
		}
		return c.goTo(ctx, p, home.Name, bound)
	}
	if c.Run == nil {
		return Result{}, fmt.Errorf("%s: no command runner is configured", name)
	}
	wait := p.compiled[i].runWait
	runCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if err := c.Run(runCtx, p.Targets[i].Command.Run, p.Path); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			// What the command said follows the deadline's text, as Run
			// returns it.
			said, ok := strings.CutPrefix(err.Error(), context.DeadlineExceeded.Error())
			if !ok {
				said = ""
			}
			return Result{}, fmt.Errorf("%s: command did not end within %s%s", name, wait, said)
		}
		return Result{}, fmt.Errorf("%s: %w", name, err)
	}
	res = Result{Target: name, Ran: true}
	if read {
		// A focus not read before the run cannot tell a run that moved it
		// from one that left it, and a false toggle is worse than none.
		res.Focused = c.focusMoved(ctx, before)
	}
	return res, nil
}

// onRun reports whether the last run on record is of this target of this
// project, and now, the focus the authority reports, is the window that run
// left focus on. A focus that cannot be read is zero, and is not that window:
// a false toggle sends the user home when they asked for the command.
func onRun(last *state.LastRun, p revier.ProjectName, name revier.TargetName, now revier.TargetRef) bool {
	return last != nil && last.Project == p && last.Target == name &&
		!last.Focused.IsZero() && !now.IsZero() && key(now) == key(last.Focused)
}

// focusMoved is the focus after a run when it moved off before, the focus
// before the run, and zero when it stayed. A focus that stayed is read again
// every BindPoll for RunSettle, for a raise that lands after the command
// exits.
func (c *Core) focusMoved(ctx context.Context, before revier.TargetRef) revier.TargetRef {
	if c.focusAuthority() == nil {
		return revier.TargetRef{}
	}
	settled := time.Now().Add(RunSettle)
	for {
		if now, _ := c.focusedNow(ctx); !now.IsZero() && key(now) != key(before) {
			return now
		}
		if !time.Now().Before(settled) {
			return revier.TargetRef{}
		}
		select {
		case <-ctx.Done():
			return revier.TargetRef{}
		case <-time.After(BindPoll):
		}
	}
}

// focusedNow is what the focus authority reports has focus, zero when there
// is none, and false when there is no authority or it cannot say.
func (c *Core) focusedNow(ctx context.Context) (revier.TargetRef, bool) {
	auth := c.focusAuthority()
	if auth == nil {
		return revier.TargetRef{}, false
	}
	ref, err := auth.Focused(ctx)
	if err != nil {
		slog.Warn("go: focused", "host", auth.Name(), "err", err)
		return revier.TargetRef{}, false
	}
	return ref, true
}
