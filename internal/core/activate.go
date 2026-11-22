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

// activate is one press of a target: for a command target, run its command
// against the last run on record; for any other, run-or-raise it under the
// project's press lock, unless a launch of it by an earlier press, here or
// from another process, is still on record. That press is waiting for the
// window, and a second launch opens a duplicate, so while the target has no
// instance the press does nothing and the Result reports ComingUp. A pending
// launch whose window has appeared is raised like any other.
//
// Every Ref in the Result, also one returned with an error, names an instance
// the activation pins and is on record; a launched Result with a zero Ref is
// a window to bind, and its launch is on record.
func (c *Core) activate(ctx context.Context, p Project, name revier.TargetName, resumes []Resume) (Result, error) {
	// Before comingUp, and with no lock: a command target launches nothing,
	// so a launch of it on record holds no press back, and its command can
	// itself press a target of this project.
	if i, ok := p.index(name); ok && p.isCommand(i) {
		return c.runCommand(ctx, p, i, c.state())
	}
	return c.holdingPress(ctx, p, func(st *state.State, waited bool) (Result, error) {
		bound := st.Bound[p.Name]
		if coming, err := c.comingUp(ctx, p, name, bound, st.Pending(p.Name, name, time.Now())); err != nil || coming {
			return Result{Target: name, ComingUp: coming}, err
		}
		return c.goToResuming(ctx, p, name, bound, resumes, waited)
	})
}

// holdingPress runs one press that can launch under the project's press
// lock, from the state it reads through the write of what the press did
// (decisions.md D195). A press of another process then waits, reads that
// write, lists again and raises the instance. press is told it waited: the
// focus it then finds is the other press's work, so it does not toggle back.
// The wait for a detached launch's window comes after the lock: its launch is
// on record by then, and holds the next press back without it.
func (c *Core) holdingPress(ctx context.Context, p Project, press func(st *state.State, waited bool) (Result, error)) (Result, error) {
	waited := false
	if c.Ledger != nil {
		release, held, err := c.Ledger.HoldPress(ctx, p.Name)
		if err != nil {
			return Result{}, err
		}
		defer release()
		waited = held
	}
	res, err := press(c.state(), waited)
	if res.Launched && res.Ref.IsZero() && err == nil {
		at := time.Now()
		var class string
		if t, ok := p.Target(res.Target); ok && t.Window != nil {
			class = t.Window.Match.Class
		}
		var replaced *state.Launch
		c.update(func(st *state.State) { replaced = st.Launched(p.Name, res.Target, class, at) })
		LogReplacedLaunch(replaced, p.Name, res.Target, at)
	}
	if !res.Ref.IsZero() {
		c.update(func(st *state.State) { st.Landed(p.Name, res.Target, res.Ref) })
	}
	return res, err
}

// comingUp reports whether the launch on record of the target name, when
// pending, has not produced its instance yet. A press or a restore step then
// launches nothing and logs "go: launch still coming up, not launched again"
// with project and target. target is the pressed target, or the home: for
// the second press of a command target, and for an agent press in a linked
// project. A press of any other target writes no "go" line then; the second
// press of a command target writes its "go" line after it, with ran false.
// A check that fails logs "go: is the pending launch up" at Error with
// project, target and err, and the press fails.
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
