package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// ShutdownScope is what a shutdown closes (decisions.md D78).
type ShutdownScope uint8

const (
	// ShutdownAll closes every open target and attached window, and with
	// them the agents they hold.
	ShutdownAll ShutdownScope = iota
	// ShutdownAgents closes the agent panels and leaves their workspaces.
	ShutdownAgents
	// ShutdownTargets closes the targets and attached windows that hold no
	// agent, so no conversation ends.
	ShutdownTargets
)

// CloseAction is how a shutdown closes one step.
type CloseAction uint8

const (
	// CloseInstance closes the whole instance through its host.
	CloseInstance CloseAction = iota
	// ClosePanel closes the step's panels of the instance through the
	// runtime, one by one.
	ClosePanel
	// CloseUnsupported is a step whose host cannot close it. It is a normal
	// outcome: the shutdown names it and leaves it open.
	CloseUnsupported
)

// CloseStep is one thing a shutdown closes. Target is empty for an attached
// window and for an agent. Panel is set when panels close rather than the
// instance, and names the step; Panels is every panel that closes with it,
// which for a whole tab is the tab (decisions.md D94). Agents are the agents
// on this machine that end with it.
type CloseStep struct {
	Project revier.ProjectName
	Target  revier.TargetName
	Ref     revier.TargetRef
	Panel   revier.PanelID
	Panels  []revier.PanelID
	Action  CloseAction
	Agents  []revier.AgentView
}

// closes are the panels the step closes: its tab, or the one panel that
// names it.
func (s CloseStep) closes() []revier.PanelID {
	if len(s.Panels) > 0 {
		return s.Panels
	}
	return []revier.PanelID{s.Panel}
}

// panels are the panels whose agents the step ends, and none for a step that
// closes the whole instance, where every agent in it ends.
func (s CloseStep) panels() []revier.PanelID {
	if s.Panel == "" {
		return nil
	}
	return s.closes()
}

// Name is how a step is named to the user: its target, the harness of the
// agent in the panel it ends, that panel when no probe named one, or an
// attached window's title. A step that closes a whole instance is named by
// the instance, not by an agent inside it: two attached terminals each
// holding a claude would otherwise be one name twice.
func (s CloseStep) Name() string {
	switch {
	case s.Target != "":
		return string(s.Target)
	case s.Panel != "":
		if len(s.Agents) > 0 {
			return s.Agents[0].State.Harness
		}
		return "panel " + s.Panel.String()
	}
	return s.Ref.Title
}

// Busy reports an agent of the step that is working or waiting for an answer:
// closing it loses the turn it is in.
func (s CloseStep) Busy() bool { return len(s.BusyAgents()) > 0 }

// BusyAgents are the step's agents that work or wait for an answer.
func (s CloseStep) BusyAgents() []revier.AgentView {
	var out []revier.AgentView
	for _, a := range s.Agents {
		if a.State.Status == revier.StatusRunning || a.State.Status == revier.StatusAttention {
			out = append(out, a)
		}
	}
	return out
}

// Busy returns the steps of a plan that would end a busy agent.
func Busy(plan []CloseStep) []CloseStep {
	var out []CloseStep
	for _, s := range plan {
		if s.Busy() {
			out = append(out, s)
		}
	}
	return out
}

// ShutdownPlan decides what a shutdown of one project, or of every project
// when only is empty, closes. It is the whole of shutdown's policy; Shutdown
// walks it.
//
// Each instance is closed once: one instance can back two targets, and a tab
// target's ref is the instance that holds it. A remote project's agent is
// closed by closing the panel here that shows it, which ends the ssh and with
// it the agent; one no panel here shows is its host's alone.
func (c *Core) ShutdownPlan(r Report, only revier.ProjectName, scope ShutdownScope) []CloseStep {
	instances := make(map[string]revier.Instance, len(r.Instances))
	for _, inst := range r.Instances {
		instances[key(inst.Ref)] = inst
	}
	// An instance another project also holds - a shared target, one
	// workspace two projects match - stays open for that project when only
	// one project shuts down.
	seen := map[string]bool{}
	for _, v := range r.Views {
		if only == "" || v.Project.Name == only {
			continue
		}
		for _, tv := range v.Targets {
			if !tv.Ref.IsZero() {
				seen[key(tv.Ref)] = true
			}
		}
	}
	var plan []CloseStep
	for _, v := range r.Views {
		if only != "" && v.Project.Name != only {
			continue
		}
		local := c.agentsHere(v)
		if scope == ShutdownAgents {
			for _, a := range local {
				if seen[key(a.Ref)+"\x00"+string(a.Panel)] || seen[key(a.Ref)] {
					continue
				}
				panels := agentPanels(instances[key(a.Ref)], a.Panel)
				for _, p := range panels {
					seen[key(a.Ref)+"\x00"+string(p)] = true
				}
				plan = append(plan, c.closeStep(CloseStep{Project: v.Project.Name, Ref: a.Ref, Panel: a.Panel, Panels: panels, Agents: agentsIn(local, a.Ref, panels)}))
			}
			continue
		}
		// The instances first and the tabs after, so a tab in an instance
		// that closes is not closed on its own too.
		var tabs []revier.TargetView
		for _, tv := range v.Targets {
			if tv.Ref.IsZero() {
				continue
			}
			if t, ok := v.Project.Target(tv.Name); ok && !tv.Attached && tabTarget(t) {
				tabs = append(tabs, tv)
				continue
			}
			held := agentsIn(local, tv.Ref, nil)
			if scope == ShutdownTargets && len(held) > 0 {
				continue
			}
			if k := key(tv.Ref); !seen[k] {
				seen[k] = true
				plan = append(plan, c.closeStep(CloseStep{Project: v.Project.Name, Target: tv.Name, Ref: tv.Ref, Agents: held}))
			}
		}
		for _, tv := range tabs {
			if seen[key(tv.Ref)] {
				continue
			}
			inst := instances[key(tv.Ref)]
			panel, open := tabOf(inst, tv.Name)
			if !open {
				continue
			}
			panels := tabAt(inst, panel)
			held := agentsIn(local, tv.Ref, panels)
			if scope == ShutdownTargets && len(held) > 0 {
				continue
			}
			plan = append(plan, c.closeStep(CloseStep{Project: v.Project.Name, Target: tv.Name, Ref: tv.Ref, Panel: panel, Panels: panels, Agents: held}))
		}
	}
	return plan
}

// CloseRow is one row of a project that del closes: a target, a window
// attached to the project, or the agent in a panel of an instance. One of
// Target, Attached and Panel is set.
type CloseRow struct {
	Target   revier.TargetName
	Attached revier.TargetRef
	Agent    revier.TargetRef // the instance that holds the agent's panel
	Panel    revier.PanelID
}

// ClosePlan decides what closing one row of a project closes. A tab target
// closes its tab and leaves the workspace; an agent closes its panel, which
// for a link's agent is the panel here that shows it. A row with nothing open
// here, a link's agent no panel here shows among them, closes nothing.
func (c *Core) ClosePlan(r Report, project revier.ProjectName, row CloseRow) []CloseStep {
	i := slices.IndexFunc(r.Views, func(v revier.ProjectView) bool { return v.Project.Name == project })
	if i < 0 {
		return nil
	}
	v := r.Views[i]
	local := c.agentsHere(v)
	step := CloseStep{Project: project}
	var panels []revier.PanelID
	switch {
	case row.Panel != "":
		j := slices.IndexFunc(local, func(a revier.AgentView) bool { return key(a.Ref) == key(row.Agent) && a.Panel == row.Panel })
		if j < 0 {
			return nil
		}
		step.Ref, step.Panel = local[j].Ref, row.Panel
		panels = []revier.PanelID{row.Panel}
		if k := slices.IndexFunc(r.Instances, func(inst revier.Instance) bool { return key(inst.Ref) == key(step.Ref) }); k >= 0 {
			panels = agentPanels(r.Instances[k], row.Panel)
		}
		step.Panels = panels
	case !row.Attached.IsZero():
		j := slices.IndexFunc(v.Targets, func(tv revier.TargetView) bool { return tv.Attached && key(tv.Ref) == key(row.Attached) })
		if j < 0 {
			return nil
		}
		step.Ref = v.Targets[j].Ref
	default:
		j := slices.IndexFunc(v.Targets, func(tv revier.TargetView) bool {
			return !tv.Attached && tv.Name == row.Target && !tv.Ref.IsZero()
		})
		if j < 0 {
			return nil
		}
		step.Target, step.Ref = row.Target, v.Targets[j].Ref
		if t, ok := v.Project.Target(row.Target); ok && tabTarget(t) {
			k := slices.IndexFunc(r.Instances, func(inst revier.Instance) bool { return key(inst.Ref) == key(step.Ref) })
			if k < 0 {
				return nil
			}
			panel, open := tabOf(r.Instances[k], row.Target)
			if !open {
				return nil
			}
			step.Panel = panel
			panels = tabAt(r.Instances[k], panel)
			step.Panels = panels
		}
	}
	step.Agents = agentsIn(local, step.Ref, panels)
	return []CloseStep{c.closeStep(step)}
}

// agentPanels is what closing an agent's panel closes: the whole tab that
// holds it, so the shell opened beside the agent by `revier agent new` goes
// with it, since that tab carries no target mark of its own to be closed by.
// The workspace's own tab is the exception: a declared [agent, shell] layout
// keeps its shell, which is what a shutdown of the agents alone promises
// (decisions.md D94).
func agentPanels(in revier.Instance, panel revier.PanelID) []revier.PanelID {
	if ownTab(in, panel) {
		return []revier.PanelID{panel}
	}
	return tabAt(in, panel)
}

// agentsIn are the agents in an instance, or in the given panels of it when
// there are any.
func agentsIn(agents []revier.AgentView, ref revier.TargetRef, panels []revier.PanelID) []revier.AgentView {
	var out []revier.AgentView
	for _, a := range agents {
		if key(a.Ref) == key(ref) && (len(panels) == 0 || slices.Contains(panels, a.Panel)) {
			out = append(out, a)
		}
	}
	return out
}

// closeStep sets how the step closes: through the host that listed the
// instance, when it can.
func (c *Core) closeStep(s CloseStep) CloseStep {
	s.Action = CloseUnsupported
	if s.Panel != "" {
		if _, ok := c.panelCloser(s.Ref); ok {
			s.Action = ClosePanel
		}
		return s
	}
	if _, ok := c.closer(s.Ref); ok {
		s.Action = CloseInstance
	}
	return s
}

func (c *Core) closer(ref revier.TargetRef) (revier.Closer, bool) {
	h, ok := c.hostNamed(ref.Host)
	if !ok {
		return nil, false
	}
	closer, ok := h.(revier.Closer)
	return closer, ok
}

func (c *Core) panelCloser(ref revier.TargetRef) (revier.PanelCloser, bool) {
	if c.Runtime == nil || c.Runtime.Name() != ref.Host {
		return nil, false
	}
	closer, ok := c.Runtime.(revier.PanelCloser)
	return closer, ok
}

// CloseLast moves the steps that would end the calling process to the end of
// the plan, so a shutdown run from a terminal of a workspace closes everything
// else before its own terminal. self reports a panel the process runs under.
// A window host lists a window with no panels, so its process stands for it.
func CloseLast(plan []CloseStep, instances []revier.Instance, self func(revier.Panel) bool) []CloseStep {
	byRef := make(map[string]revier.Instance, len(instances))
	for _, inst := range instances {
		byRef[key(inst.Ref)] = inst
	}
	ends := func(s CloseStep) bool {
		inst := byRef[key(s.Ref)]
		if s.Panel == "" && len(inst.Panels) == 0 {
			return self(revier.Panel{PID: inst.PID})
		}
		for _, p := range inst.Panels {
			if (s.Panel == "" || slices.Contains(s.closes(), p.ID)) && self(p) {
				return true
			}
		}
		return false
	}
	out := slices.Clone(plan)
	slices.SortStableFunc(out, func(a, b CloseStep) int {
		switch ea, eb := ends(a), ends(b); {
		case ea == eb:
			return 0
		case eb:
			return -1
		}
		return 1
	})
	return out
}

// RunsUnder reports a panel the calling process runs under: the panel's
// process is this process or one of its ancestors. It is CloseLast's self.
func RunsUnder() func(revier.Panel) bool {
	mine := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !mine[pid]; {
		mine[pid] = true
		ppid, ok := parentPID(pid)
		if !ok {
			break
		}
		pid = ppid
	}
	return func(p revier.Panel) bool { return p.PID > 0 && mine[p.PID] }
}

// parentPID reads a process's parent from /proc. The command is the second
// field of stat, in parentheses, and may itself hold spaces and parentheses,
// so the fields are counted from its last closing one.
func parentPID(pid int) (int, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	s := string(raw)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

// CloseResult is one step of a shutdown and what came of it. Open is a step
// still listed once the shutdown stopped waiting: an application that asks
// about unsaved work, or a host that did not close it.
type CloseResult struct {
	CloseStep
	Err  error
	Open bool
}

// Note is what a shutdown says about the step.
func (r CloseResult) Note() string {
	switch {
	case r.Action == CloseUnsupported:
		return "left open: its host cannot close it"
	case r.Err != nil:
		return r.Err.Error()
	case r.Open:
		return "still open"
	}
	return "closed"
}

// Closed is every step of one shutdown, in the order it walked them.
type Closed []CloseResult

// Counts is how many steps closed, how many are still open, and how many
// failed. A step whose host cannot close it counts as open.
func (cs Closed) Counts() (closed, open, failed int) {
	for _, r := range cs {
		switch {
		case r.Err != nil:
			failed++
		case r.Open || r.Action == CloseUnsupported:
			open++
		default:
			closed++
		}
	}
	return closed, open, failed
}

// ClosePoll is how often a shutdown lists the hosts while it waits for what
// it closed to go.
const ClosePoll = 100 * time.Millisecond

// CloseWait is how long a shutdown waits for what it closed to go.
const CloseWait = 3 * time.Second

// ShutdownOpts is what a close needs beyond its plan: whether a busy agent
// is closed anyway, and the survey that reads the plan's agents again.
type ShutdownOpts struct {
	// Force closes a step whose agent works or waits for an answer. It is
	// what `--force` sets, and what a confirm means in a surface that named
	// the busy agents before it asked (decisions.md D78).
	Force bool

	// Projects, Bound and Attached are what the recheck surveys, the three
	// Survey takes. Without Force and without projects there is nothing to
	// read the agents from, and the shutdown refuses rather than closing
	// blind.
	Projects []Project
	Bound    map[revier.ProjectName]Bindings
	Attached map[revier.ProjectName][]revier.TargetRef

	// Report is the survey the plan was made from. It stands in for the
	// recheck's own where Force skips the recheck, and nowhere else: what a
	// close works from is the freshest survey it has.
	Report Report

	// Self reports a panel the calling process runs under. With it, the
	// steps that would end that process go last (CloseLast), ordered off the
	// same survey the agents were read from, so a shutdown run from a
	// terminal of a workspace closes everything else before its own
	// terminal. Nil keeps the plan's order.
	Self func(revier.Panel) bool

	// Before runs once the recheck has let the plan through and before the
	// first close, handed the survey the agents and the order were read
	// from. It is where the session a shutdown ends is saved, so a close the
	// busy guard refuses saves nothing either, and the session records what
	// is open now rather than what was open when the plan was drawn. Its
	// failure is the shutdown's, and nothing closes.
	Before func(Report) error
}

// ErrAgentBusy is a close refused because a step would end an agent that
// works or waits for an answer. Nothing closed.
var ErrAgentBusy = errors.New("an agent is busy")

// BusyRefusal is ErrAgentBusy with the plan as the recheck read it and the
// survey it was read from, so a surface shows the plan again with the agents
// that refused it, and a close forced after it works from that survey rather
// than from the listing the plan was drawn from.
type BusyRefusal struct {
	Plan   []CloseStep
	Report Report
}

func (r *BusyRefusal) Error() string { return ErrAgentBusy.Error() }

func (r *BusyRefusal) Unwrap() error { return ErrAgentBusy }

// Shutdown closes each step of the plan in order, then lists the hosts until
// every step it closed is gone or wait has passed. One step's failure is not
// the shutdown's: the others still close, and the one that did not is named.
//
// It closes nothing at all while an agent of the plan is busy, unless forced
// (decisions.md D97). Every plan was made from a survey that has aged since -
// by a confirm the user read, by a session saved before the close - so the
// agents are read again here, the one place every close goes through. A
// recheck that cannot be read refuses too: no agents read is not idle.
//
// That recheck's survey is the freshest one there is, so the order opts.Self
// asks for and the session opts.Before saves are both taken from it, after
// the guard: a refused close saves nothing, and a saved session holds what is
// open now rather than what was open when the plan was drawn.
func (c *Core) Shutdown(ctx context.Context, plan []CloseStep, wait time.Duration, opts ShutdownOpts) (Closed, error) {
	r := opts.Report
	if !opts.Force {
		fresh, freshPlan, err := c.recheck(ctx, plan, opts)
		if err != nil {
			return nil, err
		}
		if busy := Busy(freshPlan); len(busy) > 0 {
			slog.Info("shutdown refused", "steps", len(freshPlan), "busy", len(busy))
			return nil, &BusyRefusal{Plan: freshPlan, Report: fresh}
		}
		r, plan = fresh, freshPlan
	}
	if opts.Self != nil {
		plan = CloseLast(plan, r.Instances, opts.Self)
	}
	if opts.Before != nil {
		if err := opts.Before(r); err != nil {
			return nil, err
		}
	}
	out := make(Closed, 0, len(plan))
	for _, s := range plan {
		// The plan may come from a core whose hosts have changed since, as the
		// TUI's does after a runtime switch: a host that is gone cannot close.
		res := CloseResult{CloseStep: c.closeStep(s)}
		switch res.Action {
		case CloseInstance:
			closer, _ := c.closer(s.Ref)
			res.Err = closer.Close(ctx, s.Ref)
		case ClosePanel:
			// A tab closes as its panels: the runtime ends the tab with the
			// last of them (decisions.md D94).
			closer, _ := c.panelCloser(s.Ref)
			for _, p := range res.closes() {
				res.Err = errors.Join(res.Err, closer.ClosePanel(ctx, s.Ref, p))
			}
		}
		slog.Info("shutdown step", "project", s.Project, "target", s.Target, "ref", s.Ref, "panel", s.Panel, "panels", len(res.closes()),
			"agents", len(s.Agents), "busy", s.Busy(), "unsupported", res.Action == CloseUnsupported, "err", res.Err)
		out = append(out, res)
	}
	c.awaitClosed(ctx, out, wait)
	closed, open, failed := out.Counts()
	slog.Info("shutdown", "closed", closed, "open", open, "failed", failed)
	return out, nil
}

// recheck surveys again and returns that survey with the plan whose steps
// carry the agents it found. Everything the close does from here - the order,
// the session it saves - is read off the returned report, not off the one the
// plan was drawn from.
func (c *Core) recheck(ctx context.Context, plan []CloseStep, opts ShutdownOpts) (Report, []CloseStep, error) {
	r, err := c.Survey(ctx, opts.Projects, opts.Bound, opts.Attached)
	if err != nil {
		return Report{}, nil, fmt.Errorf("the plan's agents could not be read again: %w", err)
	}
	fresh, err := c.rechecked(r, plan)
	if err != nil {
		return Report{}, nil, fmt.Errorf("the plan's agents could not be read again: %w", err)
	}
	return r, fresh, nil
}

// rechecked is the plan with each step's agents as a later survey finds them.
// It adds and drops no step: a plan confirmed is the plan that runs, and only
// what its agents do now can stop it (decisions.md D78).
//
// It errors on a step the survey could not answer for - the host that lists
// its instance did not answer, or its project is no longer surveyed. A
// degraded survey reports no agents, and no agents read is not idle: taking
// it as idle would let the close through in the one case it knows least.
func (c *Core) rechecked(r Report, plan []CloseStep) ([]CloseStep, error) {
	here := make(map[revier.ProjectName][]revier.AgentView, len(r.Views))
	unreachable := map[revier.ProjectName]string{}
	for _, v := range r.Views {
		here[v.Project.Name] = c.agentsHere(v)
		if v.Unreachable != "" {
			unreachable[v.Project.Name] = v.Unreachable
		}
	}
	out := make([]CloseStep, len(plan))
	for i, step := range plan {
		if err := r.Failed[step.Ref.Host]; err != nil {
			return nil, fmt.Errorf("%s did not answer: %w", step.Ref.Host, err)
		}
		local, surveyed := here[step.Project]
		if !surveyed {
			return nil, fmt.Errorf("%s is no longer surveyed", step.Project)
		}
		// A link's agents are its host's word (D84). A host that stopped
		// answering between the plan and the close reports none, and no
		// agents read is not idle, so the panel here that shows a busy agent
		// is not closed on a survey that could not ask about it.
		if why, ok := unreachable[step.Project]; ok {
			return nil, fmt.Errorf("%s did not answer: %s", step.Project, why)
		}
		step.Agents = agentsIn(local, step.Ref, step.panels())
		out[i] = step
	}
	return out, nil
}

// awaitClosed marks every closed step that is still listed once wait has
// passed. A step whose host cannot list is taken as gone, since the close
// itself answered without an error; a step whose host answered is judged by
// that listing, whatever another host did (decisions.md D89). A close that
// failed on something the listing no longer holds is closed all the same:
// whether it is gone is the listing's word, not Close's (revier.Closer).
func (c *Core) awaitClosed(ctx context.Context, out Closed, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for {
		snap, failed := c.listing(ctx)
		pending := false
		for i := range out {
			r := &out[i]
			if r.Action == CloseUnsupported {
				continue
			}
			if failed[r.Ref.Host] != nil {
				r.Open = false
				continue
			}
			still := listed(snap, r.CloseStep)
			if r.Err != nil && !still {
				r.Err = nil
			}
			r.Open = r.Err == nil && still
			pending = pending || r.Open
		}
		if !pending || time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ClosePoll):
		}
	}
}

// listed reports what a step closes in a listing: its instance, or any panel
// it closes - a tab is still open while one panel of it is listed.
func listed(snap snapshot, s CloseStep) bool {
	inst, ok := byRef(snap, s.Ref)
	if !ok || s.Panel == "" {
		return ok
	}
	return slices.ContainsFunc(inst.Panels, func(p revier.Panel) bool {
		return slices.Contains(s.closes(), p.ID)
	})
}

// SaveChanged saves the session a shutdown ends, unless the newest saved
// session already holds the same, or nothing is open. saved is false when it
// wrote nothing.
func (c *Core) SaveChanged(ctx context.Context, stateRoot string, r Report, current revier.ProjectName, at time.Time) (stored session.Session, saved bool, gaps SessionGaps, err error) {
	s, gaps := c.Session(ctx, r, current)
	if len(s.Projects) == 0 {
		return session.Session{}, false, gaps, nil
	}
	newest, err := session.Load(stateRoot, "")
	switch {
	case err == nil && newest.SameAs(s):
		return newest, false, gaps, nil
	case err != nil && !errors.Is(err, session.ErrNoSession):
		return session.Session{}, false, gaps, err
	}
	s.At = at
	stored, _, err = store(stateRoot, s, gaps, "shutdown")
	if err != nil {
		return session.Session{}, false, gaps, err
	}
	return stored, true, gaps, nil
}
