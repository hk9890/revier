package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// A link's agent is a process on its host, shown by a panel here that runs an
// ssh (decisions.md D83). The host's revier says what the agent does; every
// act on it - going to it, typing into it, closing it - is an act on the panel
// here. The two are one agent by the tag the panel gave the process: this
// machine's name and the panel's pid.

// ErrAgentElsewhere means the host reports an agent no panel here shows: one
// started from another machine, or whose terminal is gone while its ssh on the
// host has not yet ended.
var ErrAgentElsewhere = errors.New("the agent runs on its host and no panel here shows it")

var machine = sync.OnceValue(func() string {
	name, _ := os.Hostname()
	return name
})

// tagOf is the tag the panel gave what it started on a host.
func (c *Core) tagOf(panel revier.Panel) revier.PanelID {
	name := c.Machine
	if name == "" {
		name = machine()
	}
	return revier.PanelID(name + "." + strconv.Itoa(panel.PID))
}

// shown is where a tag is shown here.
type shown struct {
	ref   revier.TargetRef
	panel revier.Panel
}

// tags indexes the runtime's panels by the tag each would have given. A panel
// that started nothing on a host has a tag no host reports.
func (c *Core) tags(snap snapshot) map[revier.PanelID]shown {
	out := map[revier.PanelID]shown{}
	if c.Runtime == nil {
		return out
	}
	for _, inst := range snap[c.Runtime.Name()] {
		for _, panel := range inst.Panels {
			if panel.PID != 0 {
				out[c.tagOf(panel)] = shown{ref: inst.Ref, panel: panel}
			}
		}
	}
	return out
}

// localise names each agent the host reported by the panel here that shows
// it, so the surface reaches it as it reaches any agent. The activity is read
// from that panel's title, which crosses the ssh; the host's process has no
// title. An agent no panel here shows keeps the host's names, which nothing
// here can act on.
func (c *Core) localise(ctx context.Context, tags map[revier.PanelID]shown, agents []revier.AgentView) {
	for i, a := range agents {
		at, ok := tags[a.Panel]
		if !ok {
			continue
		}
		agents[i].Panel, agents[i].Ref = at.panel.ID, at.ref
		for _, probe := range c.Probes {
			if probe.Name() != a.State.Harness {
				continue
			}
			if titled, err := probe.Inspect(ctx, revier.Panel{Title: at.panel.Title}); err == nil && titled.Activity != "" {
				agents[i].State.Activity = titled.Activity
			}
		}
	}
}

// here reports whether an agent is in a panel of this machine's runtime.
func (c *Core) here(a revier.AgentView) bool {
	return c.Runtime != nil && a.Ref.Host == c.Runtime.Name()
}

// agentsHere is the agents a shutdown can close: every agent of a project on
// this machine, and of a link the ones a panel here shows.
func (c *Core) agentsHere(v revier.ProjectView) []revier.AgentView {
	if v.Project.Remote == nil {
		return v.Agents
	}
	var out []revier.AgentView
	for _, a := range v.Agents {
		if c.here(a) {
			out = append(out, a)
		}
	}
	return out
}

// linkAgents asks a link's host for its agents and names them by their panels
// here.
func (c *Core) linkAgents(ctx context.Context, p Project, snap snapshot) ([]revier.AgentView, error) {
	a := c.askRemote(ctx, p.Remote.Host, []Project{p})[p.Name]
	var v revier.ProjectView
	merge(&v, a)
	if v.Unreachable != "" {
		return nil, fmt.Errorf("%s: %s", p.Remote.Host, v.Unreachable)
	}
	c.localise(ctx, c.tags(snap), v.Agents)
	return v.Agents, nil
}

// linkAgent finds the agent addr names in a link: with addr empty, the link's
// only agent shown here; otherwise the panel here with that id.
func (c *Core) linkAgent(ctx context.Context, p Project, addr string, snap snapshot) (Agent, error) {
	agents, err := c.linkAgents(ctx, p, snap)
	if err != nil {
		return Agent{}, err
	}
	var found []Agent
	elsewhere := 0
	for _, a := range agents {
		if !c.here(a) {
			elsewhere++
			continue
		}
		if addr != "" && string(a.Panel) != addr {
			continue
		}
		inst, _ := byRef(snap, a.Ref)
		for _, panel := range inst.Panels {
			if panel.ID == a.Panel {
				found = append(found, Agent{Ref: a.Ref, Panel: panel, State: a.State, link: &p})
			}
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		addrs := ""
		for _, a := range found {
			addrs += " " + string(p.Name) + ":" + a.Panel.ID.String()
		}
		return Agent{}, fmt.Errorf("%s %w: use one of%s", p.Name, ErrAmbiguous, addrs)
	case elsewhere > 0:
		return Agent{}, fmt.Errorf("%s: %w", p.Name, ErrAgentElsewhere)
	}
	return Agent{}, fmt.Errorf("%s: %w", p.Name, ErrNoAgent)
}

// rereadLink asks the host for the agent's state again.
func (c *Core) rereadLink(ctx context.Context, a Agent) (revier.AgentState, error) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		return revier.AgentState{}, err
	}
	agents, err := c.linkAgents(ctx, *a.link, snap)
	if err != nil {
		return revier.AgentState{}, err
	}
	for _, next := range agents {
		if next.Panel == a.Panel.ID && key(next.Ref) == key(a.Ref) {
			return next.State, nil
		}
	}
	return revier.AgentState{}, ErrAgentGone
}

// plainWord is an argument that crosses the shells between a link's panel and
// `revier agent exec` on the host as itself, with no quoting
// (config.RemotePanel).
var plainWord = regexp.MustCompile(`^[A-Za-z0-9._/:=@+~-]+$`)

// startLinkAgent points a link's agent panel at a recorded conversation. The
// panel's command is the ssh that runs `revier agent exec` on the host, and
// the conversation and its directory go to it as arguments: whether the
// directory is still there and which harness resumes are the host's to say,
// and it starts the agent empty when it cannot.
func startLinkAgent(spec *revier.PanelSpec, r Resume) AgentOutcome {
	if r.Session == "" || !plainWord.MatchString(string(r.Session)) {
		return AgentEmpty
	}
	command := append(append([]string(nil), spec.Command...), "--resume", string(r.Session))
	if r.Dir != "" && plainWord.MatchString(r.Dir) {
		command = append(command, "--dir", r.Dir)
	}
	spec.Command = command
	return AgentResumed
}

// conversationsThere asks each link's host which conversation each of its
// agents holds, one call per host, and answers by link and by the tag the
// host names the agent with. A host that does not answer is returned as a
// failure of the save, and its agents are recorded by nothing.
func (c *Core) conversationsThere(ctx context.Context, views []revier.ProjectView) (map[revier.ProjectName]map[revier.PanelID]session.Agent, []error) {
	byHost := map[string][]revier.ProjectView{}
	for _, v := range views {
		if v.Project.Remote != nil && v.Running {
			byHost[v.Project.Remote.Host] = append(byHost[v.Project.Remote.Host], v)
		}
	}
	out := map[revier.ProjectName]map[revier.PanelID]session.Agent{}
	var failed []error
	for host, links := range byHost {
		names := make([]revier.ProjectName, len(links))
		for i, v := range links {
			names[i] = v.Project.Remote.Project
		}
		r, err := c.remote(host)
		var named []revier.ProjectView
		if err == nil {
			named, err = r.Conversations(ctx, names)
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", host, err))
			continue
		}
		for _, v := range links {
			agents := map[revier.PanelID]session.Agent{}
			for _, there := range named {
				if there.Project.Name != v.Project.Remote.Project {
					continue
				}
				for _, a := range there.Agents {
					agent := session.Agent{Harness: a.State.Harness}
					if a.Conversation != nil {
						agent.Session, agent.Dir = a.Conversation.ID, a.Conversation.Dir
					}
					agents[a.Panel] = agent
				}
			}
			out[v.Project.Name] = agents
		}
	}
	return out, failed
}

// NameConversations fills in the conversation each surveyed agent holds: the
// answer to Remote.Conversations, which a save on another machine records. An
// agent whose probe cannot say keeps none.
func (c *Core) NameConversations(ctx context.Context, r Report) {
	var agents []agentPanel
	var at []*revier.AgentView
	for i := range r.Views {
		for j := range r.Views[i].Agents {
			a := &r.Views[i].Agents[j]
			for _, inst := range r.Instances {
				if key(inst.Ref) != key(a.Ref) {
					continue
				}
				for _, panel := range inst.Panels {
					if probe, ok := c.probeFor(panel); ok && panel.ID == a.Panel {
						agents = append(agents, agentPanel{panel: panel, probe: probe})
						at = append(at, a)
					}
				}
			}
		}
	}
	named, _ := c.conversations(ctx, agents)
	for i, conv := range named {
		if conv != (revier.Conversation{}) {
			at[i].Conversation = &conv
		}
	}
}
