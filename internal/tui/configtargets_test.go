package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
)

const sharedTargets = `[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
    # the agent
    [[target.runtime.panels]]
    kind = "agent"
    title = "Claude Code"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea" }
`

// targetsSurface is the surface over a configuration root whose config.toml
// holds the shared targets above and one project with none of its own, and
// that root.
func targetsSurface(t *testing.T) (tui.Model, string) {
	t.Helper()
	root := configRoot(t, sharedTargets)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "demo.toml"), []byte("path = \"/tmp/demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, c, _ := world(t, 1)
	m := tui.New(c, projects, stateWith(t, nil), cfg, time.Second, theme.Default(), "")
	next, _ := m.Update(m.Survey()())
	return resize(next.(tui.Model), 140, 60), root
}

// onTargetRow opens the config screen with the cursor on the i-th shared
// target, or on the add row past the last.
func onTargetRow(m tui.Model, i int) tui.Model {
	m, _ = press(m, "alt+c")
	for range 4 + i {
		m, _ = press(m, "down")
	}
	return m
}

func downs(m tui.Model, n int) tui.Model {
	for range n {
		m, _ = press(m, "down")
	}
	return m
}

// The screen lists every shared target with its key and where it opens.
func TestTheConfigScreenListsTheSharedTargets(t *testing.T) {
	m, _ := targetsSurface(t)
	m, _ = press(m, "alt+c")
	for _, want := range []string{"ctrl+shift+u", "runtime, 2 panels · home", "ctrl+shift+o", "window: idea {{.Path}}", "add a target"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("config screen does not say %q:\n%s", want, screen(m))
		}
	}
}

// A field changed in the form is written in place, its comment kept, and the
// screen shows it.
func TestTheConfigScreenChangesASharedTarget(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 1), "enter")
	m = downs(m, 10) // name, key, home, the runtime's five fields, add a panel, the window's name: its command
	m = typeInto(clearField(m), "code {{.Path}}")
	m, _ = press(m, "enter")

	want := strings.Replace(sharedTargets, `launch = ["idea", "{{.Path}}"] # IntelliJ`, `launch = ["code", "{{.Path}}"] # IntelliJ`, 1)
	if got := configText(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "window: code {{.Path}}") {
		t.Errorf("config screen does not show the new command:\n%s", screen(m))
	}
}

// Panels are a list in the form: one deleted, one added with a kind, a title
// and a command, and all of it written when the target is saved.
func TestTheConfigScreenEditsPanels(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 0), "enter")
	m = downs(m, 8) // the agent panel
	m, _ = press(m, "alt+d")
	m = downs(m, 1) // add a panel
	m, _ = press(m, "enter")
	m, _ = press(m, "right") // shell to tool
	m, _ = press(m, "tab")
	m = typeInto(m, "tests")
	m, _ = press(m, "tab")
	m = typeInto(m, "mise watch")
	m, _ = press(m, "enter")
	if got := configText(t, root); got != sharedTargets {
		t.Fatalf("config.toml changed before the target was saved:\n%s", got)
	}
	m, _ = press(m, "up")
	m, _ = press(m, "up") // place
	m, _ = press(m, "enter")
	if !strings.Contains(screen(m), "runtime, 2 panels") {
		t.Errorf("config screen does not show the saved panels:\n%s", screen(m))
	}

	want := strings.Replace(sharedTargets, `    # the agent
    [[target.runtime.panels]]
    kind = "agent"
    title = "Claude Code"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"
`, `    # the agent
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"
    [[target.runtime.panels]]
    kind = "tool"
    title = "tests"
    command = ["mise", "watch"]
`, 1)
	if got := configText(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A new target is written at the end and listed.
func TestTheConfigScreenAddsASharedTarget(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 2), "enter")
	m = typeInto(m, "notes")
	m = downs(m, 10) // the window's command
	m = typeInto(m, "gedit")
	m = downs(m, 2) // its match class
	m = typeInto(m, "^gedit$")
	m, _ = press(m, "enter")

	want := sharedTargets + "\n[[target]]\nname = \"notes\"\n  [target.window]\n  launch = [\"gedit\"]\n  match = { class = \"^gedit$\" }\n"
	if got := configText(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "window: gedit") {
		t.Errorf("config screen does not list the new target:\n%s", screen(m))
	}
}

// Delete asks first. A target a project needs is not deleted, and the footer
// says why; one no project needs is.
func TestTheConfigScreenDeletesASharedTarget(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 0), "alt+d")
	if f := footer(m); !strings.Contains(f, `delete target "home" from every project?`) {
		t.Errorf("footer = %q, want the question", f)
	}
	m, _ = press(m, "y")
	if f := footer(m); !strings.Contains(f, "it would break") {
		t.Errorf("footer = %q, want the refusal", f)
	}
	if got := configText(t, root); got != sharedTargets {
		t.Errorf("config.toml changed:\n%s", got)
	}

	m, _ = press(m, "down")
	m, _ = press(m, "alt+d")
	m, _ = press(m, "y")
	if got := configText(t, root); strings.Contains(got, "editor") || !strings.Contains(got, "# IntelliJ") {
		t.Errorf("config.toml =\n%s\nwant editor gone and its comment kept", got)
	}
	if strings.Contains(screen(m), "window: idea") {
		t.Errorf("config screen still lists editor:\n%s", screen(m))
	}
}
