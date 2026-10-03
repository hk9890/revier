package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// dirOf is the directory the view of a project at path carries for its one
// agent, when the agent's probe reports dir.
func dirOf(t *testing.T, path, dir string) string {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle, Dir: dir}}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	projects := core.Prepare([]revier.Project{{Name: "demo", Path: path, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:demo$"}}},
	}}})
	report, err := c.Survey(context.Background(), projects, nil, nil)
	if err != nil || len(report.Views) != 1 || len(report.Views[0].Agents) != 1 {
		t.Fatalf("Survey = %+v, %v; want one project with one agent", report.Views, err)
	}
	return report.Views[0].Agents[0].State.Dir
}

// A view carries an agent's directory only when it is not the project's own:
// the directory is what tells an agent in a worktree from the others, and one
// in the project's own says nothing. The two are the same directory however
// either is spelled, through a symbolic link included.
func TestAViewCarriesAnAgentsDirectoryOnlyWhenItIsNotTheProjects(t *testing.T) {
	real := t.TempDir()
	worktree := filepath.Join(real, ".claude", "worktrees", "fix")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")

	for name, tc := range map[string]struct{ path, dir, want string }{
		"the project's own directory":             {real, real, ""},
		"the same, with a trailing separator":     {real, real + "/", ""},
		"the project reached through a link":      {link, real, ""},
		"the agent reached through a link":        {real, link, ""},
		"a worktree of the project":               {real, worktree, worktree},
		"a worktree, the project through a link":  {link, worktree, worktree},
		"no directory reported":                   {real, "", ""},
		"a directory that is not there":           {real, gone, gone},
		"a project that is not there, as written": {gone, gone, ""},
	} {
		if got := dirOf(t, tc.path, tc.dir); got != tc.want {
			t.Errorf("%s: Dir = %q, want %q", name, got, tc.want)
		}
	}
}
