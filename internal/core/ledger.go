package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// StateLedger is the state file as an activation reads and writes it: the
// one ledger the command line, the surface and a restore share, so the
// launch rule (decisions.md D21) is written once. Every read is of the file
// as it is now, under its lock, because another process writes launches to
// it - a desktop key while the surface runs - and a press must find them.
// Written, when set, is told of every write, so a surface can take the state
// into itself before its next survey does.
type StateLedger struct {
	Root    string
	Written chan<- struct{}
}

func (l StateLedger) Bound(p revier.ProjectName) Bindings { return l.load().Bound[p] }

func (l StateLedger) Pending(p revier.ProjectName, t revier.TargetName) bool {
	return l.load().Launch.Pending(p, t, BindWindow)
}

func (l StateLedger) Launched(p revier.ProjectName, t revier.TargetName, at time.Time) {
	l.update(func(st *state.State) { st.Launched(p, t, at) })
}

func (l StateLedger) Landed(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef) {
	l.update(func(st *state.State) { st.Landed(p, t, ref) })
}

// load is the state on disk, or an empty one: state is a convenience, and
// losing it costs a binding, not the activation.
func (l StateLedger) load() *state.State {
	st, err := state.Load(l.Root)
	if err != nil {
		slog.Warn("ledger: state load", "err", err, "root", l.Root)
		return &state.State{}
	}
	return st
}

func (l StateLedger) update(apply func(st *state.State)) {
	if _, err := state.Update(l.Root, func(st *state.State) bool {
		apply(st)
		return true
	}); err != nil {
		slog.Warn("ledger: state update", "err", err, "root", l.Root)
	}
	if l.Written == nil {
		return
	}
	// A write still waiting to be read already stands for this one: the
	// reader takes the whole state again.
	select {
	case l.Written <- struct{}{}:
	default:
	}
}

// ActivateAgentWaiting is ActivateAgent with the ledger: the project's
// bindings and whether its home is still coming up are the ledger's, and the
// instance the agent's panel is in is recorded as where its target landed.
func (c *Core) ActivateAgentWaiting(ctx context.Context, p Project, a revier.AgentView, l Ledger) (Result, error) {
	homePending := false
	if home, ok := p.Home(); ok {
		homePending = l.Pending(p.Name, home.Name)
	}
	res, err := c.ActivateAgent(ctx, p, a, l.Bound(p.Name), homePending)
	if res.Target != "" && !res.Ref.IsZero() {
		l.Landed(p.Name, res.Target, res.Ref)
	}
	return res, err
}

// Settle is what a survey settles in state, under one write: refs to windows
// the listing no longer holds are pruned; the window that appeared since the
// previous listing is claimed for the last launch (claim-on-appear); a
// launch past its window expires. before is the state as it was when the
// survey started, so a ref written since is to a window the listing may
// have missed and is kept. prev is the window host's previous listing, and
// surveyed whether there was one: with no previous listing no window is
// new, so nothing is claimed and nothing expires. It reports whether state
// changed.
func (c *Core) Settle(st, before *state.State, r Report, prev []revier.Instance, surveyed bool, projects []Project, now time.Time) bool {
	changed := st.Prune(r.Hosts, r.Instances, before)
	if !surveyed || st.Launch == nil {
		return changed
	}
	p, ok := projectNamed(projects, st.Launch.Project)
	if !ok {
		return changed
	}
	l := Launch{Project: p, Target: st.Launch.Target, At: st.Launch.At}
	if claimed, ok := c.Claim(prev, r.Windows, l, now, projects); ok {
		slog.Info("claim", "project", l.Project.Name, "target", claimed.Target, "ref", claimed.Ref)
		st.Claim(claimed.Target, claimed.Ref)
		return true
	}
	if !l.Pending(now) {
		slog.Info("claim: launch expired with no window claimed", "project", l.Project.Name, "target", l.Target, "launched_at", l.At)
		st.Launch = nil
		return true
	}
	return changed
}
