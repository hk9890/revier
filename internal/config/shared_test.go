package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const sharedConfig = `
[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
`

// sharedRoot is a configuration root with the shared targets above and the
// given project files, loaded.
func sharedRoot(t *testing.T, projects map[string]string) []core.Project {
	t.Helper()
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range projects {
		write(t, filepath.Join(root, "projects"), name+".toml", body)
	}
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return loaded
}

func target(t *testing.T, p core.Project, name revier.TargetName) revier.Target {
	t.Helper()
	for _, tg := range p.Targets {
		if tg.Name == name {
			return tg
		}
	}
	t.Fatalf("project %s has no target %q", p.Name, name)
	return revier.Target{}
}

func names(p core.Project) []revier.TargetName {
	var out []revier.TargetName
	for _, tg := range p.Targets {
		out = append(out, tg.Name)
	}
	return out
}

// A project file with no targets has the shared ones, rendered for itself.
func TestAProjectHasTheSharedTargets(t *testing.T) {
	p := sharedRoot(t, map[string]string{"demo": "path = \"/tmp/demo\"\n"})[0]
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "editor"}) {
		t.Fatalf("targets = %v, want home and editor", got)
	}
	home := target(t, p, "home")
	if home.Runtime.Name != "session:demo" || len(home.Runtime.Panels) != 2 {
		t.Errorf("home runtime = %+v, want the shared one rendered for demo", home.Runtime)
	}
}

// A project's target of a shared name overrides only the fields it writes: a
// match title leaves the shared class and launch as they were.
func TestAProjectOverridesOneFieldOfASharedTarget(t *testing.T) {
	p := sharedRoot(t, map[string]string{"demo": `path = "/tmp/demo"
[[target]]
name = "editor"
  [target.window]
  match = { title = "^platform-demo( |$)" }
`})[0]
	editor := target(t, p, "editor")
	if editor.Window.Match.Title != "^platform-demo( |$)" {
		t.Errorf("editor title = %q, want the project's", editor.Window.Match.Title)
	}
	if editor.Window.Match.Class != "^jetbrains-idea" || !slices.Equal(editor.Window.Launch, []string{"idea", "/tmp/demo"}) {
		t.Errorf("editor window = %+v, want the shared class and launch kept", editor.Window)
	}
	if editor.Key != "ctrl-shift-o" {
		t.Errorf("editor key = %q, want the shared key kept", editor.Key)
	}
}

// A list is a value like any other: a project's panels replace the shared
// panels whole.
func TestAProjectListReplacesTheSharedList(t *testing.T) {
	p := sharedRoot(t, map[string]string{"demo": `path = "/tmp/demo"
[[target]]
name = "home"
  [target.runtime]
    [[target.runtime.panels]]
    kind = "shell"
`})[0]
	home := target(t, p, "home")
	if len(home.Runtime.Panels) != 1 || home.Runtime.Panels[0].Kind != revier.PanelShell {
		t.Errorf("home panels = %+v, want the project's one shell", home.Runtime.Panels)
	}
	if home.Runtime.Name != "session:demo" || !home.Home {
		t.Errorf("home = %+v, want the shared name and home kept", home)
	}
}

// A target of a name no shared target has is the project's own, after the
// shared ones.
func TestAProjectAddsItsOwnTarget(t *testing.T) {
	p := sharedRoot(t, map[string]string{"demo": `path = "/tmp/demo"
[[target]]
name = "web"
key = "ctrl-shift-i"
  [target.window]
  launch = ["chrome", "--app=https://example.invalid"]
  match = { class = "^chrome-example" }
`})[0]
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "editor", "web"}) {
		t.Errorf("targets = %v, want the shared ones, then web", got)
	}
}

// One project's override is its own: the shared target the next project gets
// is the one config.toml declares.
func TestAnOverrideDoesNotReachAnotherProject(t *testing.T) {
	projects := sharedRoot(t, map[string]string{
		"a": "path = \"/tmp/a\"\n[[target]]\nname = \"editor\"\n  [target.window]\n  match = { title = \"^other\" }\n",
		"b": "path = \"/tmp/b\"\n",
	})
	b := projects[1]
	if b.Name != "b" {
		t.Fatalf("projects = %s, %s; want a, b", projects[0].Name, b.Name)
	}
	if title := target(t, b, "editor").Window.Match.Title; title != "^b( |$)" {
		t.Errorf("b editor title = %q, want the shared one", title)
	}
}

// A shared target with no remote part is local-only, so a link is left with
// the ssh pane onto the host alone (decisions.md D82).
func TestALinkDoesNotGetALocalOnlySharedTarget(t *testing.T) {
	p := sharedRoot(t, map[string]string{"far": "[remote]\nhost = \"buildbox\"\n"})[0]
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home"}) {
		t.Fatalf("targets = %v, want the derived home alone", got)
	}
	if launch := target(t, p, "home").Runtime.Launch; len(launch) == 0 || launch[0] != "ssh" {
		t.Errorf("home launch = %v, want the ssh pane", launch)
	}
}

// A shared target no project could use is refused in config.toml, once.
func TestLoadRefusesABrokenSharedTarget(t *testing.T) {
	for body, want := range map[string]string{
		"[[target]]\nkey = \"ctrl-o\"\n":                              "has no name",
		"[[target]]\nname = \"a\"\n[[target]]\nname = \"a\"\n":        `"a" declared twice`,
		"[[target]]\nname = \"a\"\n  [target.window]\n  launch = 5\n": `target "a"`,
	} {
		root := t.TempDir()
		write(t, root, "config.toml", body)
		if _, _, err := config.Load(root); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%q) = %v, want %q", body, err, want)
		}
	}
}

// A project the merge leaves incomplete is refused against its own file.
func TestAProjectIncompleteAfterTheMergeIsRefused(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", "[[target]]\nname = \"editor\"\n  [target.window]\n  launch = [\"idea\"]\n")
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := write(t, filepath.Join(root, "projects"), "demo.toml", "path = \"/tmp/demo\"\n")
	_, _, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "empty match") {
		t.Errorf("Load = %v, want demo.toml named with the empty match", err)
	}
}

// A value of the wrong type in the project file itself is reported against
// the file's own line, not as a failure of the merge.
func TestAWrongTypeInTheProjectFileIsReportedAtItsLine(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "demo.toml", "path = \"/tmp/demo\"\n\n[[target]]\nname = \"home\"\nhome = \"yes\"\n")
	_, _, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "line 5") || strings.Contains(err.Error(), "shared targets") {
		t.Errorf("Load = %v, want the error at line 5 of demo.toml", err)
	}
}

// With shared targets, a new project file holds the project alone.
func TestCreateWritesNoTargetsWhenTheyAreShared(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig)
	p, err := config.Create(root, "demo", t.TempDir(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(p.File)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[[target]]") {
		t.Errorf("project file =\n%s\nwant no targets", data)
	}
	if got := names(p); !slices.Equal(got, []revier.TargetName{"home", "editor"}) {
		t.Errorf("targets = %v, want the shared ones", got)
	}
}

// A project turns a shared standalone target into a tab with one line. The
// shared match and name it keeps are not used, and do not refuse the load.
func TestAProjectMakesASharedTargetATab(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig+`
[[target]]
name = "tickets"
  [target.runtime]
  name = "tickets:{{.Name}}"
  launch = ["taskmgr-ui"]
  match = { title = "^tickets:{{.Name}}$" }
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "revier.toml", `
path = "/p"
[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
`)
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := target(t, loaded[0], "tickets").Runtime; got.Inside != "home" || len(got.Launch) != 1 {
		t.Errorf("tickets runtime = %+v, want the shared launch inside home", got)
	}
}
