package core

import (
	"maps"
	"slices"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// SeenHere is what a survey saw of this machine, as the state record keeps
// it (decisions.md D151): every project loaded, the hosts that listed and why
// each other one did not, every instance listed, each target open on one,
// each instance attached by hand to a project, and the agents of the projects
// here. attached is the state's attachments: a survey of a surface lists
// none, and one that is still listed is attached. A link's agents are its
// host's, in SeenThere.
func (c *Core) SeenHere(r Report, attached map[revier.ProjectName][]revier.TargetRef) record.Seen {
	s := record.Seen{Hosts: slices.Clone(r.Hosts)}
	if r.before != nil {
		s.Current = r.before.Current
	}
	for _, host := range slices.Sorted(maps.Keys(r.Failed)) {
		if s.Unlisted == nil {
			s.Unlisted = map[string]string{}
		}
		s.Unlisted[host] = r.Failed[host].Error()
	}
	listed := make(map[string]revier.Instance, len(r.Instances))
	for _, inst := range r.Instances {
		listed[key(inst.Ref)] = inst
		s.Instances = append(s.Instances, record.Instance{Host: inst.Ref.Host, Ref: inst.Ref.ID, Title: inst.Title})
	}
	has := map[record.Target]bool{}
	add := func(t record.Target) {
		if !has[t] {
			has[t] = true
			s.Targets = append(s.Targets, t)
		}
	}
	for _, v := range r.Views {
		s.Projects = append(s.Projects, v.Project.Name)
		for _, tv := range v.Targets {
			if tv.Ref.IsZero() || tv.Unknown != "" {
				continue
			}
			t, _ := v.Project.Target(tv.Name)
			add(record.Target{
				Project: v.Project.Name, Target: tv.Name, Host: tv.Ref.Host, Ref: tv.Ref.ID, Attached: tv.Attached,
				Tab: tabTarget(t) && (panelTab(v.Project, t) || listedTab(v.Project, t)),
			})
		}
		for _, ref := range attached[v.Project.Name] {
			if _, ok := listed[key(ref)]; ok {
				add(record.Target{Project: v.Project.Name, Host: ref.Host, Ref: ref.ID, Attached: true})
			}
		}
		if v.Project.Remote == nil {
			s.Agents = append(s.Agents, seenAgents(v, listed)...)
		}
	}
	return s
}

// SeenThere is what a survey saw of each linked host it laid an answer of
// over its projects, one Seen each: the links to the host, why the host gave
// no answer, why it gave none for a link, and the agents of those links, by
// the panel here that shows each. A host not asked is in none: what the
// survey has of its links is the window here that reaches them.
func (c *Core) SeenThere(r Report) []record.Seen {
	var out []record.Seen
	for _, host := range slices.Sorted(maps.Keys(r.asked)) {
		s := record.Seen{Machine: host, Unreachable: r.asked[host]}
		for _, v := range r.Views {
			if v.Project.Remote == nil || v.Project.Remote.Host != host {
				continue
			}
			s.Projects = append(s.Projects, v.Project.Name)
			if s.Unreachable == "" && v.Unreachable != "" {
				if s.Unknown == nil {
					s.Unknown = map[revier.ProjectName]string{}
				}
				s.Unknown[v.Project.Name] = v.Unreachable
			}
			s.Agents = append(s.Agents, seenAgents(v, nil)...)
		}
		out = append(out, s)
	}
	return out
}

// seenAgents is the agents of a view, each with the named agent its panel's
// mark names when listed holds its instance. A local agent's directory is
// the project's when the survey left it out for being the project's own.
func seenAgents(v revier.ProjectView, listed map[string]revier.Instance) []record.Agent {
	var out []record.Agent
	for _, a := range v.Agents {
		seen := record.Agent{
			Project: v.Project.Name, Host: a.Ref.Host, Ref: a.Ref.ID, Panel: a.Panel,
			Harness: a.State.Harness, Session: a.State.Session, Dir: a.State.Dir,
		}
		if inst, ok := listed[key(a.Ref)]; ok {
			if seen.Dir == "" {
				seen.Dir = v.Project.Path
			}
			for _, p := range inst.Panels {
				if p.ID == a.Panel {
					seen.Agent = p.Vars[revier.PanelAgentVar]
				}
			}
		}
		out = append(out, seen)
	}
	return out
}
