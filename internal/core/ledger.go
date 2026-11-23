package core

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// Ledger is where the core keeps what it learned at runtime and what it did:
// the one way state reaches the core, and the one way out. It is storage. The
// rules - what a launch records, what a landing consumes, what a survey settles
// - are the core's and internal/state's, so a second ledger cannot hold a
// second form of one. A method for each rule would make every fake write the
// rule again.
type Ledger interface {
	// State is the state as it is now, and never nil; a store that cannot be
	// read is the state it held last, or an empty one. It is a copy of the
	// caller's own: Settle changes the one it reads, and that must not reach
	// the store.
	State() *state.State
	// Update applies a change under the store's lock and returns the state
	// after it. apply reports whether it changed anything.
	Update(apply func(*state.State) bool) *state.State
	// HoldPress waits for the project's press lock and returns its release,
	// and whether another press held it first. One press that can launch
	// holds it from its listing through the write of what it did. It fails only
	// when ctx ends first.
	HoldPress(ctx context.Context, p revier.ProjectName) (release func(), waited bool, err error)
	// Record appends one event.
	Record(e revier.Event)
	// Note appends one line to the state record: what an operation did,
	// failed or not, or what a save saw. A line with no By is one an
	// operation did (decisions/0151).
	Note(l record.Line)
	PopupMarks
}

// PopupMarks is the part of a ledger that keeps the mark of a press that raised the
// popup from minimized, until the popup's surface takes it at the focus the
// raise gives it. The mark is on the raise's path, so it
// is kept apart from the state, whose save is made to last.
type PopupMarks interface {
	MarkPopupHidden() error
	// TakePopupHidden reports whether the mark was there, and removes it.
	TakePopupHidden() bool
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

func (c *Core) note(l record.Line) {
	if c.Ledger != nil {
		c.Ledger.Note(l)
	}
}

// ActivateAgentWaiting is one press of an agent: the agent is brought to the
// front, unless the project is a link whose workspace is still coming up from
// an earlier press. Nothing is written to state: an agent is focused where
// the survey saw it, and a workspace still coming up is left to the press
// that launched it.
func (c *Core) ActivateAgentWaiting(ctx context.Context, p Project, a revier.AgentView) (Result, error) {
	if home, ok := p.Home(); ok && p.Remote != nil {
		st := c.state()
		pending := st.Pending(p.Name, home.Name, time.Now())
		if coming, err := c.comingUp(ctx, p, home.Name, st.Bound[p.Name], pending); err != nil || coming {
			return Result{Target: home.Name, ComingUp: coming}, err
		}
	}
	return c.goAgent(ctx, p, a)
}

// Settle is what a survey settles in state, under one write: refs to windows
// the listing no longer holds are pruned; the window that appeared since the
// previous listing is claimed for the last launch (claim-on-appear); a
// launch past its window expires, into the late launches when it is a
// target's; a late launch is bound to the window it awaits, or ends past
// LateWindow. It returns the state after it.
//
// A ref written since the survey started is to a window the listing may have
// missed, and is kept. A launch written since now was read is kept too:
// another process writes one while this waits for the lock, and it is not
// past its window. The previous listing is the core's own, of the last
// report it settled: with none no window is new, so the first settle of a
// process claims nothing and expires nothing of the launch on record; a late
// launch needs no previous listing, as its window need not be new. The lock
// is taken only when there is something to settle, since `revier list`
// settles on every refresh of a surface that links to this machine.
func (c *Core) Settle(r Report, projects []Project, now time.Time) *state.State {
	prev, surveyed := c.lastWindows(r, projects)
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
// whether there was one; r's listing takes its place, and projects the place
// of the projects settled. A report the window host is missing from leaves
// the listing as it was: a missing listing is not an empty one, and the next
// settle would take every window as new and claim one of them.
func (c *Core) lastWindows(r Report, projects []Project) ([]revier.Instance, bool) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	c.settled = projects
	prev, surveyed := c.windows, c.listedWindows
	if c.Window == nil || slices.Contains(r.Hosts, c.Window.Name()) {
		c.windows, c.listedWindows = r.Windows, true
	}
	return prev, surveyed
}

// settledProjects is the projects of the last report settled, or nil before
// the first.
func (c *Core) settledProjects() []Project {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	return c.settled
}

// LogReplacedLaunch writes the line of a launch still pending that the launch
// of p's target t, at at, replaced. A target's launch is set aside as late,
// and its window is still bound when it comes; an action's is lost, because
// the one launch on record is the only one a window is attached for.
//
// The line is "claim: launch set aside" for a target's launch, kept until
// LateWindow after launched_at (decisions/0190), and
// "claim: launch replaced" for an action's. project, target and launched_at are
// the replaced launch's, by_project and by_target the new one's, and age_ms is
// the replaced launch's age at at. A target's launch past BindWindow, still
// kept, that another replaces before a settle set it aside is set aside with no
// line: Launched returns only a launch still pending.
func LogReplacedLaunch(replaced *state.Launch, p revier.ProjectName, t revier.TargetName, at time.Time) {
	if replaced == nil {
		return
	}
	msg := "claim: launch replaced"
	if replaced.Target != "" {
		msg = "claim: launch set aside"
	}
	slog.Info(msg, "project", replaced.Project, "target", replaced.Target, "launched_at", replaced.At,
		"by_project", p, "by_target", t, "age_ms", at.Sub(replaced.At).Milliseconds())
}

// settle is one pass of Settle over st. say takes the log line of a dropped
// binding, of a claim or of an expiry.
func (c *Core) settle(st *state.State, r Report, prev []revier.Instance, surveyed bool, projects []Project, now time.Time, say func(msg string, args ...any)) bool {
	dropped := st.Prune(r.Hosts, r.Instances, r.before)
	for _, d := range dropped {
		// Logs "binding dropped" for each ref the survey found gone, with
		// project, target and ref (state.Dropped).
		say("binding dropped", "project", d.Project, "target", d.Target, "ref", d.Ref)
	}
	changed := c.claimLate(st, r, projects, now, say) || len(dropped) > 0
	l := st.Launch
	if !surveyed || l == nil {
		return changed
	}
	p, ok := projectNamed(projects, l.Project)
	if !ok {
		return changed
	}
	if ref, ok := c.claim(prev, r.Windows, p, st, now, projects); ok {
		// Logs "claim" with project, target and ref. An empty target is an
		// action's launch, whose window is attached; else the window is bound.
		// The first settle of a process claims nothing (Settle), so only the TUI
		// writes this line.
		say("claim", "project", p.Name, "target", l.Target, "ref", ref)
		// An attachment records the terminal inside the window as well; a
		// binding is of the window itself.
		refs := []revier.TargetRef{ref}
		if l.Target == "" {
			refs = c.attachment(r.Instances, ref)
		}
		st.Claim(l.Target, refs...)
		return true
	}
	if !l.Pending(now) {
		if l.Target != "" && l.Kept(now) {
			// A target's launch past BindWindow with no window, and still
			// kept: logs "claim: launch set aside" as LogReplacedLaunch
			// describes, with no by_project or by_target.
			say("claim: launch set aside", "project", p.Name, "target", l.Target, "launched_at", l.At, "age_ms", now.Sub(l.At).Milliseconds())
			st.SetAside(*l)
		} else {
			// An action's launch past ClaimWindow, or a target's launch past
			// LateWindow, ends here with no window claimed. The line carries
			// project, target (empty for an action) and launched_at.
			say("claim: launch expired with no window claimed", "project", p.Name, "target", l.Target, "launched_at", l.At)
		}
		st.Launch = nil
		return true
	}
	return changed
}

// claimLate ends each late launch past LateWindow, then binds each one that
// awaits exactly one window of the window host's listing. No two late
// launches await one window: a window either awaits is matched by no other
// target's rule, and a launch ends the late one of its own target. It needs
// no previous listing: a late launch's window may have been listed for
// minutes, under a title its rule came to match only now. `revier list`
// logs only WARN and ERROR, so its expiries and binds write no line. A broader
// rule of another project can take the window first: holding it back would
// make claim and bind read every late launch's rendered rule.
func (c *Core) claimLate(st *state.State, r Report, projects []Project, now time.Time, say func(msg string, args ...any)) bool {
	n := len(st.Late)
	st.Late = slices.DeleteFunc(st.Late, func(l state.Launch) bool {
		if l.Kept(now) {
			return false
		}
		// Logs "claim: late launch expired with no window bound" with
		// project, target and launched_at: the late launch reached LateWindow
		// with no window bound. Its window never came, or no settle found the
		// one window it awaits (awaits).
		say("claim: late launch expired with no window bound", "project", l.Project, "target", l.Target, "launched_at", l.At)
		return true
	})
	changed := len(st.Late) < n
	if len(st.Late) == 0 || c.Window == nil || !slices.Contains(r.Hosts, c.Window.Name()) {
		return changed
	}
	// Backwards, so that a claim, which removes its launch, moves none of
	// the launches still to be asked.
	for i := len(st.Late) - 1; i >= 0; i-- {
		l := st.Late[i]
		// A launch stamped after now was written after the listing was
		// taken, so no window in the listing is its.
		if l.At.After(now) {
			continue
		}
		var awaited []revier.TargetRef
		for _, w := range r.Windows {
			if c.awaits(l, w, st, projects) {
				awaited = append(awaited, w.Ref)
			}
		}
		if len(awaited) != 1 {
			continue
		}
		// The line carries project, target, ref and age_ms, the time since
		// the launch: a late launch's window can come minutes after it
		// (decisions/0190). Any process that settles can bind one.
		say("claim: late launch bound", "project", l.Project, "target", l.Target, "ref", awaited[0], "age_ms", now.Sub(l.At).Milliseconds())
		st.ClaimLate(i, awaited[0])
		changed = true
	}
	return changed
}

// awaits reports whether w is the window the late launch l awaits: its
// target's full window rule matches it, no binding or attachment holds it,
// and no other target's window rule matches it. Its own rule is asked first,
// so the rules of every project are asked only of the few windows it takes.
func (c *Core) awaits(l state.Launch, w revier.Instance, st *state.State, projects []Project) bool {
	p, ok := projectNamed(projects, l.Project)
	if !ok {
		return false
	}
	i, ok := p.index(l.Target)
	if !ok || p.Targets[i].Window == nil || !p.compiled[i].window.Matches(w) || st.Held(w.Ref) {
		return false
	}
	for _, q := range projects {
		for j, t := range q.Targets {
			if t.Window != nil && (q.Name != p.Name || j != i) && q.compiled[j].window.Matches(w) {
				return false
			}
		}
	}
	return true
}
