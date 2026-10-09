package core

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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

// HostWait is how long a save waits for the linked hosts to say what the
// agents of the links open here are working on. A host that takes a
// connection and then says nothing would otherwise hold the save for its
// whole bound, and the agents on this machine, asked after it, would be asked
// on a bound already spent (decisions.md D115). A variable so a test can
// shorten it.
var HostWait = 10 * time.Second

// remoteAnswer is what a host said about one of its projects, or why it
// said nothing. link is the host and the project the question named.
type remoteAnswer struct {
	link revier.Link
	view revier.ProjectView
	err  error
}

// RemoteAnswers is what the linked hosts said, by the name each link has
// here. The zero value holds no answer.
type RemoteAnswers struct {
	by map[revier.ProjectName]remoteAnswer
}

// Has reports an answer for the project: a local one has none, and a link
// has none until its host was asked about it as the file names it now.
func (a RemoteAnswers) Has(p revier.Project) bool {
	_, ok := a.of(p)
	return ok
}

// With returns the answers with what the hosts named said replaced by got:
// one host answers by itself, and what the others said last stands.
func (a RemoteAnswers) With(hosts []string, got RemoteAnswers) RemoteAnswers {
	by := make(map[revier.ProjectName]remoteAnswer, len(a.by)+len(got.by))
	for name, answer := range a.by {
		if !slices.Contains(hosts, answer.link.Host) {
			by[name] = answer
		}
	}
	maps.Copy(by, got.by)
	return RemoteAnswers{by: by}
}

// Renamed returns the answers with the one for the link called from under to:
// the file moved, and what its host said of the project stands.
func (a RemoteAnswers) Renamed(from, to revier.ProjectName) RemoteAnswers {
	by := maps.Clone(a.by)
	if answer, ok := by[from]; ok {
		delete(by, from)
		by[to] = answer
	}
	return RemoteAnswers{by: by}
}

// hosts are the hosts that were asked, sorted, for the log line of the
// survey that asked them.
func (a RemoteAnswers) hosts() []string {
	var out []string
	for _, answer := range a.by {
		if !slices.Contains(out, answer.link.Host) {
			out = append(out, answer.link.Host)
		}
	}
	slices.Sort(out)
	return out
}

// failures is why a host gave no answer for a link, each reason once and
// sorted. The reason names its host.
func (a RemoteAnswers) failures() []string {
	var out []string
	for _, answer := range a.by {
		if answer.err != nil && !slices.Contains(out, answer.err.Error()) {
			out = append(out, answer.err.Error())
		}
	}
	slices.Sort(out)
	return out
}

// of is the answer for a link. One asked for before the file was pointed at
// another host or project is no answer for it.
func (a RemoteAnswers) of(p revier.Project) (remoteAnswer, bool) {
	if p.Remote == nil {
		return remoteAnswer{}, false
	}
	got, ok := a.by[p.Name]
	return got, ok && got.link == *p.Remote
}

// AskRemotes asks every host named by the projects for its projects, one
// call per host and every host at once, so it costs one round trip however
// many hosts and projects there are. It returns an answer for every remote
// project, and nothing for a local one.
func (c *Core) AskRemotes(ctx context.Context, projects []Project) RemoteAnswers {
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
	return RemoteAnswers{by: out}
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
			out[p.Name] = remoteAnswer{link: *p.Remote, err: err}
		}
		return out
	}
	listed := make(map[revier.ProjectName]revier.ProjectView, len(views))
	for _, v := range views {
		listed[v.Project.Name] = v
	}
	for _, p := range links {
		if v, ok := listed[p.Remote.Project]; ok {
			out[p.Name] = remoteAnswer{link: *p.Remote, view: v}
		} else {
			out[p.Name] = remoteAnswer{link: *p.Remote, err: fmt.Errorf("%s did not list project %q", host, p.Remote.Project)}
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
// (decisions.md D101): a terminal attached to the link by hand can hold an
// agent this side read for itself. merge returns the ones it added, which are
// the only ones the host named and so the only ones localise has a tag for;
// dropDoubles then settles the ones both sides reported.
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

// dropDoubles removes an agent the host reported that localise placed on a
// panel this side already read: one agent seen twice, which would double the
// row's count and name it twice on a close (decisions.md D101). Whether the
// probe here claims a panel running an ssh is the probe's business, so the
// two are told apart by the panel they landed on and not by assuming they
// cannot meet. The local one stands: it read the panel itself, while the
// host's word about it crossed a machine. at is where the host's agents
// begin in v.Agents.
func dropDoubles(v *revier.ProjectView, at int) {
	if at == 0 {
		// Nothing was read here, so nothing the host named can be a second
		// reading of it. The survey is the hot path and this is the ordinary
		// link, so it keeps the slice it already has.
		return
	}
	held := make(map[string]bool, at)
	for _, a := range v.Agents[:at] {
		held[key(a.Ref)+"\x00"+string(a.Panel)] = true
	}
	kept := v.Agents[:at:at]
	for _, a := range v.Agents[at:] {
		if !held[key(a.Ref)+"\x00"+string(a.Panel)] {
			kept = append(kept, a)
		}
	}
	v.Agents = kept
}

// RemoteEvents asks the revier on every host the projects name for what it
// recorded in the last days, every host at once, and marks each event with
// its host and with the project here that links to its project there
// (decisions.md D112). A host that fails costs its own events and is returned
// by name with why; the others answer.
func (c *Core) RemoteEvents(ctx context.Context, projects []Project, days int) ([]revier.Event, map[string]error) {
	hosts := map[string]bool{}
	links := map[revier.Link]revier.ProjectName{}
	for _, p := range projects {
		if p.Remote == nil {
			continue
		}
		hosts[p.Remote.Host] = true
		if first, ok := links[*p.Remote]; !ok || p.Name < first {
			links[*p.Remote] = p.Name
		}
	}
	var out []revier.Event
	failed := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for host := range hosts {
		wg.Go(func() {
			recorded, err := c.eventsOn(ctx, host, days)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed[host] = err
				return
			}
			for _, e := range recorded {
				e.Host = host
				e.Link = links[revier.Link{Host: host, Project: e.Project}]
				out = append(out, e)
			}
		})
	}
	wg.Wait()
	return out, failed
}

func (c *Core) eventsOn(ctx context.Context, host string, days int) ([]revier.Event, error) {
	start := time.Now()
	r, err := c.remote(host)
	if err != nil {
		return nil, err
	}
	recorded, err := r.Events(ctx, days)
	logging.Op("remote events", start, err, "host", host, "days", days, "events", len(recorded))
	return recorded, err
}
