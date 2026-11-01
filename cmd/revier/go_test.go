package main

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// A press of a command target names the command it ran: it lands on no
// instance to name.
func TestGoNamesTheCommandItRan(t *testing.T) {
	ran := 0
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Run: func(context.Context, []string, string) error {
		ran++
		return nil
	}}
	p := demoProject(t)
	p = core.PrepareProject(revier.Project{Name: p.Name, Path: p.Path, Targets: append(p.Targets,
		revier.Target{Name: "web", Command: &revier.Command{Run: []string{"repo-web"}}})})
	root := t.TempDir()
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{p}, stateRoot: root, core: c}, &state.State{})

	got := output(t, a, func() error { return cmdGo(context.Background(), a, []string{"web", "-p", "demo"}) })
	if got != "demo: ran web\n" || ran != 1 {
		t.Errorf("go printed %q after %d runs, want demo: ran web after one", got, ran)
	}
}
