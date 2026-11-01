package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// BindWait is how long an activation waits for the window a detached launch
// produces. Long, because an editor's cold start takes this long; a keypress
// process lingers for it and the TUI waits off its update loop, so neither
// wait is seen.
// A variable, so a test need not wait this long for a window that never comes.
var BindWait = 30 * time.Second

// activate is one press of a target, judged against st: for a command
// target, run its command against the last run on record; for any other,
// run-or-raise it, unless a launch of it by an earlier press, here or from
// another process, is still on record. That press is waiting for the window,
// and a second launch opens a duplicate, so while the target has no instance
// the press does nothing and the Result reports ComingUp. A pending launch
// whose window has appeared is raised like any other.
//
// Every Ref in the Result, also one returned with an error, names an instance
// the activation pins; a launched Result with a zero Ref is a window to bind.
func (c *Core) activate(ctx context.Context, p Project, name revier.TargetName, st *state.State, resumes []Resume) (Result, error) {
	bound := st.Bound[p.Name]
	// Before comingUp: a command target launches nothing, so a launch of it
	// on record is from before it ran a command, and holds no press back.
	if i, ok := p.index(name); ok && p.isCommand(i) {
		return c.runCommand(ctx, p, i, bound, st)
	}
	if coming, err := c.comingUp(ctx, p, name, bound, st.Pending(p.Name, name, time.Now())); err != nil || coming {
		return Result{Target: name, ComingUp: coming}, err
	}
	return c.goToResuming(ctx, p, name, bound, resumes)
}

func (c *Core) comingUp(ctx context.Context, p Project, name revier.TargetName, bound Bindings, pending bool) (bool, error) {
	if !pending {
		return false, nil
	}
	// Every binding of a target consumes its launch, so a launch still on
	// record has not landed, whatever an older binding says.
	up, err := c.isUp(ctx, p, name, bound)
	if err != nil {
		slog.Error("go: is the pending launch up", "project", p.Name, "target", name, "err", err)
		return false, err
	}
	if !up {
		slog.Info("go: launch still coming up, not launched again", "project", p.Name, "target", name)
	}
	return !up, nil
}
