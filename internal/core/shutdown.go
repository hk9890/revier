package core

import (
	"context"
	"errors"
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
	// ClosePanel closes one panel of the instance through the runtime.
	ClosePanel
	// CloseUnsupported is a step whose host cannot close it. It is a normal
	// outcome: the shutdown names it and leaves it open.
	CloseUnsupported
)

// CloseStep is one thing a shutdown closes. Target is empty for an attached
// window and for an agent. Panel is set when only that panel closes. Agents
// are the agents on this machine that end with it.
type CloseStep struct {
	Project revier.ProjectName
	Target  revier.TargetName
	Ref     revier.TargetRef
	Panel   revier.PanelID
	Action  CloseAction
	Agents  []revier.AgentView
}

// Name is how a step is named to the user: its target, the harness of the
// agent it ends, the panel when no probe named one, or an attached window's
// title.
func (s CloseStep) Name() string {
	switch {
	case s.Target != "":
		return string(s.Target)
	case len(s.Agents) > 0:
		return s.Agents[0].State.Harness
	case s.Panel != "":
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
				if k := key(a.Ref) + "\x00" + string(a.Panel); !seen[k] && !seen[key(a.Ref)] {
					seen[k] = true
					plan = append(plan, c.closeStep(CloseStep{Project: v.Project.Name, Ref: a.Ref, Panel: a.Panel, Agents: []revier.AgentView{a}}))
				}
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
			held := agentsIn(local, tv.Ref, "")
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
			panel, open := tabOf(instances[key(tv.Ref)], tv.Name)
			if !open {
				continue
			}
			held := agentsIn(local, tv.Ref, panel)
			if scope == ShutdownTargets && len(held) > 0 {
				continue
			}
			plan = append(plan, c.closeStep(CloseStep{Project: v.Project.Name, Target: tv.Name, Ref: tv.Ref, Panel: panel, Agents: held}))
		}
	}
	return plan
}

// agentsIn are the agents in an instance, or in one panel of it.
func agentsIn(agents []revier.AgentView, ref revier.TargetRef, panel revier.PanelID) []revier.AgentView {
	var out []revier.AgentView
	for _, a := range agents {
		if key(a.Ref) == key(ref) && (panel == "" || a.Panel == panel) {
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
			if (s.Panel == "" || p.ID == s.Panel) && self(p) {
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

// Shutdown closes each step of the plan in order, then lists the hosts until
// every step it closed is gone or wait has passed. One step's failure is not
// the shutdown's: the others still close, and the one that did not is named.
func (c *Core) Shutdown(ctx context.Context, plan []CloseStep, wait time.Duration) Closed {
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
			closer, _ := c.panelCloser(s.Ref)
			res.Err = closer.ClosePanel(ctx, s.Ref, s.Panel)
		}
		slog.Info("shutdown step", "project", s.Project, "target", s.Target, "ref", s.Ref, "panel", s.Panel,
			"agents", len(s.Agents), "busy", s.Busy(), "unsupported", res.Action == CloseUnsupported, "err", res.Err)
		out = append(out, res)
	}
	c.awaitClosed(ctx, out, wait)
	closed, open, failed := out.Counts()
	slog.Info("shutdown", "closed", closed, "open", open, "failed", failed)
	return out
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
			still := listed(snap, r.Ref, r.Panel)
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

// listed reports an instance, or one panel of it, in a listing.
func listed(snap snapshot, ref revier.TargetRef, panel revier.PanelID) bool {
	inst, ok := byRef(snap, ref)
	if !ok || panel == "" {
		return ok
	}
	return holdsPanel(inst, panel)
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
