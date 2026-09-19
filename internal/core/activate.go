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

// Activate is one press of a target: run-or-raise it, unless pending says a
// launch of it by an earlier press, here or from another process, is still on
// record. That press is waiting for the window, and a second launch opens a
// duplicate (decisions.md D21), so while the target has no instance the press
// does nothing and the Result reports ComingUp. A pending launch whose window
// has appeared is raised like any other.
//
// Every Ref in the Result, also one returned with an error, names an instance
// the caller pins; a launched Result with a zero Ref is a window to Bind.
func (c *Core) Activate(ctx context.Context, p Project, name revier.TargetName, bound Bindings, pending bool, resumes []Resume) (Result, error) {
	if coming, err := c.comingUp(ctx, p, name, bound, pending); err != nil || coming {
		return Result{Target: name, ComingUp: coming}, err
	}
	return c.GoResuming(ctx, p, name, bound, resumes)
}

// ActivateAgent is Activate for an agent: GoAgent, unless the project is a
// link whose workspace is still coming up from an earlier press, which
// homePending reports.
func (c *Core) ActivateAgent(ctx context.Context, p Project, a revier.AgentView, bound Bindings, homePending bool) (Result, error) {
	if home, ok := p.Home(); ok && p.Remote != nil {
		if coming, err := c.comingUp(ctx, p, home.Name, bound, homePending); err != nil || coming {
			return Result{Target: home.Name, ComingUp: coming}, err
		}
	}
	return c.GoAgent(ctx, p, a, bound)
}

func (c *Core) comingUp(ctx context.Context, p Project, name revier.TargetName, bound Bindings, pending bool) (bool, error) {
	if !pending {
		return false, nil
	}
	// Every binding of a target consumes its launch, so a launch still on
	// record has not landed, whatever an older binding says.
	up, err := c.Running(ctx, p, name, bound)
	if err != nil {
		slog.Error("go: is the pending launch up", "project", p.Name, "target", name, "err", err)
		return false, err
	}
	if !up {
		slog.Info("go: launch still coming up, not launched again", "project", p.Name, "target", name)
	}
	return !up, nil
}
