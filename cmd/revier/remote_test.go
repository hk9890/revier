package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
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
	p, err := core.PrepareProject(revier.Project{Name: "far", Path: "~/dev/far", Remote: &revier.Link{Host: "buildbox", Project: "far"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "far", Launch: []string{"ssh", "-t", "buildbox", "revier", "open", "far", "--attach"}, Match: revier.Match{Title: "^far$"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stdout runs f with os.Stdout captured. The pipe is drained while f runs, so
// output larger than the pipe buffer cannot block the command.
func stdout(t *testing.T, f func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	saved := os.Stdout
	os.Stdout = w
	runErr := f()
	os.Stdout = saved
	_ = w.Close()
	out := <-done
	if runErr != nil {
		t.Fatalf("%v\n%s", runErr, out)
	}
	return out
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

	r, there, err := a.remoteFor("far:home")
	if err != nil || r == nil || r.Name() != "buildbox" || there != "far:home" {
		t.Errorf("remoteFor(far:home) = %v, %q, %v; want buildbox and the address as the host knows it", r, there, err)
	}
	if r, _, err := a.remoteFor("demo"); err != nil || r != nil {
		t.Errorf("remoteFor(demo) = %v, err %v; want a local project", r, err)
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

// An agent or a shell asked for a remote project opens on the host, whose
// workspace holds it: by -p, with a target only the host has; or by the panel
// of the ssh pane a key was pressed in, where the host picks its own target
// and a --dir, a path on this machine, is neither sent nor checked. Nothing
// opens here.
func TestTabsOfARemoteProjectOpenOnTheHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	rt := hosttest.NewRuntime("tmux")
	pane := rt.Add("far", "", revier.Panel{ID: "%7", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}
	ctx := context.Background()

	if err := a.newAgent(ctx, "far:work", "", "", "abc-123"); err != nil {
		t.Fatalf("agent new -p far:work: %v", err)
	}
	if err := a.newAgent(ctx, "", "%7", t.TempDir(), ""); err != nil {
		t.Fatalf("agent new --panel: %v", err)
	}
	// A --dir that is no directory here is not the host's concern.
	if err := a.newShell(ctx, "", "%7", "/srv/only-on-the-host"); err != nil {
		t.Fatalf("shell new --panel: %v", err)
	}
	want := []hosttest.RemoteTab{
		{Kind: revier.PanelAgent, Address: "far:work", Resume: "abc-123"},
		{Kind: revier.PanelAgent, Address: "far"},
		{Kind: revier.PanelShell, Address: "far"},
	}
	if !slices.Equal(remote.Tabs, want) {
		t.Errorf("remote tabs = %+v, want %+v", remote.Tabs, want)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("local tabs = %+v in %+v, want none", rt.Tabs, pane)
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
