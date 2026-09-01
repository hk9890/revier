package core_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func TestRenderExpandsEveryTemplatedField(t *testing.T) {
	p := revier.Project{
		Name: "revier",
		Path: "/home/hans/dev/github/revier",
		Vars: map[string]string{"url": "https://example.invalid/pulls"},
		Targets: []revier.Target{{
			Name: "pulls",
			Window: &revier.Realization{
				Name:   "revier-{{.Name}}-pulls",
				Launch: []string{"chrome", "--app={{.Vars.url}}", "--class=revier-{{.Name}}"},
				Match:  revier.Match{Class: "^revier-{{.Name}}$", Title: "{{.Name}}"},
			},
			Runtime: &revier.Realization{
				Name:   "{{.Name}}",
				Launch: []string{"less"},
				Match:  revier.Match{Title: "^{{.Name}}$"},
				Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude", "--cwd", "{{.Path}}"}}},
			},
		}},
	}

	out, err := core.Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	w := out.Targets[0].Window
	if w.Name != "revier-revier-pulls" {
		t.Errorf("name = %q", w.Name)
	}
	if w.Launch[1] != "--app=https://example.invalid/pulls" {
		t.Errorf("launch[1] = %q", w.Launch[1])
	}
	if w.Launch[2] != "--class=revier-revier" {
		t.Errorf("launch[2] = %q", w.Launch[2])
	}
	if w.Match.Class != "^revier-revier$" || w.Match.Title != "revier" {
		t.Errorf("match = %+v", w.Match)
	}
	r := out.Targets[0].Runtime
	if r.Panels[0].Command[2] != "/home/hans/dev/github/revier" {
		t.Errorf("panel command = %v", r.Panels[0].Command)
	}
}

// Render must not mutate its input: the loaded project is reused across
// commands, and an in-place expansion would render an already-rendered value
// on the second pass.
func TestRenderDoesNotMutateInput(t *testing.T) {
	p := revier.Project{
		Name: "x",
		Targets: []revier.Target{{
			Name:   "home",
			Window: &revier.Realization{Name: "{{.Name}}", Launch: []string{"a"}, Match: revier.Match{Class: "^{{.Name}}$"}},
		}},
	}
	if _, err := core.Render(p); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if p.Targets[0].Window.Name != "{{.Name}}" {
		t.Errorf("input was mutated: %q", p.Targets[0].Window.Name)
	}
}

// A missing key must fail loudly. An argv silently losing an argument is far
// harder to diagnose than a refusal.
func TestRenderRejectsAMissingKey(t *testing.T) {
	p := revier.Project{
		Name: "x",
		Targets: []revier.Target{{
			Name:   "home",
			Window: &revier.Realization{Launch: []string{"{{.Vars.absent}}"}, Match: revier.Match{Class: "^x$"}},
		}},
	}
	_, err := core.Render(p)
	if err == nil {
		t.Fatal("want an error for a missing key")
	}
	if !strings.Contains(err.Error(), "home") {
		t.Errorf("error should name the target: %v", err)
	}
}

func TestRenderLeavesPlainStringsAlone(t *testing.T) {
	p := revier.Project{
		Name: "x",
		Targets: []revier.Target{{
			Name:   "home",
			Window: &revier.Realization{Launch: []string{"code", "/plain/path"}, Match: revier.Match{Class: "^code$"}},
		}},
	}
	out, err := core.Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out.Targets[0].Window.Launch[1] != "/plain/path" {
		t.Errorf("launch = %v", out.Targets[0].Window.Launch)
	}
}
