package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

// renameRoot is a configuration root with one project file in it.
func renameRoot(t *testing.T, name, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), name+".toml", body)
	return root
}

func TestRenameMovesTheFileAndKeepsItsText(t *testing.T) {
	root := renameRoot(t, "revier", "# kept\n"+valid)
	p, err := config.Rename(root, "revier", "rv", nil)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if p.Name != "rv" || p.File != config.ProjectFile(root, "rv") {
		t.Errorf("project %q in %s, want rv in its own file", p.Name, p.File)
	}
	if _, err := os.Stat(config.ProjectFile(root, "revier")); !os.IsNotExist(err) {
		t.Errorf("old file: %v, want it gone", err)
	}
	got, _ := os.ReadFile(p.File)
	if string(got) != "# kept\n"+valid {
		t.Errorf("file = %q, want the old text unchanged", got)
	}
}

// A link that leaves its name on the host to default to its own would reach
// another project there once renamed; the old name is written to keep it.
func TestRenameKeepsTheProjectALinkReaches(t *testing.T) {
	root := renameRoot(t, "far", link)
	p, err := config.Rename(root, "far", "near", nil)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if p.Name != "near" || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Errorf("link %q on %q reaches %q, want near on buildbox reaching far", p.Name, p.Remote.Host, p.Remote.Project)
	}
}

func TestRenameLeavesALinkThatNamesTheHostsProject(t *testing.T) {
	body := strings.Replace(link, `host = "buildbox"`, "host = \"buildbox\"\nproject = \"other\"", 1)
	root := renameRoot(t, "far", body)
	p, err := config.Rename(root, "far", "near", nil)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got, _ := os.ReadFile(p.File); string(got) != body {
		t.Errorf("file = %q, want it unchanged", got)
	}
}

// `revier new` writes the name into a match pattern where a regexp would
// read it, such as the dot in "example.com", under a name that renders
// {{.Name}}. The pattern would look for the old name after the rename while
// the name followed, so the rename is refused and names it.
func TestRenameRefusesAFileThatWritesTheNameOut(t *testing.T) {
	body := strings.NewReplacer("  name = \"home\"\n", "  name = \"session:{{.Name}}\"\n", "^session:{{.Name}}$", `^session:example\\.com$`).Replace(valid)
	root := renameRoot(t, "example.com", body)
	_, err := config.Rename(root, "example.com", "web", nil)
	if err == nil || !strings.Contains(err.Error(), "writes the name out") || !strings.Contains(err.Error(), "home") {
		t.Errorf("err = %v, want the refusal naming the target", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "example.com")); err != nil {
		t.Errorf("old file: %v, want it kept", err)
	}
}

// A project named after a program its file runs - claude, with a panel that
// runs claude - is renamed like any other: a launch or a panel command is
// nothing Match reads, and a realization that writes the name out everywhere
// stays consistent with itself.
func TestRenameReadsOnlyWhatMatchReads(t *testing.T) {
	root := renameRoot(t, "claude", valid)
	if _, err := config.Rename(root, "claude", "cl", nil); err != nil {
		t.Errorf("Rename: %v, want a project named after its program renamed", err)
	}

	pane := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  name = "session:far"
  match = { title = "^session:far$" }
    [[target.remote.runtime.panels]]
    kind = "agent"
    command = ["sh", "-c", "exec ssh buildbox revier agent exec -p 'far'", "sh"]
`
	root = renameRoot(t, "far", pane)
	p, err := config.Rename(root, "far", "near", nil)
	if err != nil || p.Remote.Project != "far" {
		t.Errorf("Rename = %+v, %v; want the link's written-out home kept, reaching far", p.Project, err)
	}
}

// A rename is refused for what it breaks, not for what was broken before it:
// a target refused at load stays refused under the new name, and is no reason
// to keep the old one.
func TestRenameToleratesWhatWasAlreadyBroken(t *testing.T) {
	body := valid + "\n[[target]]\nname = \"docs\"\n  [target.window]\n  launch = [\"zeal\"]\n  match = { class = \"^zeal($\" }\n"
	root := renameRoot(t, "revier", body)
	p, err := config.Rename(root, "revier", "rv", nil)
	if err != nil {
		t.Fatalf("Rename: %v, want the broken target carried over", err)
	}
	if probs := config.Problems(p); len(probs) != 1 || !strings.Contains(probs[0].Error(), "docs") {
		t.Errorf("problems = %v, want the one target still refused", probs)
	}
}

// A project file linked in from elsewhere - a dotfiles repository - is not
// moved out of it: the rename is refused, naming the file to rename by hand.
func TestRenameRefusesALinkedFile(t *testing.T) {
	root := renameRoot(t, "elsewhere", valid)
	target := config.ProjectFile(root, "elsewhere")
	if err := os.Symlink(target, config.ProjectFile(root, "revier")); err != nil {
		t.Fatal(err)
	}
	_, err := config.Rename(root, "revier", "rv", nil)
	if err == nil || !strings.Contains(err.Error(), target) {
		t.Errorf("err = %v, want the refusal naming %s", err, target)
	}
	for _, name := range []revier.ProjectName{"revier", "elsewhere"} {
		if _, err := os.Lstat(config.ProjectFile(root, name)); err != nil {
			t.Errorf("%s: %v, want it kept", name, err)
		}
	}
}

// A refused rename leaves both files as they were.
func TestRenameRefusesATakenOrInvalidName(t *testing.T) {
	root := renameRoot(t, "revier", valid)
	write(t, filepath.Join(root, "projects"), "taken.toml", "# someone else\n")
	for _, to := range []revier.ProjectName{"taken", "a/b", "has space", ""} {
		if _, err := config.Rename(root, "revier", to, nil); err == nil {
			t.Errorf("rename to %q: no error", to)
		}
	}
	if got, _ := os.ReadFile(config.ProjectFile(root, "taken")); string(got) != "# someone else\n" {
		t.Errorf("taken file = %q, want it untouched", got)
	}
	if _, err := os.Stat(config.ProjectFile(root, "revier")); err != nil {
		t.Errorf("old file: %v, want it kept", err)
	}
}
