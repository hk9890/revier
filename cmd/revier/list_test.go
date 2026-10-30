package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// listApp is the demo project, open with an agent at work on Dtop, and tm,
// a closed project in task-manager whose repository is taskmgr.
func listApp(t *testing.T) *app {
	t.Helper()
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "", revier.Panel{ID: "7", Title: "claude busy Dtop authentication options"})
	tm := core.PrepareProject(revier.Project{Name: "tm", Path: "/x/task-manager", GitURL: "git@github.com:o/taskmgr.git"})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "claude"}}}
	return withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t), tm}, stateRoot: t.TempDir(), core: c}, &state.State{})
}

// --match lists the projects a word finds, in the form the list takes
// without it: the table's rows, or the views as --json gives them.
func TestListMatchIsTheProjectsAWordFinds(t *testing.T) {
	a := listApp(t)
	ctx := context.Background()

	for _, word := range []string{"task-manager", "TASKMGR", "tm"} {
		out := output(t, a, func() error { return cmdList(ctx, a, []string{"--match", word}) })
		if !strings.Contains(out, "\ntm ") || strings.Contains(out, "demo") {
			t.Errorf("list --match %s = %q, want tm alone", word, out)
		}
	}
	out := output(t, a, func() error { return cmdList(ctx, a, []string{"--json", "--match", "dtop"}) })
	var views []revier.ProjectView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Project.Name != "demo" || len(views[0].Agents) != 1 {
		t.Errorf("list --json --match dtop = %+v, want demo's view with its agent", views)
	}
}

func TestListMatchThatFindsNothingFails(t *testing.T) {
	a := listApp(t)
	err := cmdList(context.Background(), a, []string{"--match", "no-such-word-xyz"})
	if err == nil || err.Error() != `no project matches "no-such-word-xyz"` {
		t.Errorf("err = %v, want no project matches", err)
	}
}

func TestListMatchTakesNoNames(t *testing.T) {
	a := listApp(t)
	err := cmdList(context.Background(), a, []string{"--match", "demo", "demo"})
	if err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("err = %v, want a usage error", err)
	}
}
