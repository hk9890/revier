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
		Vars: map[string]string{"url": "https://example.invalid/pulls", "x": "right"},
		Targets: []revier.Target{{
			Name: "pulls",
			Window: &revier.Realization{
				Name:   "revier-{{.Name}}-pulls",
				Launch: []string{"chrome", "--app={{.Vars.url}}", "--class=revier-{{.Name}}"},
				Match:  revier.Match{Class: "^revier-{{.Name}}$", Title: "{{.Name}}"},
				Place:  "{{.Vars.x}} top 75% 100%",
			},
			Runtime: &revier.Realization{
				Name:   "{{.Name}}",
				Launch: []string{"less"},
				Match:  revier.Match{Title: "^{{.Name}}$"},
				Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Title: "agent:{{.Name}}", Command: []string{"claude", "--cwd", "{{.Path}}"}}},
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
	if w.Place != "right top 75% 100%" {
		t.Errorf("place = %q", w.Place)
	}
	r := out.Targets[0].Runtime
	if r.Panels[0].Command[2] != "/home/hans/dev/github/revier" {
		t.Errorf("panel command = %v", r.Panels[0].Command)
	}
	if r.Panels[0].Title != "agent:revier" {
		t.Errorf("panel title = %q", r.Panels[0].Title)
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

// A realization starts in the project directory unless it says otherwise, so
// a workspace opens where the project lives without every file repeating it.
func TestRenderFillsDirFromTheProjectPath(t *testing.T) {
	p := revier.Project{
		Name: "x", Path: "/home/user/dev/x",
		Targets: []revier.Target{
			{Name: "home", Runtime: &revier.Realization{Launch: []string{"sh"}, Match: revier.Match{Title: "^x$"}}},
			{Name: "notes", Runtime: &revier.Realization{Dir: "{{.Path}}/docs", Launch: []string{"sh"}, Match: revier.Match{Title: "^n$"}}},
		},
	}
	out, err := core.Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := out.Targets[0].Runtime.Dir; got != "/home/user/dev/x" {
		t.Errorf("default dir = %q, want the project path", got)
	}
	if got := out.Targets[1].Runtime.Dir; got != "/home/user/dev/x/docs" {
		t.Errorf("explicit dir = %q, want it rendered", got)
	}
}

// An action's argv renders by the same rules as a launch: one template
// language, and a missing key is refused rather than dropped.
func TestRenderArgv(t *testing.T) {
	p := revier.Project{Name: "revier", Path: "/p", Vars: map[string]string{"url": "https://example.invalid"}}
	got, err := core.RenderArgv(p, []string{"wl-copy", "{{.Path}}", "{{.Name}}", "{{.Vars.url}}", "plain"})
	if err != nil {
		t.Fatalf("RenderArgv: %v", err)
	}
	want := []string{"wl-copy", "/p", "revier", "https://example.invalid", "plain"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := core.RenderArgv(p, []string{"{{.Vars.absent}}"}); err == nil {
		t.Error("a missing key must be an error, not an empty argument")
	}
}

// A panel starts where its realization starts. The fallback is the core's, so
// no runtime decides it and none can forget it.
func TestRenderFillsEveryPanelDirFromItsRealization(t *testing.T) {
	p := revier.Project{
		Name: "x", Path: "/home/user/dev/x",
		Targets: []revier.Target{{Name: "home", Runtime: &revier.Realization{
			Match:  revier.Match{Title: "^x$"},
			Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}, {Kind: revier.PanelShell}},
		}}},
	}
	out, err := core.Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for i, panel := range out.Targets[0].Runtime.Panels {
		if panel.Dir != "/home/user/dev/x" {
			t.Errorf("panel %d starts in %q, want the realization's dir", i, panel.Dir)
		}
	}
}

// An argument that renders to nothing is refused: a link written before its
// host reported a path would otherwise launch an editor on the empty string,
// and nothing would say why (decisions.md D83).
func TestRenderRejectsAnArgumentThatRendersToNothing(t *testing.T) {
	link := &revier.Link{Host: "buildbox", Project: "far"}
	for name, tc := range map[string]struct {
		launch []string
		vars   map[string]string
		want   string
	}{
		"a path the link has not recorded":       {launch: []string{"code", "{{.Path}}"}, want: "{{.Path}}"},
		"a repository the link has not recorded": {launch: []string{"chrome", "{{.GitURL}}"}, want: "{{.GitURL}}"},
		"a var written blank":                    {launch: []string{"git", "switch", "{{.Vars.branch}}"}, vars: map[string]string{"branch": ""}, want: "{{.Vars.branch}}"},
	} {
		p := revier.Project{
			Name:   "far",
			Remote: link,
			Vars:   tc.vars,
			Targets: []revier.Target{{
				Name:   "editor",
				Window: &revier.Realization{Launch: tc.launch, Match: revier.Match{Class: "^Code$"}},
			}},
		}
		_, err := core.Render(p)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "editor") {
			t.Errorf("%s: err = %v, want one naming the target and %q", name, err, tc.want)
		}
	}

	// A guard is how a template says the argument is optional, and there is
	// no such thing in an argv: the position stays either way.
	guarded := revier.Project{
		Name: "far", Remote: link,
		Targets: []revier.Target{{
			Name:   "editor",
			Window: &revier.Realization{Launch: []string{"code", "{{if .Path}}{{.Path}}{{end}}"}, Match: revier.Match{Class: "^Code$"}},
		}},
	}
	if _, err := core.Render(guarded); err == nil {
		t.Error("a guarded argument still renders to nothing, and is still refused")
	}

	p := revier.Project{
		Name: "far", Remote: link, Path: "/srv/far",
		Targets: []revier.Target{{
			Name:   "editor",
			Window: &revier.Realization{Launch: []string{"code", "{{.Path}}"}, Match: revier.Match{Class: "^Code$"}},
		}},
	}
	out, err := core.Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := out.Targets[0].Window.Launch[1]; got != "/srv/far" {
		t.Errorf("launch = %q, want the recorded path", got)
	}
}

// A field no argument renders is not required: a link with no path still
// opens its pane onto the host.
func TestRenderLeavesAnEmptyFieldNoArgumentRenders(t *testing.T) {
	p := revier.Project{
		Name:   "far",
		Remote: &revier.Link{Host: "buildbox", Project: "far"},
		Targets: []revier.Target{{
			Name:    "home",
			Home:    true,
			Runtime: &revier.Realization{Launch: []string{"ssh", "{{.Name}}"}, Match: revier.Match{Title: "^session:far$"}},
		}},
	}
	if _, err := core.Render(p); err != nil {
		t.Errorf("Render: %v", err)
	}
}
