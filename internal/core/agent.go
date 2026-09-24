package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// Driving one agent from a script: find it, wait on its state, type into it.
// Which panel may be typed into, and when, is policy, so every refusal is made
// here. A runtime only delivers the text (decisions.md D31).

var (
	// ErrNoAgent means a project, or one of its targets, holds no agent panel.
	ErrNoAgent = errors.New("no agent panel")

	// ErrNotAgent refuses a panel no probe claims, or one whose foreground is
	// a shell: text typed there runs as a command. A probe can still claim a
	// shell - the kitty marker outlives the agent in a `--hold` window - which
	// is why the foreground is checked as well.
	ErrNotAgent = errors.New("not an agent panel")

	// ErrAmbiguous refuses an address that names more than one agent.
	ErrAmbiguous = errors.New("names more than one agent")

	// ErrAttention refuses a prompt to an agent that is waiting for the human.
	// It shows a question or a permission dialog, where the Enter that submits
	// a prompt picks the highlighted option instead.
	ErrAttention = errors.New("agent is waiting for an answer")

	// ErrAgentGone means the panel a wait was following is no longer an
	// agent's, or no longer there.
	ErrAgentGone = errors.New("agent panel is gone")

	// ErrNoWriter means the runtime holding the agent cannot type into a panel.
	ErrNoWriter = errors.New("runtime cannot type into a panel")
)

// AgentPoll is how often Wait and Prompt read an agent again. LinkPoll is
// the same for a link's agent, whose every read is an ssh to its host.
const (
	AgentPoll = 500 * time.Millisecond
	LinkPoll  = 2 * time.Second
)

// PromptConfirmPolls is how many polls Prompt watches an idle agent for the
// turn it asked for. A runtime reports that text was delivered, never that it
// was read, so an agent leaving idle is the only confirmation there is.
const PromptConfirmPolls = 6

// Agent is one agent panel: the instance holding it, the panel, and what its
// probe read there.
type Agent struct {
	Ref   revier.TargetRef
	Panel revier.Panel
	State revier.AgentState

	// link is set for an agent of a link: its state is its host's to say.
	link *Project
}

// Poll is how often the agent is read again: over ssh for a link's.
func (a Agent) Poll() time.Duration {
	if a.link != nil {
		return LinkPoll
	}
	return AgentPoll
}

// untils are the statuses a wait can ask for. "stopped" is an agent doing no
// work, at rest or waiting for the human: what a script that prompted it waits
// for before it reads the result.
var untils = map[string][]revier.Status{
	"idle":      {revier.StatusIdle},
	"running":   {revier.StatusRunning},
	"attention": {revier.StatusAttention},
	"stopped":   {revier.StatusIdle, revier.StatusAttention},
}

// Until returns the statuses a wait for name ends on.
func Until(name string) ([]revier.Status, error) {
	if s, ok := untils[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("unknown status %q: want idle, running, attention or stopped", name)
}

// Worst is the one agent a project is summed up by, where a surface has room
// for one: the agent in the state closest to needing the human, and of two in
// that state the first. `revier list` shows it. The second return is false
// for a project with no agent.
func Worst(agents []revier.AgentView) (revier.AgentState, bool) {
	if len(agents) == 0 {
		return revier.AgentState{}, false
	}
	worst := agents[0].State
	for _, a := range agents[1:] {
		if a.State.Status > worst.Status {
			worst = a.State
		}
	}
	return worst, true
}

// Agent finds the agent panel addr names in a project: with addr empty, the
// project's only agent; with a target name, the only agent in that target's
// instance; otherwise the panel with that id.
func (c *Core) Agent(ctx context.Context, p Project, addr string, bound Bindings) (Agent, error) {
	// Agents are panels, and only the runtime and the served processes
	// list instances with panels.
	snap, err := c.answered(ctx, nameOf(c.Runtime), nameOf(c.Served))
	if err != nil {
		return Agent{}, err
	}
	where, only := string(p.Name), revier.TargetName("")
	if _, ok := p.index(revier.TargetName(addr)); ok {
		where, only = where+":"+addr, revier.TargetName(addr)
	}
	scope := c.running(snap, p, bound, only)
	if p.Remote != nil {
		return c.linkAgent(ctx, p, addr, only, scope, snap)
	}
	switch {
	case addr != "" && only == "":
		return c.panel(ctx, scope, p.Name, addr)
	case only != "" && len(scope) == 0:
		return Agent{}, fmt.Errorf("%s is not running", where)
	}

	var found []Agent
	var targets []revier.TargetName
	ids := map[revier.PanelID]int{}
	for _, h := range scope {
		for _, panel := range h.inst.Panels {
			if probe, ok := c.agentProbe(panel); ok {
				found = append(found, Agent{Ref: h.inst.Ref, Panel: panel, State: c.read(ctx, probe, h.inst.Ref, panel)})
				targets = append(targets, h.target)
				ids[panel.ID]++
			}
		}
	}
	switch len(found) {
	case 0:
		return Agent{}, fmt.Errorf("%s: %w", where, ErrNoAgent)
	case 1:
		return found[0], nil
	}
	// A panel id is one process's - a kitty window id is - so two windows can
	// each hold a panel 1. Such an agent is named by its target instead.
	addrs := make([]string, len(found))
	for i, a := range found {
		addrs[i] = string(p.Name) + ":" + a.Panel.ID.String()
		if ids[a.Panel.ID] > 1 {
			addrs[i] = string(p.Name) + ":" + string(targets[i])
		}
	}
	return Agent{}, fmt.Errorf("%s %w: use one of %s", where, ErrAmbiguous, strings.Join(addrs, ", "))
}

// panel finds an agent by panel id among a project's running instances. An
// id held in more than one of them - each by its own process - names none.
func (c *Core) panel(ctx context.Context, scope []held, project revier.ProjectName, id string) (Agent, error) {
	var hits []held
	var hit revier.Panel
	for _, h := range scope {
		for _, panel := range h.inst.Panels {
			if panel.ID.String() == id {
				hits, hit = append(hits, h), panel
			}
		}
	}
	switch len(hits) {
	case 0:
		return Agent{}, fmt.Errorf("%s has no target and no running panel named %q", project, id)
	case 1:
	default:
		names := make([]string, len(hits))
		for i, h := range hits {
			names[i] = string(project) + ":" + string(h.target)
		}
		return Agent{}, fmt.Errorf("%s:%s %w: more than one window holds a panel %s; use one of %s",
			project, id, ErrAmbiguous, id, strings.Join(names, ", "))
	}
	probe, ok := c.agentProbe(hit)
	if !ok {
		return Agent{}, fmt.Errorf("%s:%s is %w: %s", project, id, ErrNotAgent, notAgentReason(hit))
	}
	return Agent{Ref: hits[0].inst.Ref, Panel: hit, State: c.read(ctx, probe, hits[0].inst.Ref, hit)}, nil
}

func notAgentReason(panel revier.Panel) string {
	if panel.Kind == revier.PanelShell {
		return "its foreground is a shell, where the text would run as a command"
	}
	return "no probe recognises what runs in it"
}

// held is a running instance and the target it backs.
type held struct {
	target revier.TargetName
	inst   revier.Instance
}

// running lists the instances backing a project's targets, each once, in
// target order; with only set, that target's alone.
func (c *Core) running(snap snapshot, p Project, bound Bindings, only revier.TargetName) []held {
	var out []held
	seen := map[string]bool{}
	for i, t := range p.Targets {
		if only != "" && t.Name != only {
			continue
		}
		if p.isTab(i) {
			// A tab's panel is listed with the instance that holds it, and
			// alone when the tab is asked for by name.
			if only != "" {
				out = append(out, c.heldTab(snap, p, i, bound)...)
			}
			continue
		}
		if inst, ok := c.served(snap, p, i); ok && !seen[key(inst.Ref)] {
			seen[key(inst.Ref)] = true
			out = append(out, held{target: t.Name, inst: inst})
		}
		host, _, m, err := c.resolveAt(p, i)
		if err != nil {
			continue
		}
		if inst, ok := c.locate(snap, p, i, host, m, bound[t.Name]); ok && !seen[key(inst.Ref)] {
			seen[key(inst.Ref)] = true
			out = append(out, held{target: t.Name, inst: inst})
		}
	}
	return out
}

// heldTab is the open tab of the i-th target: the instance that holds it,
// narrowed to the tab's own panel.
func (c *Core) heldTab(snap snapshot, p Project, i int, bound Bindings) []held {
	in, _, tab, open, err := c.container(snap, p, i, bound)
	if err != nil || !open {
		return nil
	}
	for _, panel := range in.Panels {
		if panel.ID == tab {
			in.Panels = []revier.Panel{panel}
			return []held{{target: p.Targets[i].Name, inst: in}}
		}
	}
	return nil
}

// agentProbe returns the probe that reads the panel, when the panel is an
// agent's: a probe claims it and its foreground is not a shell.
func (c *Core) agentProbe(panel revier.Panel) (revier.AgentProbe, bool) {
	if panel.Kind == revier.PanelShell {
		return nil, false
	}
	return c.probeFor(panel)
}

// Wait blocks until the agent's status is one of until, and returns the state
// that matched. When ctx ends first it returns ctx's error with the last state
// read, which is how a caller tells a timeout from a failure.
func (c *Core) Wait(ctx context.Context, a Agent, until []revier.Status, poll time.Duration) (revier.AgentState, error) {
	state := a.State
	for !slices.Contains(until, state.Status) {
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-time.After(poll):
		}
		next, err := c.reread(ctx, a)
		if err != nil {
			// A host call cut short by the deadline is the deadline.
			if ctx.Err() != nil {
				return state, ctx.Err()
			}
			return state, err
		}
		state = next
	}
	return state, nil
}

// Prompt types one line into an agent and submits it. It refuses an agent that
// is waiting for the human, and one whose state is unknown, since either may
// be showing a dialog the Enter would answer.
//
// It returns once an idle agent has left idle, so a following Wait sees the
// turn it asked for rather than the rest before it. The state returned is the
// last one read: still idle means the agent was not seen to take the prompt.
// An agent that was already working queues the prompt, and nothing marks its
// arrival, so Prompt returns at once.
func (c *Core) Prompt(ctx context.Context, a Agent, text string, poll time.Duration) (revier.AgentState, error) {
	w, ok := c.Runtime.(revier.PanelWriter)
	if !ok || a.Ref.Host != c.Runtime.Name() {
		return a.State, fmt.Errorf("%w: %s", ErrNoWriter, a.Ref.Host)
	}
	switch a.State.Status {
	case revier.StatusAttention:
		return a.State, fmt.Errorf("%w: it shows a question or a permission dialog, where the Enter that submits a prompt would pick the highlighted option; answer it in the panel", ErrAttention)
	case revier.StatusUnknown:
		return a.State, errors.New("the agent's state is unknown, so it may be showing a dialog the Enter would answer")
	}
	if strings.ContainsAny(text, "\r\n") {
		return a.State, errors.New("a prompt is one line: a newline submits the text early and sends the rest as further prompts")
	}

	if err := w.SendText(ctx, a.Ref, a.Panel.ID, text); err != nil {
		return a.State, err
	}
	if err := w.SendText(ctx, a.Ref, a.Panel.ID, "\r"); err != nil {
		// Say so: prompting again would type the text a second time.
		return a.State, fmt.Errorf("the text reached the panel but the Enter did not, so it sits unsent in the composer; submit it there: %w", err)
	}
	if a.State.Status != revier.StatusIdle {
		return a.State, nil
	}
	for range PromptConfirmPolls {
		select {
		case <-ctx.Done():
			return a.State, nil
		case <-time.After(poll):
		}
		// A failed read proves nothing about a text already delivered, so it
		// only costs a poll. A probe that failed reads as unknown, not as an
		// error, and is no more proof of a turn.
		s, err := c.reread(ctx, a)
		if err == nil && s.Status != revier.StatusIdle && s.Status != revier.StatusUnknown {
			return s, nil
		}
	}
	return a.State, nil
}

// reread reads the agent's panel again, from one listing of its host.
func (c *Core) reread(ctx context.Context, a Agent) (revier.AgentState, error) {
	if a.link != nil {
		return c.rereadLink(ctx, a)
	}
	var host revier.Host
	for _, h := range c.allHosts() {
		if h.Name() == a.Ref.Host {
			host = h
		}
	}
	if host == nil {
		return revier.AgentState{}, fmt.Errorf("%w: no host named %q", ErrNoHost, a.Ref.Host)
	}
	instances, err := host.Instances(ctx)
	if err != nil {
		return revier.AgentState{}, fmt.Errorf("%s: instances: %w", host.Name(), err)
	}
	for _, inst := range instances {
		if inst.Ref.ID != a.Ref.ID {
			continue
		}
		for _, panel := range inst.Panels {
			if panel.ID != a.Panel.ID {
				continue
			}
			if probe, ok := c.agentProbe(panel); ok {
				return c.read(ctx, probe, inst.Ref, panel), nil
			}
		}
	}
	return revier.AgentState{}, fmt.Errorf("%w: %s", ErrAgentGone, a.Panel.ID)
}

// Details is what each agent in the views said last, in the views' order, for
// the pane that shows one project's agents (revier.Detailed). The panels are
// the ones the last listing found, so a detail asks no host anything; an
// agent that listing did not hold has no detail until the next survey.
//
// A detail is empty for an agent whose probe does not say, one on another
// machine, and one whose probe failed: the detail is display, and the failure
// is logged rather than shown (decisions.md D106).
func (c *Core) Details(ctx context.Context, agents []revier.AgentView) []revier.AgentDetail {
	c.seenMu.Lock()
	seen := c.seen
	c.seenMu.Unlock()
	out := make([]revier.AgentDetail, len(agents))
	for i, a := range agents {
		for _, inst := range seen[a.Ref.Host] {
			if inst.Ref.ID != a.Ref.ID {
				continue
			}
			for _, panel := range inst.Panels {
				if panel.ID == a.Panel {
					out[i] = c.detail(ctx, inst.Ref, panel)
				}
			}
		}
	}
	return out
}

func (c *Core) detail(ctx context.Context, ref revier.TargetRef, panel revier.Panel) revier.AgentDetail {
	probe, ok := c.agentProbe(panel)
	if !ok {
		return revier.AgentDetail{}
	}
	detailed, ok := probe.(revier.Detailed)
	if !ok {
		return revier.AgentDetail{}
	}
	d, err := detailed.Detail(ctx, panel)
	logging.Repeat("detail\x00"+probe.Name()+"\x00"+key(ref)+"\x00"+string(panel.ID), "detail", err, "probe", probe.Name(), "ref", ref, "panel", panel.ID)
	if err != nil {
		return revier.AgentDetail{}
	}
	return d
}
