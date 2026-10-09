package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
)

// The config screen runs with no root model: a step on the theme row writes
// config.toml and hands back the theme to repaint in, and an action saved in
// its form comes back as the actions to bind.
func TestTheConfigScreenWritesAndHandsBackWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), list: 80}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	cs := newConfigScreen(th, &config.Config{})
	cs.open()

	res, _ := cs.key(sf, tea.KeyMsg{Type: tea.KeyRight})
	if res.err != nil || res.theme == nil || cs.ui.Theme == "" {
		t.Fatalf("result = %+v, ui = %+v, want a theme to repaint in", res, cs.ui)
	}
	written, err := os.ReadFile(config.File(root))
	if err != nil || !strings.Contains(string(written), cs.ui.Theme) {
		t.Errorf("config.toml = %q, %v, want the theme %q written", written, err, cs.ui.Theme)
	}

	// With no target and no action the row that adds an action is the last.
	cs.row = cs.addRow(sf)
	if res, _ := cs.key(sf, enter); res.err != nil || !cs.aform.open {
		t.Fatalf("form open = %v, err = %v, want the action form", cs.aform.open, res.err)
	}
	cs.aform.fields[fieldName].SetValue("sync")
	cs.aform.fields[fieldCommand].SetValue("git pull")
	cs.aform.fields[fieldKey].SetValue("ctrl+g")
	res, _ = cs.key(sf, enter)
	if res.err != nil || res.actions == nil || cs.aform.open {
		t.Fatalf("result = %+v, want the action saved and the form closed", res)
	}
	if got := *res.actions; len(got) != 1 || got[0].Name != "sync" || got[0].Key != "ctrl+g" {
		t.Errorf("actions = %+v, want the one saved", got)
	}

	if res, _ := cs.key(sf, tea.KeyMsg{Type: tea.KeyEsc}); !res.closed {
		t.Errorf("result = %+v, want Esc on the rows to close the screen", res)
	}
}
