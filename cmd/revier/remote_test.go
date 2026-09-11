package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// remoteProject is a project on buildbox, its home target here the ssh pane
// onto the workspace there.
func remoteProject(t *testing.T) core.Project {
	t.Helper()
	p, err := core.PrepareProject(revier.Project{Name: "far", Path: "~/dev/far", Host: "buildbox", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "far", Launch: []string{"ssh", "-t", "buildbox", "revier", "open", "far", "--attach"}, Match: revier.Match{Title: "^far$"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stdout runs f with os.Stdout captured.
func stdout(t *testing.T, f func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	runErr := f()
	os.Stdout = saved
	_ = w.Close()
	if runErr != nil {
		t.Fatalf("command: %v", runErr)
	}
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// `revier list a b` is those projects alone, in the order asked, which is
// how a revier on another machine is asked about the projects there.
func TestListNamedProjectsIsThoseAlone(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux")}
	a := &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}

	out := stdout(t, func() error { return cmdList(context.Background(), a, []string{"--json", "far"}) })
	var views []revier.ProjectView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Project.Name != "far" {
		t.Errorf("views = %+v, want far alone", views)
	}
}

func TestListRefusesANameItDoesNotKnow(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux")}
	a := &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}

	err := cmdList(context.Background(), a, []string{"--json", "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("err = %v, want the unknown name refused", err)
	}
}

// The table says which host a project is on and when the host did not
// answer, so a glance at `revier list` tells the two apart from a stopped
// local project.
func TestListTableShowsTheHostAndAnUnreachableOne(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.Err = errors.New("buildbox: connection refused")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}

	out := stdout(t, func() error { return cmdList(context.Background(), a, nil) })
	if !strings.Contains(out, "far@buildbox") || !strings.Contains(out, "unreachable") {
		t.Errorf("table = %q, want the host on the name and the state unreachable", out)
	}
}

// An agent of a remote project is driven by the revier on its host: the
// address goes there as typed.
func TestRemoteForFindsTheHostOfARemoteProjectsAgent(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}

	r, isRemote, err := a.remoteFor("far:home")
	if err != nil || !isRemote || r.Name() != "buildbox" {
		t.Errorf("remoteFor(far:home) = %v, %v, %v; want buildbox", r, isRemote, err)
	}
	if _, isRemote, err := a.remoteFor("demo"); err != nil || isRemote {
		t.Errorf("remoteFor(demo) = remote %v, err %v; want a local project", isRemote, err)
	}
	if _, _, err := a.remoteFor("nope:home"); err == nil {
		t.Error("remoteFor(nope:home): want the unknown project refused")
	}
}

// An action on a remote project runs on the host, through the remote's
// command, and needs no action of that name in the configuration here.
func TestRunOnARemoteProjectRunsOnTheHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"true"}
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}

	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "far"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(remote.Runs) != 1 || remote.Runs[0] != (hosttest.Run{Project: "far", Action: "sync"}) {
		t.Errorf("runs = %+v, want sync on far", remote.Runs)
	}
}

// A runtime that cannot attach a terminal says so, naming what would: a
// kitty window has been raised already, and there is nothing to become.
func TestAttachRefusesARuntimeThatCannotAttach(t *testing.T) {
	a := &app{core: &core.Core{Runtime: hosttest.NewRuntime("kitty")}}
	err := a.attach(revier.TargetRef{Host: "kitty", ID: "1"})
	if err == nil || !strings.Contains(err.Error(), "kitty") || !strings.Contains(err.Error(), "tmux") {
		t.Errorf("err = %v, want the runtime named and tmux suggested", err)
	}
	a = &app{core: &core.Core{}}
	if err := a.attach(revier.TargetRef{}); err == nil {
		t.Error("want a refusal with no runtime")
	}
}
