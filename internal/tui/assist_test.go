package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// alt+a hands the terminal over: the press answers with the command that
// runs the assistant, and opens no screen of the surface's own.
func TestAltAHandsTheTerminalToTheAssistant(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 160, 20)

	if bar := barLine(m); !strings.Contains(bar, "assist alt+a") {
		t.Errorf("bar = %q, want it to name the assist button and its key", bar)
	}
	before := m.View()
	m, cmd := press(m, "alt+a")
	if cmd == nil {
		t.Fatal("alt+a returned no command")
	}
	if m.View() != before {
		t.Error("alt+a changed the surface before the hand-over")
	}
}

// What the assistant wrote is on the surface when it comes back: the files
// are read again, as on the raise of the popup.
func TestTheSurfaceReadsTheFilesAgainAfterTheAssistant(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "written-by-the-assistant.toml"), []byte("path = \"/p/written\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Assisted(nil)
	if cmd == nil {
		t.Fatal("the assistant's exit read nothing")
	}
	m, _ = deliver(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "written-by-the-assistant") {
		t.Errorf("the surface lists no project the assistant wrote:\n%s", body)
	}
}

// A config.toml the assistant left that does not load is said under the list
// as it was: the user is back to read what the assistant wrote.
func TestAConfigurationTheAssistantBrokeIsSaid(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 200, 20)

	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("this is not = = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.Assisted(nil)
	m, _ = deliver(m, cmd)
	if got := footer(m); !strings.Contains(got, "config.toml") {
		t.Errorf("footer = %q, want it to name the file that does not load", got)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "project-00") {
		t.Errorf("the list did not stand over the broken configuration:\n%s", body)
	}
}

// bubbletea gives the terminal back from a hand-over with mouse reporting
// off, so the surface asks for it again.
func TestTheSurfaceTakesTheMouseBackAfterTheAssistant(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 2)
	m := refreshed(t, c, projects, stateWith(t, nil), nil)

	_, cmd := m.Assisted(nil)
	asked := false
	each(cmd, func(msg tea.Msg) { asked = asked || msg == tea.EnableMouseAllMotion() })
	if !asked {
		t.Error("the assistant's exit did not turn mouse reporting on again")
	}
}
