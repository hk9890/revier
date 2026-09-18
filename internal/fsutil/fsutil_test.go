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
