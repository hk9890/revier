package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"

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

	// ErrNoMessage means nothing here can say what the agent said last: its
	// probe reads no conversation, the conversation is on another machine, or
	// it has said nothing yet. It is a normal outcome, and what the agent's
	// panel shows can still be read (Screen).
	ErrNoMessage = errors.New("the agent's last message cannot be read")
)

// AgentPoll is how often Wait and Prompt read an agent again. LinkPoll is
// the same for a link's agent, whose every read is an ssh to its host.
const (
	AgentPoll = 500 * time.Millisecond
	LinkPoll  = 2 * time.Second
)

// KeyGap is the pause between two keys of one SendKeys call. Two keys that reach
// a terminal program in one read are one key to it: an Esc directly before a
// letter is alt and that letter.
const KeyGap = 50 * time.Millisecond

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

	// target is the target the agent was found through, empty for a link's
	// agent no target here holds.
	target revier.TargetName
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

// Agent finds the agent panel addr names in a project. addr is what follows
// the project in the agent's address: empty, for the project's only agent; a
// target name, for the only agent in that target; or a target and a panel id,
// <target>:<panel>, for that panel of the target (decisions.md D155). Where
// several agents run in what addr names, the only one that is no named
// agent's is the default agent, and the one meant (D145).
func (c *Core) Agent(ctx context.Context, p Project, addr string) (Agent, error) {
	a, err := parseAddress(p, addr)
	if err != nil {
		return Agent{}, err
	}
	bound := c.state().Bound[p.Name]
	// Agents are panels, and only the runtime and the served processes
	// list instances with panels.
	snap, err := c.answered(ctx, nameOf(c.Runtime), nameOf(c.Served))
	if err != nil {
		return Agent{}, err
	}
	scope := c.running(snap, p, bound, a.target)
	if a.target != "" && len(scope) == 0 {
		return Agent{}, fmt.Errorf("%s is not running", a.where)
	}
	if p.Remote != nil {
		return c.linkAgent(ctx, p, a, scope, snap, bound)
	}

	var found []Agent
	for _, h := range scope {
		for _, panel := range h.inst.Panels {
			if a.byPanel && panel.ID != a.panel {
				continue
			}
			probe, ok := c.agentProbe(panel)
			if !ok {
				if a.byPanel {
					return Agent{}, fmt.Errorf("%s is %w: %s", a.where, ErrNotAgent, notAgentReason(panel))
				}
				continue
			}
			found = append(found, Agent{Ref: h.inst.Ref, Panel: panel, State: c.read(ctx, probe, h.inst.Ref, panel), target: h.target})
		}
	}
	switch {
	case len(found) == 0 && a.byPanel:
		return Agent{}, a.noPanel()
	case len(found) == 0:
		return Agent{}, fmt.Errorf("%s: %w", a.where, ErrNoAgent)
	case len(found) == 1:
		return found[0], nil
	}
	if def, ok := defaultAgent(found); ok && !a.byPanel {
		return def, nil
	}
	return Agent{}, c.ambiguous(snap, p, bound, a.where, found)
}

// address is what follows the project in an agent's address, parsed: where
// is the whole of it, for a refusal to name.
type address struct {
	where   string
	target  revier.TargetName
	panel   revier.PanelID
	byPanel bool
}

// parseAddress refuses an address whose target the project does not declare
// before any host is listed: the removed <project>:<panel> lands here.
func parseAddress(p Project, addr string) (address, error) {
	target, panel, byPanel := strings.Cut(addr, ":")
	a := address{where: string(p.Name), target: revier.TargetName(target), panel: revier.PanelID(panel), byPanel: byPanel}
	if addr == "" {
		return a, nil
	}
	a.where += ":" + addr
	if _, ok := p.index(a.target); !ok {
		return address{}, fmt.Errorf("%s has no target %q: an agent's address is <project>, <project>:<target> or <project>:<target>:<panel>", p.Name, target)
	}
	return a, nil
}

// noPanel refuses an address whose panel its target does not hold.
func (a address) noPanel() error {
	return fmt.Errorf("%s: %s has no running panel %q", a.where, a.target, a.panel)
}

// ambiguous refuses where, which names every agent of found, with the
// address of each. An agent no target holds - a link's tab moved to another
// window - has none, and is named by its panel, which only the agent list
// reaches (decisions.md D155).
func (c *Core) ambiguous(snap snapshot, p Project, bound Bindings, where string, found []Agent) error {
	tabs := c.tabPlaces(snap, p, bound)
	var addrs, loose []string
	for _, a := range found {
		t := tabs.of(a.Ref, a.Panel.ID)
		if t == "" {
			t = a.target
		}
		if t != "" {
			addrs = append(addrs, Address(p.Name, t, a.Panel.ID))
		} else {
			loose = append(loose, a.Panel.ID.String())
		}
	}
	var hints []string
	if len(addrs) > 0 {
		hints = append(hints, "use one of "+strings.Join(addrs, ", "))
	}
	if len(loose) > 0 {
		hints = append(hints, fmt.Sprintf("no target of %s holds panel %s, which only the agent list reaches", p.Name, strings.Join(loose, ", ")))
	}
	return fmt.Errorf("%s %w: %s", where, ErrAmbiguous, strings.Join(hints, "; "))
}

// Address is an agent's address: its project, the target that holds it and
// its panel (decisions.md D155). A panel id is one process's - a kitty window
// id is - and the target names the process, so the three name one agent.
func Address(project revier.ProjectName, target revier.TargetName, panel revier.PanelID) string {
	return string(project) + ":" + string(target) + ":" + panel.String()
}

// places is the target each panel of one project's instances is addressed
// through: the first open tab target, in the project's order, that lists it,
// else the first target whose instance holds it, as running walks them. Each
// is a target Agent finds the panel in, so the address made of it names the
// agent back. The survey labels its agents by the same rule, from the walk
// view does (TestEveryAgentsAddressNamesItBack holds the two together).
type places struct {
	tabs map[string]revier.TargetName
	refs map[string]revier.TargetName
}

func (c *Core) places(snap snapshot, p Project, bound Bindings) places {
	pl := c.tabPlaces(snap, p, bound)
	pl.refs = map[string]revier.TargetName{}
	for _, h := range c.running(snap, p, bound, "") {
		pl.refs[key(h.inst.Ref)] = h.target
	}
	return pl
}

// tabPlaces is places of the open tabs alone.
func (c *Core) tabPlaces(snap snapshot, p Project, bound Bindings) places {
	pl := places{tabs: map[string]revier.TargetName{}}
	for i := range p.Targets {
		if p.isTab(i) {
			pl.addTab(c.heldTab(snap, p, i, bound))
		}
	}
	return pl
}

// addTab places the panels of an open tab, each under the first tab that
// lists it.
func (pl places) addTab(tab []held) {
	for _, h := range tab {
		for _, panel := range h.inst.Panels {
			k := key(h.inst.Ref) + "\x00" + string(panel.ID)
			if _, ok := pl.tabs[k]; !ok {
				pl.tabs[k] = h.target
			}
		}
	}
}

// of is the target the panel of the instance ref is addressed through, empty
// for one no target holds.
func (pl places) of(ref revier.TargetRef, panel revier.PanelID) revier.TargetName {
	if t, ok := pl.tabs[key(ref)+"\x00"+string(panel)]; ok {
		return t
	}
	return pl.refs[key(ref)]
}

// label sets the target of each agent that pl places, and leaves the target
// of any other as it is.
func (pl places) label(agents []revier.AgentView) {
	for i, a := range agents {
		if t := pl.of(a.Ref, a.Panel); t != "" {
			agents[i].Target = t
		}
	}
}

// defaultAgent is the only agent of found that carries no named agent's mark.
func defaultAgent(found []Agent) (Agent, bool) {
	var def []Agent
	for _, a := range found {
		if a.Panel.Vars[PanelAgentVar] == "" {
			def = append(def, a)
		}
	}
	if len(def) != 1 {
		return Agent{}, false
	}
	return def[0], true
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
			// A tab's panels are listed with the instance that holds it, and
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
// narrowed to the panels of the tab. Every one of them is kept, so the agent
// of a tab that declares its shell first is found. They are the panels that
// carry the tab's mark, and the ones the runtime lists in the tab of the first
// of those: the mark of Open is best effort, so the panel that created the
// instance can be without it while the one split in after it has it.
func (c *Core) heldTab(snap snapshot, p Project, i int, bound Bindings) []held {
	in, _, tab, open, err := c.container(snap, p, i, bound)
	if err != nil || !open {
		return nil
	}
	return []held{tabIn(in, p.Targets[i].Name, tab)}
}

// tabIn is the open tab name of the instance in, whose first panel is tab.
func tabIn(in revier.Instance, name revier.TargetName, tab revier.PanelID) held {
	sameTab := tabAt(in, tab)
	var panels []revier.Panel
	for _, panel := range in.Panels {
		if panel.Vars[PanelTargetVar] == string(name) || slices.Contains(sameTab, panel.ID) {
			panels = append(panels, panel)
		}
	}
	in.Panels = panels
	return held{target: name, inst: in}
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
	w, err := c.writer(a)
	if err != nil {
		return a.State, err
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

// writer is the runtime as it types into the agent's panel.
func (c *Core) writer(a Agent) (revier.PanelWriter, error) {
	w, ok := c.Runtime.(revier.PanelWriter)
	if !ok || a.Ref.Host != c.Runtime.Name() {
		return nil, fmt.Errorf("%w: %s", ErrNoWriter, a.Ref.Host)
	}
	return w, nil
}

// typedKeys are the keys SendKeys types by name, each as the bytes a terminal sends
// for it.
var typedKeys = map[string]string{
	"esc":       "\x1b",
	"enter":     "\r",
	"tab":       "\t",
	"shift+tab": "\x1b[Z",
	"space":     " ",
	"backspace": "\x7f",
	"up":        "\x1b[A",
	"down":      "\x1b[B",
	"right":     "\x1b[C",
	"left":      "\x1b[D",
	"ctrl+c":    "\x03",
}

// keyBytes is what a terminal sends for the key name names: one of typedKeys, or
// one printable character, which stands for itself.
func keyBytes(name string) (string, error) {
	if b, ok := typedKeys[strings.ToLower(name)]; ok {
		return b, nil
	}
	if r := []rune(name); len(r) == 1 && unicode.IsPrint(r[0]) {
		return name, nil
	}
	names := slices.Sorted(maps.Keys(typedKeys))
	return "", fmt.Errorf("unknown key %q: want one character, or one of %s", name, strings.Join(names, ", "))
}

// SendKeys types the named keys into an agent's panel, in order, gap apart. It
// asks nothing of the agent's state, where Prompt refuses two of them: a key
// is how a dialog is answered and how a working agent is interrupted, and
// the caller named each one (decisions.md D117). A name that is no key
// refuses them all, so nothing is typed up to a mistake.
//
// An error after the first key says how many keys arrived: sending them again
// would type those twice.
func (c *Core) SendKeys(ctx context.Context, a Agent, names []string, gap time.Duration) error {
	w, err := c.writer(a)
	if err != nil {
		return err
	}
	typed := make([]string, len(names))
	for i, name := range names {
		if typed[i], err = keyBytes(name); err != nil {
			return err
		}
	}
	for i, b := range typed {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%d of %d keys reached the panel: %w", i, len(typed), ctx.Err())
			case <-time.After(gap):
			}
		}
		if err := w.SendText(ctx, a.Ref, a.Panel.ID, b); err != nil {
			return fmt.Errorf("%d of %d keys reached the panel: %w", i, len(typed), err)
		}
	}
	return nil
}

// Said is the last thing the agent said, as its probe reads it
// (revier.Detailed): what `revier agent read` prints. The read is display,
// as it is on the surface, and nothing is decided by it (decisions.md D106,
// D116). An agent with no message to read answers ErrNoMessage; any other
// error is a probe that broke.
func (c *Core) Said(ctx context.Context, a Agent) (revier.AgentDetail, error) {
	if a.link != nil {
		return revier.AgentDetail{}, fmt.Errorf("%w: its conversation is on %s", ErrNoMessage, a.link.Remote.Host)
	}
	probe, ok := c.agentProbe(a.Panel)
	if !ok {
		return revier.AgentDetail{}, fmt.Errorf("%s is %w: %s", a.Panel.ID, ErrNotAgent, notAgentReason(a.Panel))
	}
	detailed, ok := probe.(revier.Detailed)
	if !ok {
		return revier.AgentDetail{}, fmt.Errorf("%w: the %s probe reads no conversation", ErrNoMessage, probe.Name())
	}
	d, err := detailed.Detail(ctx, a.Panel)
	if errors.Is(err, revier.ErrNoDetail) || (err == nil && d.Message == "") {
		return revier.AgentDetail{}, fmt.Errorf("%w: it has said nothing yet, or its harness lists no conversation for it", ErrNoMessage)
	}
	return d, err
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
// the surface that shows them (revier.Detailed). The panels are
// the ones the last listing found, so a detail asks no host anything; an
// agent that listing did not hold has no detail until the next survey.
//
// A detail is empty for an agent whose probe does not say, one on another
// machine, and one whose probe failed: the detail is display, and the failure
// is logged rather than shown (decisions.md D106). A probe with nothing to
// read (revier.ErrNoDetail) has not failed, and is not logged either.
func (c *Core) Details(ctx context.Context, agents []revier.AgentView) []revier.AgentDetail {
	c.seenMu.Lock()
	seen := c.seen
	c.seenMu.Unlock()
	// The agents are indexed first and the listing walked once. Walking the
	// listing per agent costs the instances of every host times the agents of
	// one project, on a surface that asks every refresh.
	wanted := make(map[string][]int, len(agents))
	for i, a := range agents {
		at := key(a.Ref) + "\x00" + string(a.Panel)
		wanted[at] = append(wanted[at], i)
	}
	out := make([]revier.AgentDetail, len(agents))
	for _, instances := range seen {
		for _, inst := range instances {
			for _, panel := range inst.Panels {
				at, ok := wanted[key(inst.Ref)+"\x00"+string(panel.ID)]
				if !ok {
					continue
				}
				d := c.detail(ctx, inst.Ref, panel)
				for _, i := range at {
					out[i] = d
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
	// A probe with nothing to read is a normal outcome and says nothing to
	// the log: a panel whose harness lists no session, and a session that has
	// not spoken yet, are both ordinary. Only a probe that broke is logged.
	if errors.Is(err, revier.ErrNoDetail) {
		return revier.AgentDetail{}
	}
	logging.Repeat("detail\x00"+probe.Name()+"\x00"+key(ref)+"\x00"+string(panel.ID), "detail", err, "probe", probe.Name(), "ref", ref, "panel", panel.ID)
	if err != nil {
		return revier.AgentDetail{}
	}
	return d
}
