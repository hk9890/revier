package core

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// Ledger is where the core keeps what it learned at runtime and what it did:
// the one way state reaches the core, and the one way out (decisions.md
// D120). It is storage. The rules - what a launch records, what a landing
// consumes, what a survey settles - are the core's and internal/state's, so
// a second ledger cannot hold a second form of one.
type Ledger interface {
	// State is the state as it is now, and never nil; a store that cannot be
	// read is an empty state. It is a copy of the caller's own: Settle
	// changes the one it reads, and that must not reach the store.
	State() *state.State
	// Update applies a change under the store's lock and returns the state
	// after it. apply reports whether it changed anything.
	Update(apply func(*state.State) bool) *state.State
	// Record appends one event.
	Record(e revier.Event)
}

// state is the state an operation starts from. Each operation reads it once,
// so one operation never sees two states. A core with no ledger remembers
// nothing: every state is empty, and a write is dropped.
func (c *Core) state() *state.State {
	if c.Ledger == nil {
		return &state.State{}
	}
	return c.Ledger.State()
}

func (c *Core) update(apply func(*state.State)) {
	if c.Ledger == nil {
		return
	}
	c.Ledger.Update(func(st *state.State) bool {
		apply(st)
		return true
	})
}

func (c *Core) record(e revier.Event) {
	if c.Ledger != nil {
		c.Ledger.Record(e)
	}
}

// ActivateAgentWaiting is one press of an agent: the agent is brought to the
// front, unless the project is a link whose workspace is still coming up from
// an earlier press. Nothing is written to state: an agent is focused where
// the survey saw it, and a workspace still coming up is left to the press
// that launched it. A press that reached the agent is one EventGoAgent.
func (c *Core) ActivateAgentWaiting(ctx context.Context, p Project, a revier.AgentView) (Result, error) {
	if home, ok := p.Home(); ok && p.Remote != nil {
		st := c.state()
		pending := st.Pending(p.Name, home.Name, time.Now())
		if coming, err := c.comingUp(ctx, p, home.Name, st.Bound[p.Name], pending); err != nil || coming {
			return Result{Target: home.Name, ComingUp: coming}, err
		}
	}
	res, err := c.goAgent(ctx, p, a)
	if err == nil {
		c.record(revier.Event{Kind: revier.EventGoAgent, Project: p.Name, Agent: a.State.Harness, Session: a.State.Session})
	}
	return res, err
}

// Settle is what a survey settles in state, under one write: refs to windows
// the listing no longer holds are pruned; the window that appeared since the
// previous listing is claimed for the last launch (claim-on-appear); a
// launch past its window expires. It returns the state after it.
//
// A ref written since the survey started is to a window the listing may have
// missed, and is kept. A launch written since now was read is kept too:
// another process writes one while this waits for the lock, and it is not
// past its window. The previous listing is the core's own, of the last
// report it settled: with none no window is new, so the first settle of a
// process claims nothing and expires nothing. The lock is taken only when
// there is something to settle, since `revier list` settles on every refresh
// of a surface that links to this machine.
func (c *Core) Settle(r Report, projects []Project, now time.Time) *state.State {
	prev, surveyed := c.lastWindows(r)
	st := c.state()
	// The first pass only asks whether there is something to settle, on a
	// copy nobody keeps, so it says nothing: the pass under the lock does.
	quiet := func(string, ...any) {}
	if c.Ledger == nil || !c.settle(st, r, prev, surveyed, projects, now, quiet) {
		return st
	}
	return c.Ledger.Update(func(st *state.State) bool {
		return c.settle(st, r, prev, surveyed, projects, now, slog.Info)
	})
}

// lastWindows is the window host's listing of the last report settled, and
// whether there was one; r's listing takes its place. A report the window
// host is missing from leaves the listing as it was: a missing listing is not
// an empty one, and the next settle would take every window as new and claim
// one of them.
func (c *Core) lastWindows(r Report) ([]revier.Instance, bool) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	prev, surveyed := c.windows, c.listedWindows
	if c.Window == nil || slices.Contains(r.Hosts, c.Window.Name()) {
		c.windows, c.listedWindows = r.Windows, true
	}
	return prev, surveyed
}

// settle is one pass of Settle over st. say takes the log line of a claim or
// of an expiry.
func (c *Core) settle(st *state.State, r Report, prev []revier.Instance, surveyed bool, projects []Project, now time.Time, say func(msg string, args ...any)) bool {
	changed := st.Prune(r.Hosts, r.Instances, r.before)
	l := st.Launch
	if !surveyed || l == nil {
		return changed
	}
	p, ok := projectNamed(projects, l.Project)
	if !ok {
		return changed
	}
	if ref, ok := c.claim(prev, r.Windows, p, l, now, projects); ok {
		say("claim", "project", p.Name, "target", l.Target, "ref", ref)
		// An attachment records the terminal inside the window as well
		// (decisions.md D95); a binding is of the window itself.
		refs := []revier.TargetRef{ref}
		if l.Target == "" {
			refs = c.attachment(r.Instances, ref)
		}
		st.Claim(l.Target, refs...)
		return true
	}
	if !l.Pending(now) {
		say("claim: launch expired with no window claimed", "project", p.Name, "target", l.Target, "launched_at", l.At)
		st.Launch = nil
		return true
	}
	return changed
}
