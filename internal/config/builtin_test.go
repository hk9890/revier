package config_test

import (
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The built-in goes through the rules a project file goes through, so a
// fault in how it is built is a refusal the surface shows, and this test
// fails on it first.
func TestTheBuiltinAssistantLoadsWhole(t *testing.T) {
	p := config.Builtin("/state/assist", []string{"/bin/revier", "assist", "--exec"})
	if probs := config.Problems(p); len(probs) != 0 {
		t.Fatalf("the built-in does not load whole: %v", probs)
	}
	if p.File != "" || p.Name != config.Assistant || p.Path != "/state/assist" {
		t.Errorf("built-in = %q in %q from %q, want %q in /state/assist from no file", p.Name, p.Path, p.File, config.Assistant)
	}
	home, ok := p.Home()
	if !ok || home.Runtime == nil || len(home.Runtime.Tabs) != 1 {
		t.Fatalf("home = %+v, want a runtime workspace with one tab", home)
	}
	tab := slices.IndexFunc(p.Targets, func(t revier.Target) bool { return t.Name == home.Runtime.Tabs[0] })
	panels := p.Targets[tab].Runtime.Panels
	want := []string{"/bin/revier", "assist", "--exec"}
	if len(panels) != 1 || !slices.Equal(panels[0].Command, want) {
		t.Errorf("the tab's panels = %+v, want one agent panel running %q", panels, want)
	}
}

// A host's built-in writes that host's configuration, so a link may not
// name it; everything else the host lists stays.
func TestAHostsBuiltinIsNotLinkable(t *testing.T) {
	views := []revier.ProjectView{{Project: revier.Project{Name: "alpha"}}, {Project: revier.Project{Name: config.Assistant}}}
	got := config.Linkable(views)
	if len(got) != 1 || got[0].Project.Name != "alpha" || len(views) != 2 {
		t.Errorf("Linkable = %v from %v, want alpha alone and the list it was given untouched", got, views)
	}
}

// A project file of the built-in's name says what that project is on this
// machine.
func TestAProjectFileOfTheBuiltinsNameWinsOverIt(t *testing.T) {
	builtin := config.Builtin("/state/assist", []string{"/bin/revier", "assist", "--exec"})
	mine := core.PrepareProject(revier.Project{Name: config.Assistant, Path: "/mine"})
	mine.File = "/cfg/projects/" + string(config.Assistant) + ".toml"

	if got := config.WithBuiltin([]core.Project{mine}, builtin); len(got) != 1 || got[0].File != mine.File {
		t.Errorf("WithBuiltin = %v, want the file's project alone", got)
	}
	if got := config.WithBuiltin(nil, builtin); len(got) != 1 || got[0].Path != "/state/assist" {
		t.Errorf("WithBuiltin on no projects = %v, want the built-in", got)
	}
	// So the name is free for a file: the built-in does not refuse it.
	if err := config.CanCreate([]core.Project{builtin}, config.Assistant, "/mine"); err != nil {
		t.Errorf("CanCreate under the built-in's name: %v, want it free for a file", err)
	}
	if err := config.CanCreate([]core.Project{mine}, config.Assistant, "/other"); err == nil {
		t.Error("CanCreate under the name of a file's project: no error, want it taken")
	}
}
