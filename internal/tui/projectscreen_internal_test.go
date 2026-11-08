package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
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
	// No survey has answered: what of the project runs is not known, so a
	// rename is refused.
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), core: &core.Core{Ledger: ledger.File{Root: t.TempDir()}}, projects: []core.Project{p}, list: 80}
	enter, down := pressedKey(tea.KeyPressMsg{Code: tea.KeyEnter}), pressedKey(tea.KeyPressMsg{Code: tea.KeyDown})
	ps := newProjectScreen(th)
	if err := ps.open(p, config.Inherited{}); err != nil {
		t.Fatal(err)
	}

	ps.key(sf, enter)
	ps.edit.SetValue("gadget")
	if res, _ := ps.key(sf, enter); res.err == nil || res.written != nil || !ps.edit.Focused() {
		t.Fatalf("result = %+v, want the rename refused and the field still typed in", res)
	}
	if _, err := os.Stat(p.File); err != nil {
		t.Errorf("project file after the refused rename: %v, want it kept", err)
	}
	ps.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyEscape}))

	ps.key(sf, down)
	ps.key(sf, enter)
	ps.edit.SetValue("/p/elsewhere")
	res, _ := ps.key(sf, enter)
	if res.err != nil || res.written == nil {
		t.Fatalf("result = %+v, want the project as its file now loads", res)
	}
	if got := res.written.Path; got != "/p/elsewhere" {
		t.Errorf("path = %q, want the one typed", got)
	}
	if ps.text.Path != "/p/elsewhere" || ps.edit.Focused() {
		t.Errorf("screen path = %q, typing = %v, want the file as written and the field left", ps.text.Path, ps.edit.Focused())
	}

	if res, _ := ps.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyEscape})); !res.closed {
		t.Errorf("result = %+v, want Esc on the rows to close the screen", res)
	}
}

// A rename of a project that left the surface since the screen opened is
// refused, naming it, and moves no file.
func TestTheProjectScreenRefusesToRenameAProjectThatIsGone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	p, err := config.Create(root, "widget", "/p/widget", "")
	if err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), core: &core.Core{Ledger: ledger.File{Root: t.TempDir()}}, settled: core.Settled{Surveyed: true}, list: 80}
	ps := newProjectScreen(th)
	if err := ps.open(p, config.Inherited{}); err != nil {
		t.Fatal(err)
	}

	if res := ps.rename(sf, "gadget"); res.err == nil || !strings.Contains(res.err.Error(), `"widget"`) || res.written != nil {
		t.Fatalf("result = %+v, want the rename refused, naming widget", res)
	}
	if _, err := os.Stat(p.File); err != nil {
		t.Errorf("project file after the refused rename: %v, want it kept", err)
	}
}
