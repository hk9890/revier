package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// remoteProject is a project on buildbox: its home target here is the ssh
// pane onto the workspace there.
func remoteProject(name string) revier.Project {
	return revier.Project{
		Name: revier.ProjectName(name),
		Path: "~/dev/" + name,
		Host: "buildbox",
		Targets: []revier.Target{{
			Name: "home", Home: true,
			Runtime: &revier.Realization{
				Name:   "session:" + name,
				Launch: []string{"ssh", "-t", "buildbox", "revier", "open", name, "--attach"},
				Match:  revier.Match{Title: "^session:" + name + "$"},
			},
		}},
	}
}

// answer is what the remote revier says about a project: its checkout is
// there, and one agent in it is in the given state.
func answer(name string, status revier.Status) revier.ProjectView {
	return revier.ProjectView{
		Project:    revier.Project{Name: revier.ProjectName(name), Path: "/home/hans/dev/" + name},
		Running:    true,
		PathExists: true,
		Agents:     []revier.AgentView{{Panel: "1", State: revier.AgentState{Harness: "claude", Status: status}}},
	}
}

func TestSurveyTakesARemoteProjectsAgentsFromItsHost(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:demo", "kitty")
	remote := hosttest.NewRemote("buildbox", answer("demo", revier.StatusAttention))
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	if !v.Attention() {
		t.Errorf("agents = %+v, want the host's attention", v.Agents)
	}
	if !v.PathExists {
		t.Error("the checkout is on the host, which said so")
	}
	if !v.Running || v.Home.IsZero() {
		t.Errorf("running = %v, home = %v: the ssh pane here is the project's window", v.Running, v.Home)
	}
	if v.Unreachable != "" {
		t.Errorf("unreachable = %q, want none", v.Unreachable)
	}
}

// Running is about the window here. An agent working on the host with no
// pane onto it here is an agent in a stopped project, and Enter opens one.
func TestSurveyKeepsRunningLocalForARemoteProject(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", answer("demo", revier.StatusRunning))
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty"), Remotes: map[string]revier.Remote{"buildbox": remote}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	if v.Running {
		t.Error("no pane here is on the project, so it is not running here")
	}
	if len(v.Agents) != 1 || v.Agents[0].State.Status != revier.StatusRunning {
		t.Errorf("agents = %+v, want the host's working agent", v.Agents)
	}
}

func TestSurveyMarksTheProjectsOfAHostThatDidNotAnswer(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.Err = errors.New("buildbox: connection refused")
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	projects := []core.Project{prepared(t, remoteProject("demo")), prepared(t, project())}

	report, err := c.Survey(context.Background(), projects, nil, nil)
	if err != nil {
		t.Fatalf("Survey: a host that is down must not fail the survey: %v", err)
	}
	v := report.Views[0]
	if !strings.Contains(v.Unreachable, "connection refused") {
		t.Errorf("unreachable = %q, want the failure", v.Unreachable)
	}
	if len(v.Agents) != 0 {
		t.Errorf("agents = %+v, want none: nothing is known", v.Agents)
	}
	if v.PathExists {
		t.Error("nothing said the checkout is there")
	}
	if local := report.Views[1]; local.Unreachable != "" {
		t.Errorf("the local project is unreachable %q; it has no host", local.Unreachable)
	}
}

// A host that answered but could not reach the project itself has said why,
// and that is this side's word too, not a healthy project with no agent.
func TestSurveyTakesAHostsOwnUnreachableWord(t *testing.T) {
	said := revier.ProjectView{Project: revier.Project{Name: "demo"}, Unreachable: "farbox: connection refused"}
	remote := hosttest.NewRemote("buildbox", said)
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty"), Remotes: map[string]revier.Remote{"buildbox": remote}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if u := report.Views[0].Unreachable; !strings.Contains(u, "farbox") {
		t.Errorf("unreachable = %q, want the host's own failure", u)
	}
}

// The pane here that reaches a remote project is not the agent in it: what
// the local probes make of it is not the project's agent, whatever the host
// then says.
func TestSurveyDoesNotProbeTheLocalPaneOfARemoteProject(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:demo", "kitty")
	remote := hosttest.NewRemote("buildbox")
	remote.Err = errors.New("buildbox: connection refused")
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	if !v.Running || len(v.Agents) != 0 {
		t.Errorf("running = %v, agents = %+v: want the pane counted as the window and nothing as an agent", v.Running, v.Agents)
	}
}

func TestSurveyAsksAHostOnceForAllItsProjects(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", answer("one", revier.StatusIdle), answer("two", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	projects := []core.Project{prepared(t, remoteProject("one")), prepared(t, project()), prepared(t, remoteProject("two"))}

	if _, err := c.Survey(context.Background(), projects, nil, nil); err != nil {
		t.Fatalf("Survey: %v", err)
	}
	want := []revier.ProjectName{"one", "two"}
	if len(remote.Asked) != 1 || !slices.Equal(remote.Asked[0], want) {
		t.Errorf("asked %v, want one call for %v", remote.Asked, want)
	}
}

func TestSurveyReportsAProjectItsHostDidNotList(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", answer("other", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty"), Remotes: map[string]revier.Remote{"buildbox": remote}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if u := report.Views[0].Unreachable; !strings.Contains(u, "demo") || !strings.Contains(u, "buildbox") {
		t.Errorf("unreachable = %q, want the host and the project it left out", u)
	}
}

func TestSurveyReportsAHostNothingIsWiredFor(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("kitty")}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, remoteProject("demo"))}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if u := report.Views[0].Unreachable; !strings.Contains(u, "buildbox") {
		t.Errorf("unreachable = %q, want the host named", u)
	}
}

// A launch started in the project's path would fail here, where the path is
// not: a remote project's launches start where revier did.
func TestRemoteProjectLaunchesStartWhereRevierRuns(t *testing.T) {
	p := prepared(t, remoteProject("demo"))
	if dir := p.Targets[0].Runtime.Dir; dir != "" {
		t.Errorf("dir = %q, want none", dir)
	}
	local := prepared(t, project())
	if dir := local.Targets[0].Runtime.Dir; dir != local.Path {
		t.Errorf("a local project's launch starts in its path, got %q", dir)
	}
}
