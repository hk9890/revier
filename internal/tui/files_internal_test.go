package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
)

const editable = `
path = "/p/alpha"
[[target]]
name = "home"
home = true
  [target.runtime]
  launch = ["sh"]
  match = { title = "^home$" }
`

func editableModel(t *testing.T) (Model, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "alpha.toml")
	if err := os.WriteFile(file, []byte(editable), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadProject(file)
	if err != nil {
		t.Fatal(err)
	}
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, []core.Project{p}, t.TempDir(), nil, time.Second, theme.Default(), "")
	return m, file
}

// What the editor wrote is what the next refresh surveys, without a restart.
func TestAnEditedFileIsReadBack(t *testing.T) {
	m, file := editableModel(t)
	if err := os.WriteFile(file, []byte(strings.Replace(editable, "/p/alpha", "/p/moved", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(editedMsg{project: "alpha", file: file})
	m = next.(Model)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if got := m.projects[0].Path; got != "/p/moved" {
		t.Errorf("path = %q, want the edited one", got)
	}
}

// A file the edit broke keeps the project as it was, and says why, so it
// can still be reached to fix.
func TestAnEditThatDoesNotLoadKeepsTheProject(t *testing.T) {
	m, file := editableModel(t)
	if err := os.WriteFile(file, []byte("path = \"/p/alpha\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(editedMsg{project: "alpha", file: file})
	m = next.(Model)
	if m.err == nil || !strings.Contains(m.err.Error(), "before the edit") {
		t.Errorf("err = %v, want the load failure and that the old version stays", m.err)
	}
	if len(m.projects) != 1 || len(m.projects[0].Targets) != 1 {
		t.Errorf("projects = %+v, want the version before the edit", m.projects)
	}
}
