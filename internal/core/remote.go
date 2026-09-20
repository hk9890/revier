package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// A remote project - one whose file names a host - is surveyed by the revier
// on that host (decisions.md D40). Its agents and its checkout are on that
// machine, where the local hosts and probes cannot see them, so this side asks
// the revier there and takes its answer. What stays local is the workspace
// that shows it: the home target's realization here is panels that each run an
// ssh onto the host, matched, raised and closed like any other
// (decisions.md D84).

// RemoteOf returns the remote a project lives on: nil for a project on this
// machine, and an error for a host nothing is wired for.
func (c *Core) RemoteOf(p Project) (revier.Remote, error) {
	if p.Remote == nil {
		return nil, nil
	}
	return c.remote(p.Remote.Host)
}

func (c *Core) remote(host string) (revier.Remote, error) {
	c.remotesMu.Lock()
	defer c.remotesMu.Unlock()
	if r, ok := c.Remotes[host]; ok {
		return r, nil
	}
	if c.NewRemote == nil {
		return nil, fmt.Errorf("no remote is wired for host %q", host)
	}
	if c.Remotes == nil {
		c.Remotes = map[string]revier.Remote{}
	}
	r := c.NewRemote(host)
	c.Remotes[host] = r
	return r, nil
}

// ProjectsOn lists every project the revier on a host has, as it sees them:
// what the link dialog offers to link (decisions.md D45).
func (c *Core) ProjectsOn(ctx context.Context, host string) ([]revier.ProjectView, error) {
	return c.survey(ctx, host, nil)
}

// LinkedAs is the name of the link here to project on host, or "" when
// nothing here links to it.
func LinkedAs(projects []Project, host string, project revier.ProjectName) revier.ProjectName {
	for _, p := range projects {
		if p.Remote != nil && p.Remote.Host == host && p.Remote.Project == project {
			return p.Name
		}
	}
	return ""
}

// remoteAnswer is what a host said about one of its projects, or why it
// said nothing.
type remoteAnswer struct {
	view revier.ProjectView
	err  error
}

// surveyRemotes asks every host named by the projects for its projects, one
// call per host and every host at once, so a survey costs one round trip
// however many hosts and projects there are. It returns an answer for every
// remote project, and nothing for a local one.
func (c *Core) surveyRemotes(ctx context.Context, projects []Project) map[revier.ProjectName]remoteAnswer {
	byHost := map[string][]Project{}
	for _, p := range projects {
		if p.Remote != nil {
			byHost[p.Remote.Host] = append(byHost[p.Remote.Host], p)
		}
	}
	out := make(map[revier.ProjectName]remoteAnswer)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for host, links := range byHost {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers := c.askRemote(ctx, host, links)
			mu.Lock()
			maps.Copy(out, answers)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// askRemote surveys one host's projects, by the names they have there, and
// answers by the names the links have here. A host that fails answers for
// all of them with the failure; one that answers but leaves a project out
// answers for that project with that.
func (c *Core) askRemote(ctx context.Context, host string, links []Project) map[revier.ProjectName]remoteAnswer {
	out := make(map[revier.ProjectName]remoteAnswer, len(links))
	names := make([]revier.ProjectName, len(links))
	for i, p := range links {
		names[i] = p.Remote.Project
	}
	start := time.Now()
	views, err := c.survey(ctx, host, names)
	logging.Poll("remote survey "+host, "remote survey", start, err, "host", host, "projects", len(names))
	if err != nil {
		for _, p := range links {
			out[p.Name] = remoteAnswer{err: err}
		}
		return out
	}
	listed := make(map[revier.ProjectName]revier.ProjectView, len(views))
	for _, v := range views {
		listed[v.Project.Name] = v
	}
	for _, p := range links {
		if v, ok := listed[p.Remote.Project]; ok {
			out[p.Name] = remoteAnswer{view: v}
		} else {
			out[p.Name] = remoteAnswer{err: fmt.Errorf("%s did not list project %q", host, p.Remote.Project)}
		}
	}
	return out
}

func (c *Core) survey(ctx context.Context, host string, names []revier.ProjectName) ([]revier.ProjectView, error) {
	r, err := c.remote(host)
	if err != nil {
		return nil, err
	}
	return r.Survey(ctx, names)
}

// merge lays the host's answer over the local view of a remote project. The
// agents and the checkout are the host's: nothing here can see either, and
// the local view carries neither until the host says. Running and Home stay
// local, because they are about the window that reaches the project, which
// is the one thing about it that is here: a remote agent working with no
// window onto it here reads as an agent in a stopped project, and Enter
// opens the window.
//
// A host that answered but could not reach the project itself - its file
// there names a further host - has said why, and that word is this side's
// too. So is a file there that did not load whole: the host lists the project
// and says what is wrong with it, and this side is the only place the user
// reading the surface would ever see it (decisions.md D85). It is added to
// what this side's own file was refused for, since the two are separate
// mistakes in separate files.
//
// The agents add to the ones read here rather than replacing them
// (decisions.md D99): a terminal attached to the link by hand holds an agent
// of this machine that the host knows nothing about. merge returns the ones
// it added, which are the only ones the host named and so the only ones
// localise has a tag for.
//
// The append copies the agents: two links to one project on one host are
// handed one answer, and the survey renames each link's agents in place.
func merge(v *revier.ProjectView, a remoteAnswer) []revier.AgentView {
	if a.err == nil && a.view.Unreachable != "" {
		a.err = errors.New(a.view.Unreachable)
	}
	if a.err != nil {
		v.Unreachable = a.err.Error()
		return nil
	}
	v.PathExists = a.view.PathExists
	at := len(v.Agents)
	v.Agents = append(v.Agents, a.view.Agents...)
	added := v.Agents[at:]
	if a.view.Invalid != "" {
		if v.Invalid != "" {
			v.Invalid += "\n"
		}
		v.Invalid += a.view.Invalid
	}
	return added
}
