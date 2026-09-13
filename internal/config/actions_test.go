package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

const twoActions = `# revier
[ui]
theme = "catppuccin-mocha"

# copies the path
[[action]]
key = "ctrl+y" # yank
name = "copy path"
run = [
  "wl-copy", # the clipboard
  "{{.Path}}",
]

[[action]]
key = "ctrl+g"
name = "sync"
run = ["git", "pull"]
`

var (
	copyPath = config.Action{Key: "ctrl+y", Name: "copy path", Run: []string{"wl-copy", "{{.Path}}"}}
	sync     = config.Action{Key: "ctrl+g", Name: "sync", Run: []string{"git", "pull"}}
)

// A new action is a new [[action]] table at the end; nothing before it moves.
func TestAddActionAppendsATable(t *testing.T) {
	root := writeConfig(t, twoActions)
	lint := config.Action{Key: "alt+l", Name: "lint", Run: []string{"mise", "run", "lint"}}
	if err := config.AddAction(root, []config.Action{copyPath, sync}, lint); err != nil {
		t.Fatal(err)
	}
	want := twoActions + "\n[[action]]\nkey = \"alt+l\"\nname = \"lint\"\nrun = [\"mise\", \"run\", \"lint\"]\n"
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

func TestAddActionWritesAMissingFile(t *testing.T) {
	root := t.TempDir()
	if err := config.AddAction(root, nil, sync); err != nil {
		t.Fatal(err)
	}
	want := "[[action]]\nkey = \"ctrl+g\"\nname = \"sync\"\nrun = [\"git\", \"pull\"]\n"
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

// A changed action keeps its place, its comments and the comments after its
// values; an array over several lines is replaced whole.
func TestReplaceActionKeepsItsComments(t *testing.T) {
	root := writeConfig(t, twoActions)
	now := config.Action{Key: "alt+y", Name: "copy", Run: []string{"xclip", "{{.Path}}"}}
	if err := config.ReplaceAction(root, 0, copyPath, now); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(twoActions, `key = "ctrl+y" # yank
name = "copy path"
run = [
  "wl-copy", # the clipboard
  "{{.Path}}",
]`, `key = "alt+y" # yank
name = "copy"
run = ["xclip", "{{.Path}}"]`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A value that did not change is not written: a renamed action keeps its
// array over several lines, and the comment inside it.
func TestReplaceActionWritesOnlyWhatChanged(t *testing.T) {
	root := writeConfig(t, twoActions)
	renamed := copyPath
	renamed.Name = "copy"
	if err := config.ReplaceAction(root, 0, copyPath, renamed); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(twoActions, `name = "copy path"`, `name = "copy"`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A removed action loses its header and values. Its comments stay, and so
// does the other action.
func TestRemoveActionKeepsItsComments(t *testing.T) {
	root := writeConfig(t, twoActions)
	if err := config.RemoveAction(root, 0, copyPath); err != nil {
		t.Fatal(err)
	}
	want := `# revier
[ui]
theme = "catppuccin-mocha"

# copies the path
# yank
# the clipboard

[[action]]
key = "ctrl+g"
name = "sync"
run = ["git", "pull"]
`
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// The last action removed leaves no blank lines behind it.
func TestRemoveActionDropsAnEmptyEntry(t *testing.T) {
	root := writeConfig(t, twoActions)
	if err := config.RemoveAction(root, 1, sync); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(twoActions, "\n[[action]]\nkey = \"ctrl+g\"\nname = \"sync\"\nrun = [\"git\", \"pull\"]\n")
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%q\nwant\n%q", got, want)
	}
}

// An action that is no longer what the caller read is not touched: the file
// was edited by hand since.
func TestAnEditToAChangedActionIsRefused(t *testing.T) {
	root := writeConfig(t, twoActions)
	stale := config.Action{Key: "ctrl+y", Name: "copy", Run: []string{"wl-copy"}}
	if err := config.RemoveAction(root, 0, stale); !errors.Is(err, config.ErrActionsChanged) {
		t.Errorf("RemoveAction = %v, want ErrActionsChanged", err)
	}
	if err := config.ReplaceAction(root, 2, sync, sync); !errors.Is(err, config.ErrActionsChanged) {
		t.Errorf("ReplaceAction past the end = %v, want ErrActionsChanged", err)
	}
	// sync was added by hand since the caller read only copy path, so adding
	// sync again would write it twice.
	if err := config.AddAction(root, []config.Action{copyPath}, sync); !errors.Is(err, config.ErrActionsChanged) {
		t.Errorf("AddAction over an action added by hand = %v, want ErrActionsChanged", err)
	}
	if got := readConfig(t, root); got != twoActions {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// An action Load would refuse is not written.
func TestAddActionRefusesAnInvalidAction(t *testing.T) {
	root := writeConfig(t, twoActions)
	for _, act := range []config.Action{
		{Key: "g", Name: "typed", Run: []string{"true"}},
		{Key: "ctrl+g", Name: "empty"},
	} {
		if err := config.AddAction(root, []config.Action{copyPath, sync}, act); err == nil {
			t.Errorf("AddAction(%+v) accepted", act)
		}
	}
	if got := readConfig(t, root); got != twoActions {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// Actions written as an inline array are not tables the line editor finds,
// so they are refused rather than written twice.
func TestActionsNotWrittenAsTablesAreRefused(t *testing.T) {
	text := "action = [{ key = \"ctrl+g\", name = \"sync\", run = [\"git\", \"pull\"] }]\n"
	root := writeConfig(t, text)
	if err := config.AddAction(root, nil, copyPath); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("AddAction = %v, want a refusal", err)
	}
	if got := readConfig(t, root); got != text {
		t.Errorf("config.toml changed:\n%s", got)
	}
}
