package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/tui"
)

const syncConfig = `# my actions
[[action]]
key = "ctrl+g" # g for git
name = "sync"
run = ["git", "pull"]
`

var syncAction = config.Action{Key: "ctrl+g", Name: "sync", Run: []string{"git", "pull"}}

// onActionRow opens the config screen with the cursor on the i-th action row,
// or on the add row past the last. The model has no shared targets, so the
// targets section is its add row alone.
func onActionRow(m tui.Model, i int) tui.Model {
	m, _ = press(m, "alt+c")
	for range 5 + i {
		m, _ = press(m, "down")
	}
	return m
}

// An action added on the screen is written to config.toml and bound at once:
// its key runs it on the surface, with no restart.
func TestTheConfigScreenAddsAnAction(t *testing.T) {
	root := configRoot(t, "")
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 30)

	m, _ = press(onActionRow(m, 0), "enter")
	m = typeInto(m, "copy path")
	m, _ = press(m, "tab")
	m = typeInto(m, "wl-copy {{ .Path }}")
	m, _ = press(m, "tab")
	m = typeInto(m, "ctrl+y")
	m, _ = press(m, "enter")

	want := "[[action]]\nkey = \"ctrl+y\"\nname = \"copy path\"\nrun = [\"wl-copy\", \"{{ .Path }}\"]\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "copy path") {
		t.Errorf("config screen does not list the new action:\n%s", screen(m))
	}
	m, _ = press(m, "esc")
	if _, cmd := send(m, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("ctrl+y ran nothing after the action was added")
	}
}

// A changed action is written in place, its comment kept, and its old key no
// longer runs it.
func TestTheConfigScreenChangesAnAction(t *testing.T) {
	root := configRoot(t, syncConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction}), 120, 30)

	m, _ = press(onActionRow(m, 0), "enter")
	m, _ = press(m, "tab")
	m, _ = press(m, "tab")
	m = typeInto(clearField(m), "alt+y")
	m, _ = press(m, "enter")

	if got, want := fileText(t, config.File(root)), strings.Replace(syncConfig, `"ctrl+g"`, `"alt+y"`, 1); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	m, _ = press(m, "esc")
	if _, cmd := send(m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}); cmd != nil {
		t.Error("ctrl+g still runs the action after its key changed")
	}
}

// Delete asks first. Any key but y keeps the action; y removes it from the
// file, and its comments stay.
func TestTheConfigScreenDeletesAnAction(t *testing.T) {
	root := configRoot(t, syncConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction}), 120, 30)

	m, _ = press(onActionRow(m, 0), "alt+d")
	if f := footer(m); !strings.Contains(f, `delete action "sync"`) {
		t.Errorf("footer = %q, want the question", f)
	}
	m, _ = press(m, "n")
	if got := fileText(t, config.File(root)); got != syncConfig {
		t.Errorf("config.toml = %q after n, want it untouched", got)
	}

	m, _ = press(m, "alt+d")
	m, _ = press(m, "y")
	if got, want := fileText(t, config.File(root)), "# my actions\n# g for git\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if strings.Contains(screen(m), "git pull") {
		t.Errorf("config screen still lists the action:\n%s", screen(m))
	}
}

// A key that already means something on the surface is refused and named,
// and nothing is written.
func TestTheConfigScreenRefusesATakenKey(t *testing.T) {
	_, _, c, projects := world(t, 1)
	for key, want := range map[string]string{
		"ctrl+g": `key of action "sync"`,
		"alt+c":  "revier's own keys",
		"ctrl+o": `key of target "editor"`,
		"ctrl+w": "edits the query",
	} {
		root := configRoot(t, syncConfig)
		m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction}), 160, 30)
		m, _ = press(onActionRow(m, 1), "enter")
		m = typeInto(m, "other")
		m, _ = press(m, "tab")
		m = typeInto(m, "true")
		m, _ = press(m, "tab")
		m = typeInto(m, key)
		m, _ = press(m, "enter")
		if f := footer(m); !strings.Contains(f, want) {
			t.Errorf("key %s: footer = %q, want %q", key, f, want)
		}
		if got := fileText(t, config.File(root)); got != syncConfig {
			t.Errorf("key %s: config.toml = %q, want it untouched", key, got)
		}
	}
}

const syncBuildConfig = `[[action]]
key = "ctrl+g"
name = "sync"
run = ["git", "pull"]

[[action]]
key = "ctrl+b"
name = "build"
run = ["make"]
`

var buildAction = config.Action{Key: "ctrl+b", Name: "build", Run: []string{"make"}}

// renamedBuild is the action form, open on build, given a new name and key
// and saved.
func renamedBuild(m tui.Model) tui.Model {
	m = typeInto(clearField(m), "compile")
	m, _ = press(m, "tab")
	m, _ = press(m, "tab")
	m = typeInto(clearField(m), "alt+y")
	m, _ = press(m, "enter")
	return m
}

// The form saves to the action it was opened on, also when config.toml got
// an action above it while the surface was away: the action now at the
// form's old place is not overwritten.
func TestTheActionFormSavesToItsActionAfterOneIsAddedAbove(t *testing.T) {
	root := configRoot(t, syncBuildConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction, buildAction}), 120, 30)

	m, _ = press(onActionRow(m, 1), "enter")
	lint := "[[action]]\nkey = \"ctrl+l\"\nname = \"lint\"\nrun = [\"lint\"]\n\n"
	m = rewritten(t, m, root, lint+syncBuildConfig)
	renamedBuild(m)

	want := lint + strings.Replace(strings.Replace(syncBuildConfig, `"ctrl+b"`, `"alt+y"`, 1), `"build"`, `"compile"`, 1)
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// The form saves to the action it was opened on when the list shrank to the
// form's old place: the edit is not added as a second action.
func TestTheActionFormSavesToItsActionAfterOneAboveIsRemoved(t *testing.T) {
	root := configRoot(t, syncBuildConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction, buildAction}), 120, 30)

	m, _ = press(onActionRow(m, 1), "enter")
	build := "[[action]]\nkey = \"ctrl+b\"\nname = \"build\"\nrun = [\"make\"]\n"
	m = rewritten(t, m, root, build)
	renamedBuild(m)

	if got, want := fileText(t, config.File(root)), "[[action]]\nkey = \"alt+y\"\nname = \"compile\"\nrun = [\"make\"]\n"; got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// An action that left config.toml while its form was open is not written:
// the footer says so, and the form stays with what was typed.
func TestTheActionFormRefusesAnActionThatIsGone(t *testing.T) {
	root := configRoot(t, syncBuildConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction, buildAction}), 120, 30)

	m, _ = press(onActionRow(m, 1), "enter")
	m = rewritten(t, m, root, "")
	m = renamedBuild(m)

	if got := fileText(t, config.File(root)); got != "" {
		t.Errorf("config.toml = %q, want it untouched", got)
	}
	if f := footer(m); !strings.Contains(f, `action "build" is no longer in config.toml`) {
		t.Errorf("footer = %q, want it to say the action is gone", f)
	}
	if !strings.Contains(screen(m), "compile") {
		t.Errorf("the form is gone, or lost what was typed:\n%s", screen(m))
	}
}

// y answers the question for the action it was asked about, also when
// config.toml got an action above it while the question waited: the action
// now under the cursor is not deleted.
func TestTheDeleteQuestionDeletesItsActionAfterOneIsAddedAbove(t *testing.T) {
	root := configRoot(t, syncBuildConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction, buildAction}), 120, 30)

	m, _ = press(onActionRow(m, 1), "alt+d")
	lint := "[[action]]\nkey = \"ctrl+l\"\nname = \"lint\"\nrun = [\"lint\"]\n\n"
	m = rewritten(t, m, root, lint+syncBuildConfig)
	if f := footer(m); !strings.Contains(f, `delete action "build"`) {
		t.Errorf("footer = %q, want the question about build", f)
	}
	press(m, "y")

	want := lint + "[[action]]\nkey = \"ctrl+g\"\nname = \"sync\"\nrun = [\"git\", \"pull\"]\n"
	if got := fileText(t, config.File(root)); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// An action that left config.toml while the question waited is not
// deleted, nor is the one now in its place: the footer says it is gone.
func TestTheDeleteQuestionRefusesAnActionThatIsGone(t *testing.T) {
	root := configRoot(t, syncBuildConfig)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), []config.Action{syncAction, buildAction}), 120, 30)

	m, _ = press(onActionRow(m, 0), "alt+d")
	build := "[[action]]\nkey = \"ctrl+b\"\nname = \"build\"\nrun = [\"make\"]\n"
	m = rewritten(t, m, root, build)
	m, _ = press(m, "y")

	if got := fileText(t, config.File(root)); got != build {
		t.Errorf("config.toml = %q, want it untouched", got)
	}
	if f := footer(m); !strings.Contains(f, `action "sync" is no longer in config.toml`) {
		t.Errorf("footer = %q, want it to say the action is gone", f)
	}
}
