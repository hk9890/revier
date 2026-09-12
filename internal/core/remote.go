package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// A remote project - one whose file names a host - is surveyed by the revier
// on that host (decisions.md D40). Its runtime instances, its agents and its
// checkout are on that machine, where the local hosts and probes cannot see
// them, so this side asks the revier there and takes its answer. What stays
// local is the window that reaches it: the home target's realization here is
// an ssh pane onto the remote workspace, matched and raised like any other.

// RemoteOf returns the remote a project lives on: nil for a project on this
// machine, and an error for a host nothing is wired for.
func (c *Core) RemoteOf(p Project) (revier.Remote, error) {
	if p.Remote == nil {
		return nil, nil
	}
	return c.remote(p.Remote.Host)
}

func (c *Core) remote(host string) (revier.Remote, error) {
	r, ok := c.Remotes[host]
	if !ok {
		return nil, fmt.Errorf("no remote is wired for host %q", host)
	}
	return r, nil
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
	views, err := c.survey(ctx, host, names)
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
// too.
func merge(v *revier.ProjectView, a remoteAnswer) {
	if a.err == nil && a.view.Unreachable != "" {
		a.err = errors.New(a.view.Unreachable)
	}
	if a.err != nil {
		v.Unreachable = a.err.Error()
		return
	}
	v.PathExists = a.view.PathExists
	v.Agents = a.view.Agents
}
