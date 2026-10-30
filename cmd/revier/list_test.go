package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
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

// stderrOf runs f and returns what it wrote on stderr, and its error.
func stderrOf(t *testing.T, f func() error) (string, error) {
	t.Helper()
	stderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	err = f()
	os.Stderr = stderr
	_ = w.Close()
	printed, _ := io.ReadAll(r)
	return string(printed), err
}

// Names a list does not know cost the names it knows nothing: those are
// listed, each unknown one has its line on stderr with the project it
// probably meant, and the exit fails, so a revier on another machine still
// reports a missing project file.
func TestListOfKnownAndUnknownNamesListsTheKnown(t *testing.T) {
	a := listApp(t)
	a.projects = append(a.projects, core.PrepareProject(revier.Project{Name: "task-manager-ui", Path: "/x/task-manager-ui"}))
	ctx := context.Background()

	for _, flags := range [][]string{{"--json"}, nil} {
		var out bytes.Buffer
		a.out = &out
		args := append(flags, "demo", "taskmgr-ui", "zzzz-no-such-project")
		printed, err := stderrOf(t, func() error { return cmdList(ctx, a, args) })
		if !errors.Is(err, errNoProjectNamed) {
			t.Errorf("list %q: err = %v, want errNoProjectNamed", args, err)
		}
		if status, say := outcome(err); status == 0 || say {
			t.Errorf("list %q: outcome = %d, %v; want a failure main prints nothing for", args, status, say)
		}
		want := "revier: no project named \"taskmgr-ui\"; did you mean task-manager-ui?\n" +
			"revier: no project named \"zzzz-no-such-project\"\n"
		if printed != want {
			t.Errorf("list %q: stderr = %q, want %q", args, printed, want)
		}
		if !strings.Contains(out.String(), "demo") || strings.Contains(out.String(), "task-manager-ui") {
			t.Errorf("list %q: stdout = %q, want demo alone", args, out.String())
		}
	}

	var out bytes.Buffer
	a.out = &out
	_, err := stderrOf(t, func() error { return cmdList(ctx, a, []string{"--json", "zzzz-no-such-project"}) })
	if !errors.Is(err, errNoProjectNamed) || out.Len() != 0 {
		t.Errorf("list of an unknown name alone: err = %v, stdout = %q; want the failure and nothing", err, out.String())
	}
}
