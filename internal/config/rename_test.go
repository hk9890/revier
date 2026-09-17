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
