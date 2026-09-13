package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

// writeConfig puts text in config.toml under a fresh root.
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(config.File(root), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readConfig(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(config.File(root))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A value is replaced on its own line. Every comment stays, the one after the
// value included.
func TestSetReplacesAValueAndKeepsEveryComment(t *testing.T) {
	root := writeConfig(t, `# revier
[ui]
# the palette
theme = "catppuccin-mocha" # dark
glyphs = "nerd"

[hosts]
window = ["none"]
`)
	if err := config.Set(root, "ui", "theme", "catppuccin-latte"); err != nil {
		t.Fatal(err)
	}
	want := `# revier
[ui]
# the palette
theme = "catppuccin-latte" # dark
glyphs = "nerd"

[hosts]
window = ["none"]
`
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A key the table does not have yet goes after the table's last value, not
// after the next table's comment.
func TestSetAddsAMissingKeyToItsTable(t *testing.T) {
	root := writeConfig(t, `[ui]
theme = "catppuccin-mocha"

# where things run
[hosts]
window = ["none"]
`)
	if err := config.Set(root, "ui", "glyphs", "ascii"); err != nil {
		t.Fatal(err)
	}
	want := `[ui]
theme = "catppuccin-mocha"
glyphs = "ascii"

# where things run
[hosts]
window = ["none"]
`
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A table the file does not have is added at its end, and a file that is not
// there is created.
func TestSetAddsAMissingTableAndAMissingFile(t *testing.T) {
	root := writeConfig(t, "# only a comment\n")
	if err := config.Set(root, "hosts", "runtime", []string{"tmux"}); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, root), "# only a comment\n\n[hosts]\nruntime = [\"tmux\"]\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}

	fresh := filepath.Join(t.TempDir(), "revier")
	if err := config.Set(fresh, "ui", "trigger_key", "alt-space"); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, fresh), "[ui]\ntrigger_key = \"alt-space\"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

// An array written over several lines is replaced whole, and the keys after
// it are untouched.
func TestSetReplacesAnArrayOverSeveralLines(t *testing.T) {
	root := writeConfig(t, `[hosts]
runtime = [
  "kitty", # first
  "tmux",
]
window = ["none"]
`)
	if err := config.Set(root, "hosts", "runtime", []string{}); err != nil {
		t.Fatal(err)
	}
	if got, want := readConfig(t, root), "[hosts]\nruntime = []\nwindow = [\"none\"]\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}

// A key of the same name in another table, or in an array of tables, is not
// the one written.
func TestSetWritesOnlyUnderItsOwnTable(t *testing.T) {
	root := writeConfig(t, `[[action]]
key = "ctrl+g"
name = "theme"
run = ["echo", "[ui]"]

[ui]
glyphs = "nerd"
`)
	if err := config.Set(root, "ui", "theme", "catppuccin-frappe"); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t, root)
	if !strings.HasSuffix(got, "[ui]\nglyphs = \"nerd\"\ntheme = \"catppuccin-frappe\"\n") {
		t.Errorf("config.toml =\n%s\nwant theme added under [ui] only", got)
	}
	if !strings.Contains(got, `run = ["echo", "[ui]"]`) {
		t.Errorf("config.toml =\n%s\nwant the action untouched", got)
	}
}

// A value Load would refuse is not written, and the file stays as it was.
func TestSetRefusesAValueLoadRefuses(t *testing.T) {
	const text = "[ui]\ntheme = \"catppuccin-mocha\"\n"
	root := writeConfig(t, text)
	if err := config.Set(root, "ui", "theme", "solarized"); err == nil {
		t.Error("an unknown theme was written")
	}
	if err := config.Set(root, "ui", "trigger_key", "frob-q"); err == nil {
		t.Error("a key that does not parse was written")
	}
	if got := readConfig(t, root); got != text {
		t.Errorf("config.toml = %q after refused writes, want %q", got, text)
	}
}

// A value the line editor cannot find where it looks - a dotted key - is
// refused rather than written a second time.
func TestSetRefusesAKeyItCannotEditInPlace(t *testing.T) {
	const text = "ui.theme = \"catppuccin-mocha\"\n"
	root := writeConfig(t, text)
	if err := config.Set(root, "ui", "theme", "catppuccin-latte"); err == nil {
		t.Error("a dotted key was edited as if it were not there")
	}
	if got := readConfig(t, root); got != text {
		t.Errorf("config.toml = %q, want it unchanged", got)
	}
}

// A config.toml linked in from elsewhere, a dotfiles repository, is written
// through the link, and the file keeps its permissions.
func TestSetWritesThroughASymlinkAndKeepsTheMode(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(target, []byte("[ui]\ntheme = \"catppuccin-mocha\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(target, config.File(root)); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(root, "ui", "theme", "catppuccin-latte"); err != nil {
		t.Fatal(err)
	}
	link, err := os.Lstat(config.File(root))
	if err != nil {
		t.Fatal(err)
	}
	if link.Mode()&os.ModeSymlink == 0 {
		t.Error("config.toml is no longer a symlink")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
	if got, want := readConfig(t, root), "[ui]\ntheme = \"catppuccin-latte\"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
}
