package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/fsutil"
)

// A write replaces the file's content and mode, and leaves nothing beside it:
// the temporary file is renamed over the path, not left for a listing to find.
func TestWriteFileReplacesTheFileAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Errorf("content = %q, %v; want the new content", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want the file alone", len(entries))
	}
}

// A link is written through, keeps the mode of the file it points at, and
// stays a link; a link to nothing is refused and stays as it was.
func TestWriteThroughKeepsTheLinkAndRefusesADanglingOne(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.toml")
	if err := os.WriteFile(real, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	got, err := fsutil.WriteThrough(path, []byte("new"))
	if err != nil || got != real {
		t.Fatalf("WriteThrough = %q, %v; want %q", got, err, real)
	}
	if info, _ := os.Lstat(path); info.Mode()&os.ModeSymlink == 0 {
		t.Error("the link was replaced")
	}
	if info, _ := os.Stat(real); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 kept", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Errorf("content = %q", b)
	}

	dangling := filepath.Join(dir, "dangling.toml")
	if err := os.Symlink(filepath.Join(dir, "gone"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := fsutil.WriteThrough(dangling, []byte("x")); err == nil {
		t.Error("WriteThrough wrote beside a dangling link")
	}
	if info, _ := os.Lstat(dangling); info.Mode()&os.ModeSymlink == 0 {
		t.Error("the dangling link was replaced")
	}
}

// A new file gets 0644, in a directory made for it.
func TestWriteThroughMakesANewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b.conf")
	if _, err := fsutil.WriteThrough(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("stat = %v, %v; want a 0644 file", info, err)
	}
}
