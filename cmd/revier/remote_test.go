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
			Name: "far", Match: revier.Match{Title: "^far$"}, Panels: []revier.PanelSpec{
				{Kind: revier.PanelAgent, Command: config.RemotePanel("buildbox", "far", "agent")},
				{Kind: revier.PanelShell, Command: config.RemotePanel("buildbox", "far", "shell")},
			}}},
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

// An agent or a shell asked for a remote project opens here, as a tab of the
// link's workspace that reaches the host itself: by -p, or by the panel a key
// was pressed in. A --dir is a path on this machine and is dropped, not
// checked. The host is asked nothing.
func TestTabsOfARemoteProjectOpenHere(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	rt := hosttest.NewRuntime("tmux")
	pane := rt.Add("far", "", revier.Panel{ID: "%7", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, state: &state.State{}, stateRoot: t.TempDir(), core: c}
	ctx := context.Background()

	if err := a.newAgent(ctx, "far", "", "", "abc-123"); err != nil {
		t.Fatalf("agent new -p far: %v", err)
	}
	if err := a.newShell(ctx, "", "%7", "/srv/only-on-the-host"); err != nil {
		t.Fatalf("shell new --panel: %v", err)
	}
	if len(rt.Tabs) != 2 || rt.Tabs[0].Ref != pane || rt.Tabs[1].Ref != pane {
		t.Fatalf("tabs = %+v, want two in %v", rt.Tabs, pane)
	}
	agent, shell := rt.Tabs[0].Real.Panels[0], rt.Tabs[1].Real.Panels[0]
	if agent.Kind != revier.PanelAgent || !slices.Equal(agent.Command[len(agent.Command)-2:], []string{"--resume", "abc-123"}) {
		t.Errorf("agent tab = %+v, want the agent panel resuming abc-123", agent)
	}
	if shell.Kind != revier.PanelShell || shell.Dir != "" {
		t.Errorf("shell tab = %+v, want the shell panel with no directory here", shell)
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %v, want nothing", remote.Asked)
	}
}

// The kitty key passes the panel's directory as --dir, and kitty gives "/" for
// a panel whose shell sits in a deleted worktree. A --dir outside the project
// of --panel opens the tab where no --dir would; one inside it is kept.
func TestTabAtAPanelIgnoresADirOutsideItsProject(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("home", "", revier.Panel{ID: "3", Kind: revier.PanelTool})
	demo := demoProject(t)
	a := &app{cfg: &config.Config{}, projects: []core.Project{demo}, state: &state.State{}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}
	ctx := context.Background()

	for _, dir := range []string{"", "/", demo.Path} {
		if err := a.newShell(ctx, "", "3", dir); err != nil {
			t.Fatalf("shell new --panel 3 --dir %q: %v", dir, err)
		}
	}
	var dirs []string
	for _, tab := range rt.Tabs {
		dirs = append(dirs, tab.Real.Dir)
	}
	if want := []string{dirs[0], dirs[0], demo.Path}; !slices.Equal(dirs, want) {
		t.Errorf("tab dirs = %q, want %q", dirs, want)
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
