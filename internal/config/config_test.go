package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

const valid = `
name = "revier"
path = "/home/hans/dev/github/revier"

[vars]
url = "https://example.invalid/pulls"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["kitty"]
  match = { title = "^session:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["code", "{{.Path}}"]
  match = { class = "^code$" }
`

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	p, err := config.LoadProject(write(t, dir, "revier.toml", valid))
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Name != "revier" || p.Path == "" {
		t.Fatalf("project = %+v", p)
	}
	if len(p.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(p.Targets))
	}
	home, ok := p.Home()
	if !ok || home.Name != "home" {
		t.Fatal("home target not found")
	}
	if home.Runtime == nil || len(home.Runtime.Panels) != 1 {
		t.Fatalf("home runtime panels = %+v", home.Runtime)
	}
	if home.Runtime.Panels[0].Kind != revier.PanelAgent {
		t.Errorf("panel kind = %q", home.Runtime.Panels[0].Kind)
	}
	if p.Vars["url"] == "" {
		t.Error("vars not decoded")
	}
}

// A file with no name takes the stem, so a project file need not repeat its
// own identity and a rename cannot leave a stale one behind.
func TestLoadProjectNameDefaultsToFileStem(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(valid, `name = "revier"`, "", 1)
	p, err := config.LoadProject(write(t, dir, "other.toml", body))
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Name != "other" {
		t.Errorf("name = %q, want other", p.Name)
	}
}

// Every rule here names a failure that is otherwise invisible until a key is
// pressed.
func TestValidateRejects(t *testing.T) {
	base := revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "^x$"}}
	cases := []struct {
		name    string
		project revier.Project
		want    string
	}{
		{
			"no home target",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "a", Window: &base}}},
			"no target is marked home",
		},
		{
			"two home targets",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &base}, {Name: "b", Home: true, Window: &base},
			}},
			"2 targets are marked home",
		},
		{
			"empty match",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &revier.Realization{Launch: []string{"x"}}},
			}},
			"empty match",
		},
		{
			"no launch argv",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Window: &revier.Realization{Match: revier.Match{Class: "^x$"}}},
			}},
			"no launch argv",
		},
		{
			"duplicate key",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Key: "ctrl-o", Window: &base},
				{Name: "b", Key: "ctrl-o", Window: &base},
			}},
			`share key "ctrl-o"`,
		},
		{
			"no realization",
			revier.Project{Path: "/p", Targets: []revier.Target{{Name: "a", Home: true}}},
			"declares no realization",
		},
		{
			"no path",
			revier.Project{Targets: []revier.Target{{Name: "a", Home: true, Window: &base}}},
			"no path",
		},
		{
			"bad prefer",
			revier.Project{Path: "/p", Targets: []revier.Target{
				{Name: "a", Home: true, Prefer: "sideways", Window: &base},
			}},
			"prefer must be",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := config.Validate(tc.project)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A pattern that does not compile and a template that does not render are
// refused at load, naming the file, like every other invalid project.
func TestLoadProjectRejectsWhatPrepareRejects(t *testing.T) {
	cases := map[string]string{
		"bad regex":   strings.Replace(valid, `class = "^code$"`, `class = "("`, 1),
		"missing key": strings.Replace(valid, `"{{.Path}}"`, `"{{.Vars.absent}}"`, 1),
		"empty match": strings.Replace(valid, `match = { class = "^code$" }`, `match = { }`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := write(t, dir, "broken.toml", body)
			_, err := config.LoadProject(path)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error should name the file: %v", err)
			}
		})
	}
}

// Projects leave Load prepared: templates rendered, so a host never sees one.
func TestLoadProjectRendersTemplates(t *testing.T) {
	dir := t.TempDir()
	p, err := config.LoadProject(write(t, dir, "revier.toml", valid))
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	home, _ := p.Home()
	if home.Runtime.Match.Title != "^session:revier$" {
		t.Errorf("match title = %q, want it rendered", home.Runtime.Match.Title)
	}
	editor, _ := p.Target("editor")
	if editor.Window.Launch[1] != "/home/hans/dev/github/revier" {
		t.Errorf("launch = %v, want it rendered", editor.Window.Launch)
	}
}

func TestLoadMissingRootIsEmptyNotAnError(t *testing.T) {
	cfg, projects, err := config.Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil || len(projects) != 0 {
		t.Errorf("cfg=%v projects=%d, want an empty config and no projects", cfg, len(projects))
	}
}

func TestLoadReadsConfigAndProjects(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[hosts]
runtime = ["tmux"]
window = ["gnome"]

[[action]]
key = "ctrl-y"
name = "copy path"
run = ["wl-copy", "{{.Path}}"]
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "revier.toml", valid)

	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Hosts.Runtime) != 1 || cfg.Hosts.Runtime[0] != "tmux" {
		t.Errorf("hosts.runtime = %v", cfg.Hosts.Runtime)
	}
	if len(cfg.Actions) != 1 || cfg.Actions[0].Key != "ctrl-y" {
		t.Errorf("actions = %+v", cfg.Actions)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
}

func TestRootHonoursOverride(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", "/tmp/scratch-revier")
	got, err := config.Root()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/scratch-revier" {
		t.Errorf("Root = %q", got)
	}
}
