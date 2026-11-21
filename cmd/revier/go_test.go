package main

import (
	"context"
	"strings"
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
	c := &core.Core{Runtime: hosttest.NewRuntime("hc"), Run: func(context.Context, []string, string) error {
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

// A press of a tab names the tab it reached, inside the instance it is in;
// a press of the home names the home alone.
func TestGoNamesTheTabItReached(t *testing.T) {
	rt := hosttest.NewRuntime("hc")
	rt.Add("home", "", revier.Panel{ID: "%1", Kind: revier.PanelTool})
	p := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Match: revier.Match{Title: "^home$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}}}},
	}})
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{p}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt, View: rt}}, &state.State{})

	for _, press := range []struct{ target, want string }{{"home", "demo: home ("}, {"agent", "demo: agent in home ("}} {
		got := output(t, a, func() error { return cmdGo(t.Context(), a, []string{press.target, "-p", "demo"}) })
		if !strings.HasPrefix(got, press.want) || (press.target == "home" && strings.Contains(got, " in ")) {
			t.Errorf("go %s printed %q, want it to start %q", press.target, got, press.want)
		}
	}
}
