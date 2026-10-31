package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// `revier list a b` is those projects alone, in the order asked, which is
// how a revier on another machine is asked about the projects there.
func TestListNamedProjectsIsThoseAlone(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux")}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, stateRoot: t.TempDir(), core: c}, &state.State{})

	out := output(t, a, func() error { return cmdList(context.Background(), a, []string{"--json", "far"}) })
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
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: c}, &state.State{})

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
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{remoteProject(t)}, stateRoot: t.TempDir(), core: c}, &state.State{})

	out := output(t, a, func() error { return cmdList(context.Background(), a, nil) })
	if !strings.Contains(out, "far@buildbox") || !strings.Contains(out, "unreachable") {
		t.Errorf("table = %q, want the host on the name and the state unreachable", out)
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
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), remoteProject(t)}, stateRoot: t.TempDir(), core: c}, &state.State{})
	ctx := context.Background()

	if err := a.newAgent(ctx, "far", "", "", core.Resume{Session: "abc-123"}); err != nil {
		t.Fatalf("agent new -p far: %v", err)
	}
	if err := a.newShell(ctx, "", "%7", "/srv/only-on-the-host"); err != nil {
		t.Fatalf("shell new --panel: %v", err)
	}
	if len(rt.Tabs) != 2 || rt.Tabs[0].Ref != pane || rt.Tabs[1].Ref != pane {
		t.Fatalf("tabs = %+v, want two in %v", rt.Tabs, pane)
	}
	agent, shell := rt.Tabs[0].Real.Panels[0], rt.Tabs[1].Real.Panels[0]
	if want := append(remote.PanelCommand("far", revier.PanelAgent, ""), "--resume", "abc-123"); agent.Kind != revier.PanelAgent || !slices.Equal(agent.Command, want) {
		t.Errorf("agent tab = %+v, want the agent panel running %q", agent, want)
	}
	if want := remote.PanelCommand("far", revier.PanelShell, ""); shell.Kind != revier.PanelShell || shell.Dir != "" || !slices.Equal(shell.Command, want) {
		t.Errorf("shell tab = %+v, want the shell panel running %q with no directory here", shell, want)
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %v, want nothing", remote.Asked)
	}
}

// A --dir outside the project of --panel opens the tab where no --dir would;
// one inside it is kept.
func TestTabAtAPanelIgnoresADirOutsideItsProject(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("home", "", revier.Panel{ID: "3", Kind: revier.PanelTool})
	demo := demoProject(t)
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demo}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
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

// The kitty key passes no --dir: kitty cannot start the key's sh in a
// directory that was removed. The tab starts in the panel's own directory: a
// worktree of the project is kept, and one that was removed, or a directory
// outside the project, opens the tab where the project's own tab would.
func TestTabAtAPanelStartsInThePanelsDirectory(t *testing.T) {
	demo := demoProject(t)
	worktree := filepath.Join(demo.Path, "wt")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	rt := hosttest.NewRuntime("kitty")
	rt.Add("home", "", revier.Panel{ID: "3", Dir: worktree}, revier.Panel{ID: "4", Dir: filepath.Join(demo.Path, "removed")}, revier.Panel{ID: "5", Dir: t.TempDir()})
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demo}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	ctx := context.Background()

	if err := a.newShell(ctx, "demo", "", ""); err != nil {
		t.Fatalf("shell new -p demo: %v", err)
	}
	for _, panel := range []string{"3", "4", "5"} {
		if err := a.newShell(ctx, "", panel, ""); err != nil {
			t.Fatalf("shell new --panel %s: %v", panel, err)
		}
	}
	var dirs []string
	for _, tab := range rt.Tabs {
		dirs = append(dirs, tab.Real.Dir)
	}
	if want := []string{dirs[0], worktree, dirs[0], dirs[0]}; !slices.Equal(dirs, want) {
		t.Errorf("tab dirs = %q, want %q", dirs, want)
	}
}

// The agent key in a worktree's panel starts its agent in the project, where
// the agent can enter any worktree; the shell key there starts in the
// worktree.
func TestTabAtAPanelStartsAnAgentInTheProject(t *testing.T) {
	demo := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Match: revier.Match{Title: "^home$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
	worktree := filepath.Join(demo.Path, "wt")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	rt := hosttest.NewRuntime("kitty")
	rt.Add("home", "", revier.Panel{ID: "3", Dir: worktree})
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demo}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	ctx := context.Background()

	if err := a.newAgent(ctx, "", "3", "", core.Resume{}); err != nil {
		t.Fatalf("agent new --panel 3: %v", err)
	}
	if err := a.newShell(ctx, "", "3", ""); err != nil {
		t.Fatalf("shell new --panel 3: %v", err)
	}
	var dirs []string
	for _, tab := range rt.Tabs {
		dirs = append(dirs, tab.Real.Dir)
	}
	if want := []string{demo.Path, worktree}; !slices.Equal(dirs, want) {
		t.Errorf("tab dirs = %q, want %q", dirs, want)
	}
}

// [terminal] outside reaches a key pressed in a window that is no project's
// workspace: "none" opens nothing there and is no failure, so the key's
// fallback does not run either; the default opens a plain tab.
func TestTabAtAPanelOutsideAWorkspaceTakesTheConfiguredAction(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "3"})
	none := &config.Config{Terminal: config.Terminal{Outside: core.OutsideNone}}
	a := withState(t, &app{cfg: none, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	ctx := context.Background()

	if err := a.newAgent(ctx, "", "3", "", core.Resume{}); err != nil {
		t.Fatalf("agent new --panel 3, outside none: %v", err)
	}
	if len(rt.Tabs) != 0 {
		t.Fatalf("tabs = %+v, want none", rt.Tabs)
	}
	a.cfg = &config.Config{}
	if err := a.newShell(ctx, "", "3", ""); err != nil {
		t.Fatalf("shell new --panel 3, outside tab: %v", err)
	}
	if len(rt.Tabs) != 1 {
		t.Errorf("tabs = %+v, want the plain tab", rt.Tabs)
	}
}

// With outside "agent", in a kitty window that is no project's workspace, the
// agent key opens a tab of config.toml's first named agent in the window's
// directory, a named agent's key that agent, and the shell key a plain tab.
func TestTabAtAPanelOutsideAWorkspaceRunsTheAgentTheKeyAsksFor(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	dir := t.TempDir()
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "3", Dir: dir})
	cfg := &config.Config{Terminal: config.Terminal{Outside: core.OutsideAgent}, Agents: []map[string]any{
		{"name": "claude", "command": []any{"claude"}},
		{"name": "dtop", "command": []any{"dtop", "--fast"}},
	}}
	a := withState(t, &app{cfg: cfg, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	ctx := context.Background()

	if err := a.newAgent(ctx, "", "3", "", core.Resume{}); err != nil {
		t.Fatalf("agent new --panel 3: %v", err)
	}
	if err := a.newAgent(ctx, "", "3", "", core.Resume{Agent: "dtop"}); err != nil {
		t.Fatalf("agent new dtop --panel 3: %v", err)
	}
	if err := a.newShell(ctx, "", "3", ""); err != nil {
		t.Fatalf("shell new --panel 3: %v", err)
	}
	var got []string
	for _, tab := range rt.Tabs {
		p := tab.Real.Panels[0]
		got = append(got, fmt.Sprintf("%s %q in %s", p.Kind, p.Command, p.Dir))
	}
	want := []string{
		fmt.Sprintf("agent [\"claude\"] in %s", dir),
		fmt.Sprintf("agent [\"dtop\" \"--fast\"] in %s", dir),
		fmt.Sprintf("shell [] in %s", dir),
	}
	if !slices.Equal(got, want) {
		t.Errorf("tabs =\n%q\nwant\n%q", got, want)
	}
}

// Outside a workspace no tab resumes a conversation, so --resume there warns
// that it was not resumed, as a workspace's tab that started empty does,
// whatever outside opens; without --resume nothing is said.
func TestTabAtAPanelOutsideAWorkspaceSaysItDidNotResume(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "3", Dir: t.TempDir()})
	a := withState(t, &app{projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	ctx := context.Background()
	for _, outside := range []core.Outside{core.OutsideTab, core.OutsideNone, core.OutsideAgent} {
		a.cfg = &config.Config{Terminal: config.Terminal{Outside: outside}, Agents: []map[string]any{{"name": "claude", "command": []any{"claude"}}}}
		for _, r := range []core.Resume{{Session: "abc-123"}, {}} {
			printed, err := stderrOf(t, func() error { return a.newAgent(ctx, "", "3", "", r) })
			if err != nil {
				t.Fatalf("outside %s, --resume %q: %v", outside, r.Session, err)
			}
			want := ""
			if r.Session != "" {
				want = "revier: warning: abc-123 was not resumed (no workspace holds panel 3)\n"
			}
			if printed != want {
				t.Errorf("outside %s, --resume %q: stderr = %q, want %q", outside, r.Session, printed, want)
			}
		}
	}
}

// A runtime that cannot attach a terminal says so, naming what would: a
// kitty window has been raised already, and there is nothing to become.
func TestAttachRefusesARuntimeThatCannotAttach(t *testing.T) {
	a := &app{core: &core.Core{Runtime: hosttest.NewRuntime("kitty")}}
	err := a.attach(context.Background(), revier.TargetRef{Host: "kitty", ID: "1"})
	if err == nil || !strings.Contains(err.Error(), "kitty") || !strings.Contains(err.Error(), "tmux") {
		t.Errorf("err = %v, want the runtime named and tmux suggested", err)
	}
	a = &app{core: &core.Core{}}
	if err := a.attach(context.Background(), revier.TargetRef{}); err == nil {
		t.Error("want a refusal with no runtime")
	}
}
