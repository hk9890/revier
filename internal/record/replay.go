package record

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// State is what the record says was open at a moment. Since is what revier
// did after this machine's last survey line, which no survey has seen yet.
type State struct {
	At       time.Time            `json:"at"`
	Current  revier.ProjectName   `json:"current,omitempty"`
	Projects []revier.ProjectName `json:"projects"`
	Machines []Machine            `json:"machines"`
	Since    []Line               `json:"since,omitempty"`
}

// Machine is what was last seen of one scope: this machine first, then each
// linked host. Seen is its last survey line. StaleSince is set once it went
// Stale with no line, and Unknown when the host gave no answer or no survey
// of it is recorded. Then its entries are what was last seen, not what was
// open; so are those of a host or a link marked unknown.
type Machine struct {
	Machine      string                        `json:"machine,omitempty"`
	Seen         time.Time                     `json:"seen,omitzero"`
	StaleSince   time.Time                     `json:"stale_since,omitzero"`
	Unknown      string                        `json:"unknown,omitempty"`
	Links        []revier.ProjectName          `json:"links,omitempty"`
	UnknownLinks map[revier.ProjectName]string `json:"unknown_links,omitempty"`
	Hosts        []HostState                   `json:"hosts,omitempty"`
	Instances    []Instance                    `json:"instances,omitempty"`
	Targets      []Target                      `json:"targets,omitempty"`
	Agents       []Agent                       `json:"agents,omitempty"`
}

// HostState is one host of a machine, and why it did not list when it did
// not.
type HostState struct {
	Host    string `json:"host"`
	Unknown string `json:"unknown,omitempty"`
}

// NoSurvey is why a machine with no survey line is unknown.
const NoSurvey = "no survey recorded"

// known is a replay's view of one scope.
type known struct {
	base Seen
	seen time.Time
}

// Replay is the state the lines say was open at at. A snapshot replaces what
// was known of its scope, but keeps what a host that did not list held, and
// a difference is a set operation, so a line two surfaces both wrote changes
// nothing. A host that did not list, or a scope with no line for Stale, is
// unknown and never empty (decisions.md D89).
func Replay(lines []Line, at time.Time) State {
	lines = slices.Clone(lines)
	slices.SortStableFunc(lines, func(a, b Line) int { return a.Time.Compare(b.Time) })
	scopes := map[string]*known{}
	projects := map[revier.ProjectName]bool{}
	var current revier.ProjectName
	var did []Line
	for _, l := range lines {
		if l.Time.After(at) {
			break
		}
		if l.By == ByDid {
			if !project(l, projects, &current) {
				did = append(did, l)
			}
			continue
		}
		sc := scopes[l.Machine]
		if sc == nil {
			sc = &known{}
			scopes[l.Machine] = sc
		}
		sc.seen = l.Time
		if l.Kind == KindSnapshot && l.Seen != nil {
			sc.base = carry(sc.base, *l.Seen)
			if l.Machine == "" {
				projects = map[revier.ProjectName]bool{}
				for _, p := range l.Seen.Projects {
					projects[p] = true
				}
				current = l.Seen.Current
			}
			continue
		}
		apply(&sc.base, l)
	}

	st := State{At: at, Current: current, Projects: slices.Sorted(maps.Keys(projects))}
	here := scopes[""]
	if here == nil {
		st.Machines = append(st.Machines, Machine{Unknown: NoSurvey})
		here = &known{}
	}
	for _, name := range slices.Sorted(maps.Keys(scopes)) {
		st.Machines = append(st.Machines, machine(name, scopes[name], at))
	}
	for _, l := range did {
		if l.Time.After(here.seen) {
			st.Since = append(st.Since, l)
		}
	}
	return st
}

// project applies a line that changes the project list or the current
// project, and reports whether it did.
func project(l Line, projects map[revier.ProjectName]bool, current *revier.ProjectName) bool {
	if l.Err != "" {
		return false
	}
	switch l.Kind {
	case KindProjectCreated, KindProjectLinked:
		projects[l.Project] = true
	case KindProjectRenamed:
		delete(projects, revier.ProjectName(l.From))
		projects[l.Project] = true
		if *current == revier.ProjectName(l.From) {
			*current = l.Project
		}
	case KindProjectDeleted:
		delete(projects, l.Project)
	case KindProjectSelected:
		*current = l.Project
	default:
		return false
	}
	return true
}

// apply is a difference line on what is known of its scope.
func apply(s *Seen, l Line) {
	switch l.Kind {
	case KindInstanceOpened, KindInstanceClosed:
		i := Instance{Host: l.Host, Ref: l.Ref, Title: l.Title}
		s.Instances = slices.DeleteFunc(s.Instances, func(o Instance) bool { return instanceKey(o) == instanceKey(i) })
		if l.Kind == KindInstanceOpened {
			s.Instances = append(s.Instances, i)
		}
	case KindTargetOpened, KindTargetClosed:
		t := Target{Project: l.Project, Target: l.Target, Host: l.Host, Ref: l.Ref, Attached: l.Attached, Tab: l.Tab}
		s.Targets = slices.DeleteFunc(s.Targets, func(o Target) bool { return o == t })
		if l.Kind == KindTargetOpened {
			s.Targets = append(s.Targets, t)
		}
	case KindAgentOpened, KindAgentClosed, KindAgentSession:
		a := Agent{
			Project: l.Project, Host: l.Host, Ref: l.Ref, Panel: l.Panel,
			Harness: l.Harness, Session: l.Session, Dir: l.Dir, Agent: l.Agent,
		}
		s.Agents = slices.DeleteFunc(s.Agents, func(o Agent) bool { return agentKey(o) == agentKey(a) })
		if l.Kind != KindAgentClosed {
			s.Agents = append(s.Agents, a)
		}
	case KindHostUnreachable:
		switch {
		case l.Host != "":
			s.Unlisted = with(s.Unlisted, l.Host, l.Err)
			s.Hosts = slices.DeleteFunc(s.Hosts, func(h string) bool { return h == l.Host })
		case l.Project != "":
			s.Unknown = with(s.Unknown, l.Project, l.Err)
		default:
			s.Unreachable = l.Err
		}
	case KindHostBack:
		switch {
		case l.Host != "":
			delete(s.Unlisted, l.Host)
			if !slices.Contains(s.Hosts, l.Host) {
				s.Hosts = append(s.Hosts, l.Host)
			}
		case l.Project != "":
			delete(s.Unknown, l.Project)
		default:
			s.Unreachable = ""
		}
	}
}

func with[K comparable](m map[K]string, k K, v string) map[K]string {
	m = maps.Clone(m)
	if m == nil {
		m = map[K]string{}
	}
	m[k] = v
	return m
}

// machine is what a replay at at says of one scope, its entries in order.
func machine(name string, sc *known, at time.Time) Machine {
	m := Machine{
		Machine: name, Seen: sc.seen, Unknown: sc.base.Unreachable,
		UnknownLinks: sc.base.Unknown,
		Instances:    slices.Clone(sc.base.Instances),
		Targets:      slices.Clone(sc.base.Targets),
		Agents:       slices.Clone(sc.base.Agents),
	}
	if name != "" {
		m.Links = slices.Sorted(slices.Values(sc.base.Projects))
	}
	if at.Sub(sc.seen) > Stale {
		m.StaleSince = sc.seen.Add(Stale)
	}
	for _, h := range sc.base.Hosts {
		m.Hosts = append(m.Hosts, HostState{Host: h})
	}
	for h, why := range sc.base.Unlisted {
		m.Hosts = append(m.Hosts, HostState{Host: h, Unknown: why})
	}
	slices.SortFunc(m.Hosts, func(a, b HostState) int { return strings.Compare(a.Host, b.Host) })
	slices.SortFunc(m.Instances, func(a, b Instance) int { return strings.Compare(a.Host+"\x00"+a.Ref, b.Host+"\x00"+b.Ref) })
	slices.SortFunc(m.Targets, func(a, b Target) int {
		return strings.Compare(targetOrder(a), targetOrder(b))
	})
	slices.SortFunc(m.Agents, func(a, b Agent) int {
		return strings.Compare(agentOrder(a), agentOrder(b))
	})
	return m
}

func targetOrder(t Target) string {
	return strings.Join([]string{string(t.Project), string(t.Target), t.Host, t.Ref}, "\x00")
}

func agentOrder(a Agent) string {
	return strings.Join([]string{string(a.Project), a.Host, a.Ref, string(a.Panel)}, "\x00")
}

// unknown is why what m holds of a host and a project is not known to be
// open, or "" when it is.
func (m Machine) unknown(host string, project revier.ProjectName) string {
	switch {
	case m.Unknown != "":
		return m.Unknown
	case !m.StaleSince.IsZero():
		return "no survey since " + m.Seen.Format(time.TimeOnly)
	case m.UnknownLinks[project] != "":
		return m.UnknownLinks[project]
	}
	for _, h := range m.Hosts {
		if h.Host == host && h.Unknown != "" {
			return h.Unknown
		}
	}
	return ""
}

// Why a save and the state it was made in differ.
const (
	// NotAsked is a linked host the save did not ask: a close asks only the
	// hosts it is about (decisions.md D115).
	NotAsked = "not asked by the save"
	// UnreachableAtSave is a host that did not list, or did not answer, for
	// the save.
	UnreachableAtSave = "unreachable at the save"
	// AttachedByHand is an instance attached by hand, which a save leaves
	// out: it has no target name to come back as.
	AttachedByHand = "attached"
	// InItsInstance is a tab that comes back with the instance that holds
	// it, which a save records as no target of its own.
	InItsInstance = "a tab of its instance"
	// NoConversation is an agent the save recorded with no conversation, or
	// by its harness alone, as the agent of a tab that runs a launch is
	// (decisions.md D68).
	NoConversation = "no conversation"
	// HostUnknown is a host whose state the record does not know then.
	HostUnknown = "host unknown"
	// Unexplained is a difference none of these explains.
	Unexplained = "unexplained"
)

// Difference is one target or agent that the state and a save do not both
// hold. In is the side that holds it: "state" or "session".
type Difference struct {
	Machine string  `json:"machine,omitempty"`
	Target  *Target `json:"target,omitempty"`
	Agent   *Agent  `json:"agent,omitempty"`
	In      string  `json:"in"`
	Why     string  `json:"why"`
}

// Compare replays the lines at the save of s, its own snapshot lines
// included, and returns that state with each difference from s and why it
// is one. The save's time is its last line, `session saved` or a snapshot,
// else s.At for a save that wrote none. A save of what a surface last saw is
// of s.At, when the surface wrote it, and not of the start that kept it.
func Compare(lines []Line, s session.Session) (State, []Difference) {
	at := s.At
	asked := map[string]Seen{}
	for _, l := range lines {
		if l.Saved == "" || l.Saved != s.ID || (l.Kind != KindSnapshot && l.Kind != KindSessionSaved) || l.Reason == session.ReasonLastSeen {
			continue
		}
		at = l.Time
		if l.Kind == KindSnapshot && l.Seen != nil {
			asked[l.Machine] = *l.Seen
		}
	}
	st := Replay(lines, at)
	c := comparison{state: st, asked: asked}
	return st, c.differences(s)
}

type comparison struct {
	state State
	asked map[string]Seen
	out   []Difference
}

// savedAgent is an agent a save recorded, and whether a survey's agent
// matched it.
type savedAgent struct {
	session.Agent
	target  revier.TargetName
	matched bool
}

func (c *comparison) differences(s session.Session) []Difference {
	targets := map[revier.ProjectName]map[revier.TargetName]bool{}
	agents := map[revier.ProjectName][]*savedAgent{}
	for _, p := range s.Projects {
		targets[p.Name] = map[revier.TargetName]bool{}
		for _, t := range p.Targets {
			targets[p.Name][t.Name] = true
			for _, a := range t.Agents {
				agents[p.Name] = append(agents[p.Name], &savedAgent{Agent: a, target: t.Name})
			}
		}
	}

	open := map[revier.ProjectName]map[revier.TargetName]bool{}
	var unsaved []Agent
	var unsavedAt []string
	for _, m := range c.state.Machines {
		for _, t := range m.Targets {
			switch {
			case t.Attached:
				c.add(m.Machine, &t, nil, "state", AttachedByHand)
			case t.Tab && !targets[t.Project][t.Target]:
				c.add(m.Machine, &t, nil, "state", InItsInstance)
			case targets[t.Project][t.Target]:
				if open[t.Project] == nil {
					open[t.Project] = map[revier.TargetName]bool{}
				}
				open[t.Project][t.Target] = true
			default:
				c.add(m.Machine, &t, nil, "state", c.why(m, t.Host, t.Project, false))
			}
		}
		for _, a := range m.Agents {
			if a.Session != "" && claim(agents[a.Project], func(sa *savedAgent) bool {
				return sa.Harness == a.Harness && sa.Session == a.Session
			}) {
				continue
			}
			unsaved, unsavedAt = append(unsaved, a), append(unsavedAt, m.Machine)
		}
	}
	// An agent recorded with no conversation holds the place of one the
	// survey saw with the same harness.
	for i, a := range unsaved {
		m := c.machine(unsavedAt[i])
		if claim(agents[a.Project], func(sa *savedAgent) bool { return sa.Harness == a.Harness && sa.Session == "" }) {
			if a.Session != "" {
				c.add(m.Machine, nil, &a, "state", NoConversation)
			}
			continue
		}
		c.add(m.Machine, nil, &a, "state", c.why(m, a.Host, a.Project, a.Session == ""))
	}

	for _, p := range s.Projects {
		m := c.machineOf(p.Name)
		for _, t := range p.Targets {
			if !open[p.Name][t.Name] {
				c.add(m.Machine, &Target{Project: p.Name, Target: t.Name}, nil, "session", c.whySaved(m, p.Name))
			}
		}
		for _, sa := range agents[p.Name] {
			if !sa.matched {
				a := Agent{Project: p.Name, Harness: sa.Harness, Session: sa.Session, Dir: sa.Dir, Agent: sa.Name}
				c.add(m.Machine, nil, &a, "session", c.whySaved(m, p.Name))
			}
		}
	}
	return c.out
}

// claim marks the first saved agent that is not matched yet and fits, and
// reports whether there was one.
func claim(saved []*savedAgent, fits func(*savedAgent) bool) bool {
	for _, sa := range saved {
		if !sa.matched && fits(sa) {
			sa.matched = true
			return true
		}
	}
	return false
}

func (c *comparison) add(machine string, t *Target, a *Agent, in, why string) {
	c.out = append(c.out, Difference{Machine: machine, Target: t, Agent: a, In: in, Why: why})
}

// why is the class of what the state holds of host and project on m and the
// save does not.
func (c *comparison) why(m Machine, host string, project revier.ProjectName, noConversation bool) string {
	saw, asked := c.asked[m.Machine]
	switch {
	case m.Machine != "" && len(c.asked) > 0 && !asked:
		return NotAsked
	case asked && (saw.Unreachable != "" || saw.Unlisted[host] != "" || saw.Unknown[project] != ""):
		return UnreachableAtSave
	case noConversation:
		return NoConversation
	case m.unknown(host, project) != "":
		return HostUnknown
	}
	return Unexplained
}

// whySaved is the class of what the save holds of project and the state
// does not. The save does not name the host of a target, so any host of m
// the record does not know explains it.
func (c *comparison) whySaved(m Machine, project revier.ProjectName) string {
	if m.unknown("", project) != "" || slices.ContainsFunc(m.Hosts, func(h HostState) bool { return h.Unknown != "" }) {
		return HostUnknown
	}
	return Unexplained
}

func (c *comparison) machine(name string) Machine {
	for _, m := range c.state.Machines {
		if m.Machine == name {
			return m
		}
	}
	return Machine{Machine: name}
}

// machineOf is the linked host project is a link to, else this machine.
func (c *comparison) machineOf(project revier.ProjectName) Machine {
	for _, m := range c.state.Machines {
		if m.Machine != "" && slices.Contains(m.Links, project) {
			return m
		}
	}
	return c.machine("")
}
