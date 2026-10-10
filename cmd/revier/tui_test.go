package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// The surface opens on the project of the directory it was started in, and
// then runs in the home directory, so what it starts later never inherits a
// directory that may be removed under it.
func TestTUIStartOpensOnItsDirectoryThenLeavesForHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	worktree := filepath.Join(project, "worktree")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktree)

	start, popup := tuiStart(tuiApp(project))
	if start != "demo" || popup {
		t.Errorf("start = %q, popup = %v; want demo, false", start, popup)
	}
	if got := cwd(t); got != resolved(t, home) {
		t.Errorf("cwd = %s, want the home directory %s", got, home)
	}
}

// The popup takes its project from the environment, and leaves its directory
// as the surface does.
func TestTUIStartInThePopupLeavesForHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(core.PopupEnv, "demo")
	t.Chdir(t.TempDir())

	start, popup := tuiStart(tuiApp(t.TempDir()))
	if start != "demo" || !popup {
		t.Errorf("start = %q, popup = %v; want demo, true", start, popup)
	}
	if _, set := os.LookupEnv(core.PopupEnv); set {
		t.Errorf("%s is still set, so what the popup launches takes itself for the popup", core.PopupEnv)
	}
	if got := cwd(t); got != resolved(t, home) {
		t.Errorf("cwd = %s, want the home directory %s", got, home)
	}
}

// A home directory that does not exist leaves the surface in the root, which
// cannot be removed under it.
func TestTUIStartWithoutAHomeLeavesForTheRoot(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "gone"))
	t.Chdir(t.TempDir())

	tuiStart(tuiApp(t.TempDir()))
	if got := cwd(t); got != "/" {
		t.Errorf("cwd = %s, want /", got)
	}
}
