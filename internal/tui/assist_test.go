package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
