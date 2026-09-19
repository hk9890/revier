package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// Ledger is where an activation reads and writes what it learns: where a
// project's targets last landed, the launch still coming up, and the ref or
// launch each activation leaves. StateLedger is the one every surface uses
// (decisions.md D91); a test may hand in another.
type Ledger interface {
	Bound(p revier.ProjectName) Bindings
	Pending(p revier.ProjectName, t revier.TargetName) bool
	Launched(p revier.ProjectName, t revier.TargetName, at time.Time)
	Landed(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef)
}

// RestoreResult is one step of a restore and what came of it. Ref is zero for
// a step that launched nothing, and for a launch whose window is not up yet.
type RestoreResult struct {
	RestoreStep
	Ref    revier.TargetRef
	Agents []AgentOutcome
	// AgentErr is why an agent tab failed to open in a workspace that did.
	AgentErr error
	Err      error
	// dry is a step planned and not walked: its agents are what a launch
	// would do with them.
	dry bool
}

// Planned reports a step from RestorePreview: planned, and not walked.
func (r RestoreResult) Planned() bool { return r.dry }

// Note is what a restore says about the step: why it was stepped over, or
// what its launch did, or would do, with the agents it recorded.
func (r RestoreResult) Note() string {
	switch {
	case r.Action != RestoreLaunch:
		return r.Action.String()
	case r.dry:
		return AgentsNote("would open", r.Agents, nil)
	case r.Err != nil:
		return r.Err.Error()
	case r.Ref.IsZero():
		// A window that has not shown yet is not a target that came back.
		return AgentsNote("launched, not up yet", r.Agents, r.AgentErr)
	}
	return AgentsNote("opened", r.Agents, r.AgentErr)
}

// Restored is every step of one restore, in the order it walked them.
type Restored []RestoreResult

// Counts is how many launches opened, how many are not up yet, and how many
// failed. A step stepped over is none of them.
func (rs Restored) Counts() (opened, pending, failed int) {
	for _, r := range rs {
		switch {
		case r.Action != RestoreLaunch:
		case r.Err != nil:
			failed++
		case r.Ref.IsZero():
			pending++
		default:
			opened++
		}
	}
	return opened, pending, failed
}

// RestorePreview is the plan with what each launch would do with its agents,
// for a dry run. It opens nothing and writes nothing.
func (c *Core) RestorePreview(s session.Session, r Report, projects []Project) Restored {
	var out Restored
	for _, step := range c.RestorePlan(s, r) {
		res := RestoreResult{RestoreStep: step, dry: true}
		if step.Action == RestoreLaunch {
			if p, ok := projectNamed(projects, step.Project); ok {
				res.Agents = c.Resumes(p, step.Target, step.Resumes)
			} else {
				res.Action = RestoreNoProject
			}
		}
		out = append(out, res)
	}
	return out
}

// Restore opens what a saved session recorded, and ends on the project the
// save was left on, so a restored desktop lands where the saved one was. back
// is why it could not return there. It returns only to a home the session
// recorded open or that runs now: the project last acted on may have been
// closed before the save, and a restore launches nothing its plan did not
// show.
//
// It walks the plan in file order and never in parallel: a launch is bound to
// the window that appears after it, so two at once are two windows neither
// can be attributed to. One step's failure is not the restore's: the other
// workspaces still come back, and the one that did not is named.
func (c *Core) Restore(ctx context.Context, s session.Session, r Report, projects []Project, l Ledger) (out Restored, back error) {
	slog.Info("session restore", "id", s.ID, "name", s.Name, "saved_at", s.At, "dry_run", false)
	for _, step := range c.RestorePlan(s, r) {
		res := RestoreResult{RestoreStep: step}
		p, loaded := projectNamed(projects, step.Project)
		// The plan was built from a survey of these projects, so a project it
		// knew cannot be missing here. Reported rather than asserted.
		if step.Action == RestoreLaunch && !loaded {
			slog.Warn("restore step: planned project not loaded", "project", step.Project, "target", step.Target)
			res.Action = RestoreNoProject
		}
		// Logged before the launch, so a step that hangs on its window is
		// the last one the log names.
		logStep(res.RestoreStep, false)
		if res.Action == RestoreLaunch {
			res = c.restoreLaunch(ctx, p, step, l)
			logAgents(res)
		}
		out = append(out, res)
	}
	if p, ok := projectNamed(projects, s.Current); ok {
		if home, has := p.Home(); has && c.returnable(ctx, s, p, home.Name, l) {
			if res := c.restoreLaunch(ctx, p, RestoreStep{Project: p.Name, Target: home.Name}, l); res.Err != nil {
				back = fmt.Errorf("could not return to %s: %w", s.Current, res.Err)
			}
		}
	}
	opened, pending, failed := out.Counts()
	slog.Info("session restored", "id", s.ID, "opened", opened, "pending", pending, "failed", failed)
	return out, back
}

// restoreLaunch is one step of a restore, walked by ActivateWaiting. It has
// the deadline a keypress and the bind after it have, so a host that never
// answers costs the restore one step and not the rest of the walk.
func (c *Core) restoreLaunch(ctx context.Context, p Project, step RestoreStep, l Ledger) RestoreResult {
	ctx, cancel := context.WithTimeout(ctx, 2*BindWait)
	defer cancel()
	ref, res, err := c.ActivateWaiting(ctx, p, step.Target, step.Resumes, l)
	return RestoreResult{RestoreStep: step, Ref: ref, Agents: res.Agents, AgentErr: res.AgentErr, Err: err}
}

// ActivateWaiting is the whole run-or-raise for one target: Activate with the
// project's bindings, then, for a detached launch, the wait that binds its
// window. The launch is recorded before the wait, so a press elsewhere during
// it does not launch again, and every ref it lands on is recorded after, so
// the next press finds the target by id whatever the application has done to
// its title since. A zero ref with no error is a target launched and not yet
// up. The Result is returned also when the wait failed: the launch ran, and
// its agents came to Result.Agents.
func (c *Core) ActivateWaiting(ctx context.Context, p Project, name revier.TargetName, resumes []Resume, l Ledger) (revier.TargetRef, Result, error) {
	res, err := c.Activate(ctx, p, name, l.Bound(p.Name), l.Pending(p.Name, name), resumes)
	ref := res.Ref
	if res.Launched && ref.IsZero() && err == nil {
		l.Launched(p.Name, res.Target, time.Now())
		var inst revier.Instance
		var ok bool
		inst, ok, err = c.Bind(ctx, p, res.Target, res.Before, BindWait)
		if ok {
			ref = inst.Ref
		}
	}
	if !ref.IsZero() {
		l.Landed(p.Name, res.Target, ref)
	}
	return ref, res, err
}

// LogRestore writes one line for a step, and, for a launch, one per recorded
// agent of it: the conversation and directory it was recorded with, and what
// the launch did with it. A launch that failed before it launched has no
// outcomes, and each of its agents is logged with outcome none.
func LogRestore(r RestoreResult) {
	logStep(r.RestoreStep, r.dry)
	logAgents(r)
}

func logStep(step RestoreStep, dry bool) {
	slog.Info("restore step", "project", step.Project, "target", step.Target, "action", step.Action.String(), "dry_run", dry)
}

// logAgents is LogRestore's agent lines. A step stepped over launches nothing,
// so its agents have no outcome worth a line.
func logAgents(r RestoreResult) {
	if r.Action != RestoreLaunch {
		return
	}
	for i, a := range r.Resumes {
		outcome := "none"
		if i < len(r.Agents) {
			outcome = r.Agents[i].String()
		}
		slog.Info("restore agent", "project", r.Project, "target", r.Target, "dry_run", r.dry,
			"harness", a.Harness, "session", a.Session, "dir", a.Dir, "outcome", outcome)
	}
}

// AgentsNote says what opening a target did, or would do, to the agents it
// recorded: how many start on their conversation, how many start empty because
// the directory they worked in is gone or their conversation cannot be resumed
// here, and how many cannot be started at all, with tabErr for the ones whose
// tab failed to open. An agent recorded with no conversation starts empty as
// it always would, and is not worth a word.
func AgentsNote(verb string, agents []AgentOutcome, tabErr error) string {
	n := map[AgentOutcome]int{}
	for _, o := range agents {
		n[o]++
	}
	note := verb
	if n[AgentResumed] > 0 {
		note += fmt.Sprintf(", %s resumed", Count(n[AgentResumed], "agent"))
	}
	if n[AgentDirGone] > 0 {
		note += fmt.Sprintf(", %s empty: directory gone", Count(n[AgentDirGone], "agent"))
	}
	if n[AgentUnresumable] > 0 {
		note += fmt.Sprintf(", %s empty: no probe here resumes its harness in its panel", Count(n[AgentUnresumable], "agent"))
	}
	if n[AgentDropped] > 0 {
		note += fmt.Sprintf(", %s not restored: no agent panel declared, or no tab can be opened here", Count(n[AgentDropped], "agent"))
	}
	if n[AgentInTab] > 0 {
		note += fmt.Sprintf(", %s not resumed: it ran in a tab target", Count(n[AgentInTab], "agent"))
	}
	if n[AgentNotAdded] > 0 {
		note += fmt.Sprintf(", %s not restored: %v", Count(n[AgentNotAdded], "agent"), tabErr)
	}
	return note
}

// Notes is what a save says about what it could not record, one line each. It
// is said while the agents still run, so the gap can be closed before the
// reboot rather than found after it.
func (g SessionGaps) Notes() []string {
	var out []string
	if g.Unnamed > 0 {
		out = append(out, fmt.Sprintf("%s without a conversation id, to be restored empty", Count(g.Unnamed, "agent")))
	}
	if len(g.InTab) > 0 {
		out = append(out, fmt.Sprintf("%s in a tab target, to be restored without its conversation: %s",
			Count(len(g.InTab), "agent"), strings.Join(g.InTab, ", ")))
	}
	for _, err := range g.Failed {
		out = append(out, fmt.Sprintf("could not ask %v", err))
	}
	// An attachment is a live id with no launch argv anywhere in the model,
	// so there is nothing that could bring one back.
	if g.Attached > 0 {
		out = append(out, fmt.Sprintf("%s not recorded; they have no name to be reopened by", Count(g.Attached, "attached instance")))
	}
	return out
}

// Count writes a number and its noun, pluralised. A summary that reads
// "1 projects" is a summary nobody proofread.
func Count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// returnable reports whether a restore may end on the project's home: the
// session recorded it open, so the plan showed it, or it runs now, so the
// return raises it and launches nothing.
func (c *Core) returnable(ctx context.Context, s session.Session, p Project, home revier.TargetName, l Ledger) bool {
	if recorded(s, p.Name, home) {
		return true
	}
	running, err := c.Running(ctx, p, home, l.Bound(p.Name))
	return err == nil && running
}

// recorded reports whether the session recorded the target of the project open.
func recorded(s session.Session, p revier.ProjectName, t revier.TargetName) bool {
	for _, sp := range s.Projects {
		if sp.Name != p {
			continue
		}
		for _, st := range sp.Targets {
			if st.Name == t {
				return true
			}
		}
	}
	return false
}

func projectNamed(projects []Project, name revier.ProjectName) (Project, bool) {
	for _, p := range projects {
		if p.Name == name {
			return p, true
		}
	}
	return Project{}, false
}
