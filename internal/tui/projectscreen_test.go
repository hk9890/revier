package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
)

const demoProject = `# demo
path = "/tmp/demo"

[[target]]
name = "editor"
key = "ctrl-o"
`

// projectSurface is the surface over a configuration root with the shared
// targets of sharedTargets and one project, demo, written as body; the
// runtime is rt. It returns the project file and the state root too.
func projectSurface(t *testing.T, body string, rt *hosttest.FakeRuntime) (tui.Model, string, string) {
	t.Helper()
	root := configRoot(t, sharedTargets)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "demo.toml")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := stateWith(t, nil)
	m := tui.New(&core.Core{Runtime: rt}, projects, stateRoot, cfg, time.Second, theme.Default(), "")
	next, _ := m.Update(m.Survey()())
	return resize(next.(tui.Model), 140, 60), file, stateRoot
}

func fileText(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// alt+e opens the screen on the highlighted project: its values, and every
// target it has with where each comes from. alt+e and Esc both leave it.
func TestAltEOpensTheProjectScreen(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	for _, leave := range []string{"esc", "alt+e"} {
		m, _ = press(m, "alt+e")
		s := screen(m)
		for _, want := range []string{"Project demo", "demo.toml as it changes", "/tmp/demo", "config.toml · runtime, 2 panels", "config.toml, changed here", "add a target"} {
			if !strings.Contains(s, want) {
				t.Errorf("project screen does not say %q:\n%s", want, s)
			}
		}
		m, _ = press(m, leave)
		if strings.Contains(screen(m), "Project demo") {
			t.Errorf("%s did not leave the screen:\n%s", leave, screen(m))
		}
	}
}

// The name over the pane is a button: it carries its key, and a click opens
// the screen.
func TestTheNameInThePaneOpensTheProjectScreen(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	x, y := paneCell(t, m, "demo edit alt+e")
	m = clickAt(m, x, y)
	if !strings.Contains(screen(m), "Project demo") {
		t.Errorf("the click did not open the screen:\n%s", screen(m))
	}
}

func TestTheProjectScreenWritesAValue(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 2) // git url
	m, _ = press(m, "enter")
	m = typeInto(m, "git@github.com:me/demo.git")
	m, _ = press(m, "enter")

	want := strings.Replace(demoProject, "path = \"/tmp/demo\"\n", "path = \"/tmp/demo\"\ngit_url = \"git@github.com:me/demo.git\"\n", 1)
	if got := fileText(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "git@github.com:me/demo.git") {
		t.Errorf("screen does not show the new value:\n%s", screen(m))
	}
}

// A new name moves the file, and what state holds under the old name, and the
// surface lists the project under the new one.
func TestTheProjectScreenRenamesTheProject(t *testing.T) {
	m, file, stateRoot := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	if _, err := state.Update(stateRoot, func(s *state.State) bool {
		s.Current = "demo"
		return true
	}); err != nil {
		t.Fatal(err)
	}
	m, _ = press(m, "alt+e")
	m, _ = press(m, "enter")
	m = typeInto(clearField(m), "trial")
	m, _ = press(m, "enter")

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("old file: %v, want it moved", err)
	}
	if got := fileText(t, filepath.Join(filepath.Dir(file), "trial.toml")); got != demoProject {
		t.Errorf("new file =\n%s\nwant the old text", got)
	}
	if st, _ := state.Load(stateRoot); st.Current != "trial" {
		t.Errorf("state current = %q, want trial", st.Current)
	}
	m, _ = press(m, "esc")
	if row := selectedRow(t, m); !strings.Contains(row, "trial") {
		t.Errorf("selected row = %q, want the renamed project", row)
	}
}

// A running project keeps its name: its session is found by a title the name
// is part of.
func TestTheProjectScreenRefusesToRenameARunningProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "sh")
	m, file, _ := projectSurface(t, demoProject, rt)
	m, _ = press(m, "alt+e")
	m, _ = press(m, "enter")
	m = typeInto(clearField(m), "trial")
	m, _ = press(m, "enter")

	if f := footer(m); !strings.Contains(f, "running") {
		t.Errorf("footer = %q, want the refusal", f)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file: %v, want it where it was", err)
	}
}

// A shared target changed here is written to the project file as what
// differs, and config.toml stays as it was.
func TestTheProjectScreenOverridesASharedTarget(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	root := filepath.Dir(filepath.Dir(file))
	m, _ = press(m, "alt+e")
	m = downs(m, 3) // home
	m, _ = press(m, "enter")
	if !strings.Contains(screen(m), "from config.toml") {
		t.Errorf("form does not say where its values come from:\n%s", screen(m))
	}
	m = downs(m, 1) // key
	m = typeInto(clearField(m), "ctrl-h")
	m, _ = press(m, "enter")

	want := demoProject + "\n[[target]]\nname = \"home\"\nkey = \"ctrl-h\"\n"
	if got := fileText(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if got := configText(t, root); got != sharedTargets {
		t.Errorf("config.toml changed:\n%s", got)
	}
	if !strings.Contains(screen(m), "ctrl+h") {
		t.Errorf("screen does not show the new key:\n%s", screen(m))
	}
}

// Deleting an override gives the project config.toml's target back; a
// target the file does not declare has nothing to delete.
func TestTheProjectScreenDropsAnOverride(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	m = downs(m, 3) // home, config.toml's
	m, _ = press(m, "alt+d")
	if f := footer(m); !strings.Contains(f, "nothing to delete") {
		t.Errorf("footer = %q, want the refusal", f)
	}
	m = downs(m, 1) // editor, overridden
	m, _ = press(m, "alt+d")
	if f := footer(m); !strings.Contains(f, `drop this project's changes to target "editor"`) {
		t.Fatalf("footer = %q, want the question", f)
	}
	m, _ = press(m, "y")
	if got := fileText(t, file); strings.Contains(got, "editor") {
		t.Errorf("file =\n%s\nwant the editor entry gone", got)
	}
	if !strings.Contains(screen(m), "ctrl+shift+o") {
		t.Errorf("screen does not show the shared key back:\n%s", screen(m))
	}
}

// A link shows its host and its name there, and offers only its name; its
// home is the derived pane until the file declares one.
func TestTheProjectScreenShowsALink(t *testing.T) {
	m, _, _ := projectSurface(t, "[remote]\nhost = \"buildbox\"\nproject = \"far\"\n", hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	s := screen(m)
	for _, want := range []string{"buildbox", "far", "the link's own"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen does not say %q:\n%s", want, s)
		}
	}
	for _, unwanted := range []string{"git url", "config.toml ·"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("screen says %q, which a link does not have:\n%s", unwanted, s)
		}
	}
}
