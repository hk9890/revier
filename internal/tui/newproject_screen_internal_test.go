package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
)

// The new-project screen runs with no root model: a path typed into the
// field, the question about a folder that is not there, and the project it
// then writes and hands to the surface.
func TestTheNewProjectScreenWritesAProjectWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	// The folder is new, so no git and no mise run on it; with no tool on the
	// PATH none can.
	t.Setenv("PATH", t.TempDir())
	dir := filepath.Join(t.TempDir(), "widget")

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), list: 60}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	s := newCreateScreen(th)
	s.open()
	s.path.SetValue(dir)

	if res, _ := s.key(sf, enter); res.err != nil || res.created != nil || s.step != newMkdir {
		t.Fatalf("step = %v, result = %+v, want the question about the folder", s.step, res)
	}
	res, _ := s.key(sf, enter)
	if res.err != nil || res.created == nil {
		t.Fatalf("result = %+v, want the project written", res)
	}
	if c := res.created; c.project.Name != "widget" || !c.exists || c.clone {
		t.Errorf("created = %+v, want widget with its folder there", c)
	}
	if _, err := os.Stat(config.ProjectFile(root, "widget")); err != nil {
		t.Errorf("the project's file: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("the folder: %v", err)
	}

	s.open()
	if res, _ := s.key(sf, tea.KeyMsg{Type: tea.KeyEsc}); !res.closed {
		t.Errorf("result = %+v, want Esc on the field to close the screen", res)
	}
}
