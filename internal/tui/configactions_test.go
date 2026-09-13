package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
// or on the add row past the last.
func onActionRow(m tui.Model, i int) tui.Model {
	m, _ = press(m, "alt+c")
	for range 4 + i {
		m, _ = press(m, "down")
	}
	return m
}

// clearField deletes what a form field holds.
func clearField(m tui.Model) tui.Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	return next.(tui.Model)
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
	if got := configText(t, root); got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if !strings.Contains(screen(m), "copy path") {
		t.Errorf("config screen does not list the new action:\n%s", screen(m))
	}
	m, _ = press(m, "esc")
	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY}); cmd == nil {
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

	if got, want := configText(t, root), strings.Replace(syncConfig, `"ctrl+g"`, `"alt+y"`, 1); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	m, _ = press(m, "esc")
	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlG}); cmd != nil {
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
	if got := configText(t, root); got != syncConfig {
		t.Errorf("config.toml = %q after n, want it untouched", got)
	}

	m, _ = press(m, "alt+d")
	m, _ = press(m, "y")
	if got, want := configText(t, root), "# my actions\n# g for git\n"; got != want {
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
		if got := configText(t, root); got != syncConfig {
			t.Errorf("key %s: config.toml = %q, want it untouched", key, got)
		}
	}
}
