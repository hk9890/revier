package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
)

// The project screen runs with no root model: a path typed over the old one
// is written to the project file and handed back as the project it now loads,
// and a rename the surface refuses writes nothing.
func TestTheProjectScreenWritesAFieldWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	p, err := config.Create(root, "widget", "/p/widget", "")
	if err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), projects: []core.Project{p}, list: 80}
	enter, down := tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyDown}
	ps := newProjectScreen(th)
	if err := ps.open(p, nil); err != nil {
		t.Fatal(err)
	}

	locked := errors.New("widget is running")
	ps.key(sf, locked, enter)
	ps.edit.SetValue("gadget")
	if res, _ := ps.key(sf, locked, enter); !errors.Is(res.err, locked) || res.written != nil || !ps.edit.Focused() {
		t.Fatalf("result = %+v, want the rename refused and the field still typed in", res)
	}
	ps.key(sf, locked, tea.KeyMsg{Type: tea.KeyEsc})

	ps.key(sf, nil, down)
	ps.key(sf, nil, enter)
	ps.edit.SetValue("/p/elsewhere")
	res, _ := ps.key(sf, nil, enter)
	if res.err != nil || res.written == nil {
		t.Fatalf("result = %+v, want the project as its file now loads", res)
	}
	if got := res.written.Path; got != "/p/elsewhere" {
		t.Errorf("path = %q, want the one typed", got)
	}
	if ps.text.Path != "/p/elsewhere" || ps.edit.Focused() {
		t.Errorf("screen path = %q, typing = %v, want the file as written and the field left", ps.text.Path, ps.edit.Focused())
	}

	if res, _ := ps.key(sf, nil, tea.KeyMsg{Type: tea.KeyEsc}); !res.closed {
		t.Errorf("result = %+v, want Esc on the rows to close the screen", res)
	}
}
