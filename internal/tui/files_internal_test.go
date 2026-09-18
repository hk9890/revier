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
  name = "home"
  launch = ["sh"]
  match = { title = "^home$" }
`

func editableModel(t *testing.T) (Model, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "alpha.toml")
	if err := os.WriteFile(file, []byte(editable), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.LoadProject(file, nil)
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, []core.Project{p}, t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
	return m, file
}

// A survey still running holds the project list it started with, on another
// goroutine. A written project goes into a new list; written into that one, it
// would race the survey reading it.
func TestAWrittenProjectLeavesTheListARunningSurveyHolds(t *testing.T) {
	m, file := editableModel(t)
	held := m.projects // what a running Survey command captured
	if err := os.WriteFile(file, []byte(strings.Replace(editable, "/p/alpha", "/p/moved", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.LoadProject(file, nil)
	m.replaceProject("alpha", p)
	if got := held[0].Path; got != "/p/alpha" {
		t.Errorf("the running survey's list changed under it: path = %q", got)
	}
	if got := m.projects[0].Path; got != "/p/moved" {
		t.Errorf("path = %q, want the written one", got)
	}
}
