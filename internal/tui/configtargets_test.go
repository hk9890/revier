package tui_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
)

// targetsSurface is the surface over a configuration root whose config.toml
// holds sharedTargets and one project with no target of its own, and that
// root.
func targetsSurface(t *testing.T) (tui.Model, string) {
	t.Helper()
	m, file, _ := projectSurfaceOver(t, sharedTargets, "path = \"/tmp/demo\"\n", hosttest.NewRuntime("rt"))
	return m, filepath.Dir(filepath.Dir(file))
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

// The screen lists every shared target with its key and where it opens: a
// target that lists its tabs with them, and a tab with the target it is inside.
func TestTheConfigScreenListsTheSharedTargets(t *testing.T) {
	m, _ := targetsSurface(t)
	m, _ = press(m, "alt+c")
	for _, want := range []string{"ctrl+shift+u", "runtime, tabs: agent · home", "ctrl+shift+o", "window: idea {{.Path}}", "tab of home, 2 panels", "add a target"} {
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
	m = downs(m, 13) // name, key, home, the runtime's eight fields, add a panel, the window's name: its command
	m = typeInto(clearField(m), "code {{.Path}}")
	m, _ = press(m, "enter")

	want := strings.Replace(sharedTargets, `launch = ["idea", "{{.Path}}"] # IntelliJ`, `launch = ["code", "{{.Path}}"] # IntelliJ`, 1)
	if got := fileText(t, config.File(root)); got != want {
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
	m, _ = press(onTargetRow(m, 2), "enter")
	m = downs(m, 11) // the agent panel
	m, _ = press(m, "alt+d")
	m = downs(m, 1) // add a panel
	m, _ = press(m, "enter")
	m, _ = press(m, "right") // shell to tool
	m, _ = press(m, "tab")
	m = typeInto(m, "tests")
	m, _ = press(m, "tab")
	m = typeInto(m, "mise watch")
	m, _ = press(m, "enter")
	if got := fileText(t, config.File(root)); got != sharedTargets {
		t.Fatalf("config.toml changed before the target was saved:\n%s", got)
	}
	m, _ = press(m, "up")
	m, _ = press(m, "up") // active
	m, _ = press(m, "enter")
	if !strings.Contains(screen(m), "tab of home, 2 panels") {
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
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A new target is written at the end and listed.
func TestTheConfigScreenAddsASharedTarget(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 3), "enter")
	m = typeInto(m, "notes")
	m = downs(m, 13) // the window's command
	m = typeInto(m, "gedit")
	m = downs(m, 2) // its match class
	m = typeInto(m, "^gedit$")
	m, _ = press(m, "enter")

	want := sharedTargets + "\n[[target]]\nname = \"notes\"\n  [target.window]\n  launch = [\"gedit\"]\n  match = { class = \"^gedit$\" }\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "window: gedit") {
		t.Errorf("config screen does not list the new target:\n%s", screen(m))
	}
}

// The form makes a tab: the target it is inside is a field of the runtime,
// and its panels are written under it. The command's note says what starts a
// target that has none.
func TestTheConfigScreenAddsATabWithAPanel(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 3), "enter")
	if !strings.Contains(screen(m), "empty when tabs or panels are what starts") {
		t.Errorf("the form does not say what starts a target with no command:\n%s", screen(m))
	}
	m = typeInto(m, "tickets")
	m = downs(m, 8) // inside
	m = typeInto(m, "home")
	m = downs(m, 3) // add a panel
	m, _ = press(m, "enter")
	m, _ = press(m, "right") // shell to tool
	m, _ = press(m, "tab")
	m = typeInto(m, "tests")
	m, _ = press(m, "tab")
	m = typeInto(m, "tt")
	m, _ = press(m, "enter")
	m, _ = press(m, "up") // active
	m, _ = press(m, "enter")

	want := sharedTargets + "\n[[target]]\nname = \"tickets\"\n  [target.runtime]\n  inside = \"home\"\n    [[target.runtime.panels]]\n    kind = \"tool\"\n    title = \"tests\"\n    command = [\"tt\"]\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "tab of home, 1 panel") {
		t.Errorf("config screen does not list the new tab:\n%s", screen(m))
	}
}

// A target that says only what it is inside is still a runtime realization:
// the form hands it on, and the reason it is not written is the rule's, which
// names what the tab lacks.
func TestTheConfigScreenKeepsATabThatHasOnlyItsInside(t *testing.T) {
	m, root := targetsSurface(t)
	m, _ = press(onTargetRow(m, 3), "enter")
	m = typeInto(m, "tickets")
	m = downs(m, 8) // inside
	m = typeInto(m, "home")
	m, _ = press(m, "enter")
	if f := footer(m); !strings.Contains(f, `is inside "home" and has no launch argv and no panels`) {
		t.Errorf("footer = %q, want the tab refused for having nothing to run", f)
	}
	if got := fileText(t, config.File(root)); got != sharedTargets {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// A target of the form before tabs, panels on a runtime realization that is
// no tab, is refused in every project. Naming the target it is inside
// repairs it from the form.
func TestTheConfigScreenRepairsPanelsOnATargetThatIsNoTab(t *testing.T) {
	const oldForm = sharedTargets + `
[[target]]
name = "logs"
  [target.runtime]
  name = "logs:{{.Name}}"
  match = { title = "^logs:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "tool"
    command = ["tail"]
`
	m, file, _ := projectSurfaceOver(t, oldForm, "path = \"/tmp/demo\"\n", hosttest.NewRuntime("rt"))
	root := filepath.Dir(filepath.Dir(file))
	_, before, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if probs := config.Problems(before[0]); len(probs) != 1 || !strings.Contains(probs[0].Error(), `target "logs" runtime realization declares panels; only a tab has them`) {
		t.Fatalf("Problems = %v, want logs refused for its panels", probs)
	}

	m, _ = press(onTargetRow(m, 3), "enter")
	m = downs(m, 8) // inside
	m = typeInto(m, "home")
	m, _ = press(m, "enter")

	if got := fileText(t, config.File(root)); !strings.Contains(got, "  inside = \"home\"\n    [[target.runtime.panels]]\n    kind = \"tool\"") {
		t.Errorf("config.toml =\n%s\nwant logs inside home, its panel kept", got)
	}
	_, after, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if probs := config.Problems(after[0]); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
	if !strings.Contains(screen(m), "tab of home, 1 panel") {
		t.Errorf("config screen does not list logs as a tab:\n%s", screen(m))
	}
}

// The form makes the target that lists its tabs: a home with a name, a match
// and the tab it opens with, beside a tab that was inside no declared target.
func TestTheConfigScreenAddsATargetThatListsItsTabs(t *testing.T) {
	const tabAlone = `[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]
`
	m, file, _ := projectSurfaceOver(t, tabAlone, "path = \"/tmp/demo\"\n", hosttest.NewRuntime("rt"))
	root := filepath.Dir(filepath.Dir(file))

	m, _ = press(onTargetRow(m, 1), "enter")
	m = typeInto(m, "home")
	m = downs(m, 2) // home
	m, _ = press(m, "right")
	m = downs(m, 1) // the runtime's name
	m = typeInto(m, "session:{{.Name}}")
	m = downs(m, 2) // its match title
	m = typeInto(m, "^session:{{.Name}}$")
	m = downs(m, 4) // tabs
	m = typeInto(m, "agent")
	m, _ = press(m, "enter")

	want := tabAlone + "\n[[target]]\nname = \"home\"\nhome = true\n  [target.runtime]\n  name = \"session:{{.Name}}\"\n  tabs = [\"agent\"]\n  match = { title = \"^session:{{.Name}}$\" }\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Fatalf("config.toml =\n%s\nwant\n%s", got, want)
	}
	_, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if probs := config.Problems(projects[0]); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
	if !strings.Contains(screen(m), "runtime, tabs: agent · home") {
		t.Errorf("config screen does not list the tabs of home:\n%s", screen(m))
	}
}

// The list of tabs and the active tab are fields of the form: a tab added to
// the list and made the active one is written in place.
func TestTheConfigScreenChangesTheTabsAndTheActiveTab(t *testing.T) {
	const withNotes = sharedTargets + `
[[target]]
name = "notes"
  [target.runtime]
  inside = "home"
  launch = ["vi", "notes.md"]
`
	m, file, _ := projectSurfaceOver(t, withNotes, "path = \"/tmp/demo\"\n", hosttest.NewRuntime("rt"))
	root := filepath.Dir(filepath.Dir(file))

	m, _ = press(onTargetRow(m, 0), "enter")
	for _, want := range []string{"the tabs it opens with, in order", "the tab that has the focus"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("the form does not say %q:\n%s", want, screen(m))
		}
	}
	m = downs(m, 9) // tabs
	m = typeInto(clearField(m), "notes, agent")
	m = downs(m, 1) // active
	m = typeInto(m, "agent")
	m, _ = press(m, "enter")

	want := strings.Replace(withNotes, `  tabs = ["agent"]`, "  tabs = [\"notes\", \"agent\"]\n  active = \"agent\"", 1)
	if got := fileText(t, config.File(root)); got != want {
		t.Fatalf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(screen(m), "runtime, tabs: notes, agent · home") {
		t.Errorf("config screen does not list the new order:\n%s", screen(m))
	}

	m, _ = press(m, "enter") // the cursor is on the target that was saved
	m = downs(m, 10)         // active
	m = typeInto(clearField(m), "editor")
	m, _ = press(m, "enter")
	if f := footer(m); !strings.Contains(f, `has active "editor", which is not an entry of its tabs`) {
		t.Errorf("footer = %q, want the active tab refused", f)
	}
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml changed:\n%s", got)
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
	if got := fileText(t, config.File(root)); got != sharedTargets {
		t.Errorf("config.toml changed:\n%s", got)
	}

	m, _ = press(m, "down")
	m, _ = press(m, "alt+d")
	m, _ = press(m, "y")
	if got := fileText(t, config.File(root)); strings.Contains(got, "editor") || !strings.Contains(got, "# IntelliJ") {
		t.Errorf("config.toml =\n%s\nwant editor gone and its comment kept", got)
	}
	if strings.Contains(screen(m), "window: idea") {
		t.Errorf("config screen still lists editor:\n%s", screen(m))
	}
}
