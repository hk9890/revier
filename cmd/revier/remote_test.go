package main

import (
	"context"
	"encoding/json"
	"errors"
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

// With outside "agent", in a kitty window that is no project's workspace, a
// named agent's key opens a tab of that agent of config.toml in the window's
// directory.
func TestTabAtAPanelOutsideAWorkspaceRunsTheAgentTheKeyAsksFor(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	dir := t.TempDir()
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "3", Dir: dir})
	cfg := &config.Config{Terminal: config.Terminal{Outside: core.OutsideAgent}, Agents: []map[string]any{
		{"name": "claude", "command": []any{"claude"}},
		{"name": "dtop", "command": []any{"dtop", "--fast"}},
	}}
	a := withState(t, &app{cfg: cfg, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})

	if err := a.newAgent(context.Background(), "", "3", "", core.Resume{Agent: "dtop"}); err != nil {
		t.Fatalf("agent new dtop --panel 3: %v", err)
	}
	if len(rt.Tabs) != 1 {
		t.Fatalf("tabs = %+v, want one", rt.Tabs)
	}
	if p := rt.Tabs[0].Real.Panels[0]; p.Kind != revier.PanelAgent || !slices.Equal(p.Command, []string{"dtop", "--fast"}) || p.Dir != dir {
		t.Errorf("panel = %+v, want the dtop agent in %s", p, dir)
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
