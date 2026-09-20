package main

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// `revier attach` records the window the user is looking at and the terminal
// inside it, so the survey has panels to probe for the agent in it
// (decisions.md D95).
func TestAttachRecordsTheTerminalInTheWindow(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	term := rt.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	wm := hosttest.New("gnome")
	window := wm.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	wm.SetFocus(window)
	root := t.TempDir()
	a := &app{
		cfg: &config.Config{}, projects: []core.Project{demoProject(t)},
		state: &state.State{}, stateRoot: root,
		core: &core.Core{Runtime: rt, Window: wm},
	}

	output(t, a, func() error { return cmdAttach(context.Background(), a, []string{"-p", "demo"}) })

	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []revier.TargetRef{window, term}
	if !slices.Equal(got.Attached["demo"], want) {
		t.Errorf("attached = %v, want %v", got.Attached["demo"], want)
	}
}

// A window no terminal pairs with beyond doubt is attached alone: two
// unnamed terminals of one process cannot be told apart, and attaching the
// wrong one is worse than attaching none (decisions.md D63, D67).
func TestAttachRecordsAnAmbiguousWindowAlone(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	for range 2 {
		rt.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	}
	wm := hosttest.New("gnome")
	window := wm.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	wm.SetFocus(window)
	root := t.TempDir()
	a := &app{
		cfg: &config.Config{}, projects: []core.Project{demoProject(t)},
		state: &state.State{}, stateRoot: root,
		core: &core.Core{Runtime: rt, Window: wm},
	}

	output(t, a, func() error { return cmdAttach(context.Background(), a, []string{"-p", "demo"}) })

	got, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Attached["demo"], []revier.TargetRef{window}) {
		t.Errorf("attached = %v, want the window %v alone", got.Attached["demo"], window)
	}
}
