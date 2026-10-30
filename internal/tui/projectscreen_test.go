package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
)

// A rename onto the built-in's name takes its place: one row of that name,
// the file's. A rename away from it, or a delete of the file, gives the
// place back to the built-in.
func TestAFileTakesTheBuiltinsPlaceAndGivesItBack(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	m = resize(m.WithAssistant(config.Builtin(t.TempDir(), []string{"/bin/revier", "assist", "--exec"})), 140, 60)
	cog := theme.Default().Glyphs.Builtin
	named := func(m tui.Model) (n int, builtin bool) {
		for _, row := range rows(m) {
			if strings.Contains(row, string(config.Assistant)) {
				n++
				builtin = builtin || strings.Contains(row, cog)
			}
		}
		return n, builtin
	}
	if n, builtin := named(m); n != 1 || !builtin {
		t.Fatalf("%d rows of the name, built-in %v; want the built-in's alone:\n%s", n, builtin, strings.Join(rows(m), "\n"))
	}
	rename := func(m tui.Model, to string) tui.Model {
		m, _ = press(m, "alt+e")
		m, _ = press(m, "enter")
		m = typeInto(clearField(m), to)
		m, _ = press(m, "enter")
		m, _ = press(m, "esc")
		return m
	}

	m = rename(m, string(config.Assistant))
	if n, builtin := named(m); n != 1 || builtin {
		t.Fatalf("after the rename onto the name: %d rows of it, built-in %v; want the file's alone:\n%s", n, builtin, strings.Join(rows(m), "\n"))
	}
	m = rename(m, "demo")
	if n, builtin := named(m); n != 1 || !builtin {
		t.Fatalf("after the rename away: %d rows of the name, built-in %v; want the built-in back", n, builtin)
	}

	m = rename(m, string(config.Assistant))
	m, _ = press(m, "alt+d")
	m, _ = press(m, "y")
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), string(config.Assistant)+".toml")); !os.IsNotExist(err) {
		t.Fatalf("the file of the name: %v, want it deleted", err)
	}
	if n, builtin := named(m); n != 1 || !builtin {
		t.Errorf("after the delete: %d rows of the name, built-in %v; want the built-in back", n, builtin)
	}
	// The built-in itself is in no file to delete.
	m, _ = press(m, "alt+d")
	if f := footer(m); !strings.Contains(f, "built in and in no file") {
		t.Errorf("footer = %q, want the delete of the built-in refused", f)
	}
}

// alt+e opens the screen on the highlighted project: its values, and every
// target it has with where each comes from. alt+e and Esc both leave it.
func TestAltEOpensTheProjectScreen(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	for _, leave := range []string{"esc", "alt+e"} {
		m, _ = press(m, "alt+e")
		s := screen(m)
		for _, want := range []string{"Project demo", "demo.toml as it changes", "/tmp/demo", "config.toml · runtime, tabs: agent · home", "config.toml · tab of home, 2 panels", "config.toml, changed here", "add a target"} {
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
// A shared target config.toml refuses reaches no project, and costs the
// screen nothing: the project opens with the targets it has.
func TestTheProjectScreenOpensBesideARefusedSharedTarget(t *testing.T) {
	refused := sharedTargets + "\n[[target]]\nname = \"web\"\nhome = \"yes\"\n  [target.window]\n  launch = [\"firefox\"]\n  match = { class = \"^firefox$\" }\n"
	m, _, _ := projectSurfaceOver(t, refused, demoProject, hosttest.NewRuntime("rt"))
	m, _ = press(m, "alt+e")
	if s := screen(m); !strings.Contains(s, "Project demo") || strings.Contains(s, "incompatible types") {
		t.Errorf("project screen = %q, want demo open without the refused web", s)
	}
}

func TestTheNameInThePaneOpensTheProjectScreen(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	x, y := paneCell(t, m, "demo edit alt+e")
	m = clickAt(m, x, y)
	if !strings.Contains(screen(m), "Project demo") {
		t.Errorf("the click did not open the screen:\n%s", screen(m))
	}
}

// The name button starts after the label, with a space around its
// text: the cell before that space opens nothing, and the space after the
// text opens the screen.
func TestTheNameButtonIsWhereItIsDrawn(t *testing.T) {
	m, _, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))
	const button = "demo edit alt+e"
	x, y := paneCell(t, m, button)
	if before := clickAt(m, x-2, y); strings.Contains(screen(before), "Project demo") {
		t.Errorf("a click left of the name opened the project screen:\n%s", screen(before))
	}
	if last := clickAt(m, x+len(button), y); !strings.Contains(screen(last), "Project demo") {
		t.Errorf("a click on the name's last cell did not open the screen:\n%s", screen(last))
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
	if got := fileText(t, config.File(root)); got != sharedTargets {
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
// home and its tab are the derived ones until the file declares them.
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
