package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// BindWait is how long an activation waits for the window a detached launch
// produces. Long, because an editor's cold start takes this long; a keypress
// process lingers for it and the TUI waits off its update loop, so neither
// wait is seen.
// A variable, so a test need not wait this long for a window that never comes.
var BindWait = 30 * time.Second

// activate is one press of a target: run-or-raise it, unless pending says a
// launch of it by an earlier press, here or from another process, is still on
// record. That press is waiting for the window, and a second launch opens a
// duplicate (decisions.md D21), so while the target has no instance the press
// does nothing and the Result reports ComingUp. A pending launch whose window
// has appeared is raised like any other.
//
// Every Ref in the Result, also one returned with an error, names an instance
// the activation pins; a launched Result with a zero Ref is a window to bind.
func (c *Core) activate(ctx context.Context, p Project, name revier.TargetName, bound Bindings, pending bool, resumes []Resume) (Result, error) {
	if coming, err := c.comingUp(ctx, p, name, bound, pending); err != nil || coming {
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
