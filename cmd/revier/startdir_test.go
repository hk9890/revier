package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A command keeps a working directory that is still there: `new`, `each` and
// the project of the working directory are all read from it.
func TestACommandKeepsAStartDirectoryThatIsStillThere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	t.Chdir(dir)

	leaveLostStartDir()
	if got := cwd(t); got != resolved(t, dir) {
		t.Errorf("cwd = %s, want the directory the command was run in %s", got, dir)
	}
}

// A command run in a directory that was removed under the shell leaves it.
// Every process it starts would otherwise inherit a directory that is gone,
// and git, claude and a probe script each refuse to run in one, so every
// agent would report unknown for a reason that has nothing to do with the
// command (decisions.md D92).
func TestACommandLeavesAStartDirectoryThatIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gone := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	leaveLostStartDir()
	if got := cwd(t); got != resolved(t, home) {
		t.Errorf("cwd = %s, want the home directory %s", got, home)
	}
}
