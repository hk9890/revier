package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A directory with more entries than the cap is listed as far as the cap and
// says it was cut. Twenty lines is what the pane shows; reading a hundred
// thousand names to render twenty of them is what the cap prevents.
func TestWalkTreeStopsAtTheLineCap(t *testing.T) {
	dir := t.TempDir()
	for i := range treeMaxLines * 2 {
		if err := os.WriteFile(filepath.Join(dir, string(rune('a'+i%26))+string(rune('0'+i/26))), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lines := walkTree(dir)
	if len(lines) != treeMaxLines+1 {
		t.Fatalf("got %d lines, want %d and a marker", len(lines), treeMaxLines)
	}
	if last := lines[len(lines)-1]; last != "..." {
		t.Errorf("last line = %q, want the truncation marker", last)
	}
}

func TestWalkTreeOfAnUnreadableDirectoryIsEmpty(t *testing.T) {
	if lines := walkTree(filepath.Join(t.TempDir(), "absent")); len(lines) != 0 {
		t.Errorf("got %v, want no lines", lines)
	}
}

// The cache is what makes the pane cheap enough to rebuild on every keypress.
func TestTreeIsCachedPerPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "first"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Model{}
	before := strings.Join(m.treeFor(dir), "\n")

	if err := os.WriteFile(filepath.Join(dir, "second"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if after := strings.Join(m.treeFor(dir), "\n"); after != before {
		t.Errorf("second call re-walked: %q -> %q", before, after)
	}
}
