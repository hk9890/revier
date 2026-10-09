package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/theme"
)

// The sessions screen runs with no root model: it lists what is saved, the
// save key opens the name step, and a save that is running refuses a second.
func TestTheSessionsScreenListsAndNamesWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	stored, _, err := session.Save(root, session.Session{
		Name: "before-reboot", At: time.Date(2026, 9, 15, 18, 10, 0, 0, time.Local),
		Projects: []session.Project{{Name: "work", Targets: []session.Target{{Name: "home"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), stateRoot: root, list: 60, pane: 40}
	sc := newSessionsScreen(th)
	if err := sc.open(root, th); err != nil {
		t.Fatal(err)
	}
	if it, ok := sc.list.SelectedItem().(sessionItem); !ok || it.session.ID != stored.ID {
		t.Fatalf("selected = %+v, want the saved session", sc.list.SelectedItem())
	}
	if pane := sc.detail(sf); !strings.Contains(pane, "before-reboot") || !strings.Contains(pane, "surveying") {
		t.Errorf("pane = %q, want the session, and its plan pending before a survey", pane)
	}

	save := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s"), Alt: true}
	if res, _ := sc.key(sf, save); res.err != nil || !sc.naming {
		t.Fatalf("naming = %v, err = %v, want the name step", sc.naming, res.err)
	}
	if res, _ := sc.key(sf, tea.KeyMsg{Type: tea.KeyEsc}); res.closed || sc.naming {
		t.Fatalf("naming = %v, closed = %v, want Esc back on the sessions", sc.naming, res.closed)
	}

	sc.saving = true
	if res, _ := sc.key(sf, save); res.err == nil || sc.naming {
		t.Errorf("err = %v, want a second save refused while one runs", res.err)
	}
	if res, _ := sc.key(sf, tea.KeyMsg{Type: tea.KeyEsc}); !res.closed {
		t.Errorf("result = %+v, want Esc on the sessions to close the screen", res)
	}
}
