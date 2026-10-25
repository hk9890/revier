package record

import (
	"slices"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// Beat is how long a scope goes with no line before a survey that sees no
// change writes one, so a replay tells a quiet desktop from no surface.
const Beat = 5 * time.Minute

// Stale is how long a scope goes with no line before a replay calls what it
// last saw unknown: one Beat and the minute a survey may run late.
const Stale = Beat + time.Minute

// Observer turns a surface's surveys into lines, one scope at a time: this
// machine, or one linked host. It is the surface's own, since a difference
// is between two surveys of one process.
type Observer struct {
	scopes map[string]*scope
}

// scope is what an Observer last saw of one scope, and when it last wrote a
// line about it.
type scope struct {
	base  Seen
	wrote time.Time
}

// NewObserver returns an Observer that has seen nothing.
func NewObserver() *Observer { return &Observer{scopes: map[string]*scope{}} }

// Saw returns the lines a survey of s at now writes. The first survey of a
// scope, the first of a day and one that sees another project list are a
// whole snapshot, so a replay can start there. Any other is the difference
// from the last: what opened, closed or changed its conversation, and which
// host stopped or started answering. A survey that sees no change writes
// nothing, unless the scope went a Beat with no line. Titles and an agent's
// status are not compared: they change on every refresh.
func (o *Observer) Saw(s Seen, now time.Time) []Line {
	sc, known := o.scopes[s.Machine]
	if !known {
		sc = &scope{}
		o.scopes[s.Machine] = sc
	}
	next := carry(sc.base, s)
	var lines []Line
	if !known || day(now) != day(sc.wrote) || !sameProjects(sc.base.Projects, s.Projects) {
		snapshot := next
		lines = []Line{{Kind: KindSnapshot, Seen: &snapshot}}
	} else {
		lines = changes(sc.base, next)
	}
	if len(lines) == 0 && now.Sub(sc.wrote) >= Beat {
		lines = []Line{{Kind: KindSurvey}}
	}
	sc.base = next
	for i := range lines {
		lines[i].Time, lines[i].By, lines[i].Machine = now, BySaw, s.Machine
	}
	if len(lines) > 0 {
		sc.wrote = now
	}
	return lines
}

// carry is what is known of a scope after a survey of s: what s saw, and of
// last what s could not see, because its host did not list, the linked host
// did not answer, or it gave no answer for that link. That is not gone, and
// is kept as it was last seen (decisions.md D89). A snapshot already holds
// what its writer kept, so a replay that starts there keeps it too.
func carry(last, s Seen) Seen {
	held := func(host string, project revier.ProjectName) bool {
		_, unlisted := s.Unlisted[host]
		_, unknown := s.Unknown[project]
		return s.Unreachable != "" || unlisted || (project != "" && unknown)
	}
	next := s
	next.Instances = keep(s.Instances, last.Instances, instanceKey, func(i Instance) bool { return held(i.Host, "") })
	next.Targets = keep(s.Targets, last.Targets, targetKey, func(t Target) bool { return held(t.Host, t.Project) })
	next.Agents = keep(s.Agents, last.Agents, agentKey, func(a Agent) bool { return held(a.Host, a.Project) })
	return next
}

// keep is the entries of seen, then those of last that are held and that
// seen does not hold already.
func keep[T any, K comparable](seen, last []T, keyOf func(T) K, held func(T) bool) []T {
	out := slices.Clone(seen)
	has := make(map[K]bool, len(seen))
	for _, e := range seen {
		has[keyOf(e)] = true
	}
	for _, e := range last {
		if held(e) && !has[keyOf(e)] {
			out = append(out, e)
		}
	}
	return out
}

func sameProjects(a, b []revier.ProjectName) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// changes is the lines that take a replay from last to next: the hosts first,
// then the instances, the targets and the agents.
func changes(last, next Seen) []Line {
	var out []Line
	reach := func(was, is bool, l Line, why string) {
		switch {
		case is && !was:
			l.Kind, l.Err = KindHostUnreachable, why
			out = append(out, l)
		case was && !is:
			l.Kind = KindHostBack
			out = append(out, l)
		}
	}
	reach(last.Unreachable != "", next.Unreachable != "", Line{}, next.Unreachable)
	for _, host := range sortedKeys(last.Unlisted, next.Unlisted) {
		_, was := last.Unlisted[host]
		_, is := next.Unlisted[host]
		reach(was, is, Line{Host: host}, next.Unlisted[host])
	}
	for _, project := range sortedKeys(last.Unknown, next.Unknown) {
		_, was := last.Unknown[project]
		_, is := next.Unknown[project]
		reach(was, is, Line{Project: project}, next.Unknown[project])
	}

	out = append(out, diff(last.Instances, next.Instances, instanceKey, nil, KindInstanceOpened, KindInstanceClosed, "", instanceLine)...)
	out = append(out, diff(last.Targets, next.Targets, targetKey, nil, KindTargetOpened, KindTargetClosed, "", targetLine)...)
	out = append(out, diff(last.Agents, next.Agents, agentKey, func(a, b Agent) bool { return a == b }, KindAgentOpened, KindAgentClosed, KindAgentSession, agentLine)...)
	return out
}

// diff is a line for each entry of next whose key last lacks, each of last
// whose key next lacks, and, given same, each that kept its key and changed.
func diff[T any, K comparable](last, next []T, keyOf func(T) K, same func(T, T) bool, opened, closed, changed string, line func(string, T) Line) []Line {
	was := make(map[K]T, len(last))
	for _, e := range last {
		was[keyOf(e)] = e
	}
	is := make(map[K]bool, len(next))
	var out []Line
	for _, e := range next {
		is[keyOf(e)] = true
		old, ok := was[keyOf(e)]
		switch {
		case !ok:
			out = append(out, line(opened, e))
		case same != nil && !same(old, e):
			out = append(out, line(changed, e))
		}
	}
	for _, e := range last {
		if !is[keyOf(e)] {
			out = append(out, line(closed, e))
		}
	}
	return out
}

type instanceID struct{ host, ref string }

func instanceKey(i Instance) instanceID { return instanceID{i.Host, i.Ref} }

func targetKey(t Target) Target { return t }

// agentID is an agent's place: a panel of an instance in a project. What it
// runs and which conversation it holds can change in that place.
type agentID struct {
	project   revier.ProjectName
	host, ref string
	panel     revier.PanelID
}

func agentKey(a Agent) agentID { return agentID{a.Project, a.Host, a.Ref, a.Panel} }

func instanceLine(kind string, i Instance) Line {
	return Line{Kind: kind, Host: i.Host, Ref: i.Ref, Title: i.Title}
}

func targetLine(kind string, t Target) Line {
	return Line{Kind: kind, Project: t.Project, Target: t.Target, Host: t.Host, Ref: t.Ref, Attached: t.Attached, Tab: t.Tab}
}

func agentLine(kind string, a Agent) Line {
	return Line{
		Kind: kind, Project: a.Project, Host: a.Host, Ref: a.Ref, Panel: a.Panel,
		Harness: a.Harness, Session: a.Session, Dir: a.Dir, Agent: a.Agent,
	}
}

func sortedKeys[K ~string](a, b map[K]string) []K {
	var out []K
	for k := range a {
		out = append(out, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
