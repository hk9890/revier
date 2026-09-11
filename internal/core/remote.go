package core

import (
	"context"
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
	byHost := map[string][]revier.ProjectName{}
	for _, p := range projects {
		if p.Host != "" {
			byHost[p.Host] = append(byHost[p.Host], p.Name)
		}
	}
	out := make(map[revier.ProjectName]remoteAnswer)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for host, names := range byHost {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers := c.askRemote(ctx, host, names)
			mu.Lock()
			maps.Copy(out, answers)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// askRemote surveys one host's projects. A host that fails answers for all
// of them with the failure; one that answers but leaves a project out
// answers for that project with that.
func (c *Core) askRemote(ctx context.Context, host string, names []revier.ProjectName) map[revier.ProjectName]remoteAnswer {
	var views []revier.ProjectView
	var err error
	if r, ok := c.Remotes[host]; ok {
		views, err = r.Survey(ctx, names)
	} else {
		err = fmt.Errorf("no remote is wired for host %q", host)
	}
	listed := make(map[revier.ProjectName]revier.ProjectView, len(views))
	for _, v := range views {
		listed[v.Project.Name] = v
	}
	out := make(map[revier.ProjectName]remoteAnswer, len(names))
	for _, n := range names {
		switch v, ok := listed[n]; {
		case err != nil:
			out[n] = remoteAnswer{err: err}
		case ok:
			out[n] = remoteAnswer{view: v}
		default:
			out[n] = remoteAnswer{err: fmt.Errorf("%s did not list project %q", host, n)}
		}
	}
	return out
}

// merge lays the host's answer over the local view of a remote project. The
// agents and the checkout are the host's: nothing here can see either.
// Running and Home stay local, because they are about the window that
// reaches the project, which is the one thing about it that is here: a
// remote agent working with no window onto it here reads as an agent in a
// stopped project, and Enter opens the window.
//
// A host that gave no answer leaves the checkout as unknown rather than
// missing: "not on this machine" would be a second thing said about the
// same failure, and possibly false.
func merge(v *revier.ProjectView, a remoteAnswer) {
	if a.err != nil {
		v.Unreachable = a.err.Error()
		v.PathExists = true
		return
	}
	v.PathExists = a.view.PathExists
	v.Agents = a.view.Agents
}
