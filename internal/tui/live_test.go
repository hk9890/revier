//go:build live

// Layer L4 for the clone Enter starts: real git, against local repositories
// only, with mise's trust list in a temporary directory.
package tui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// The clone shows git's output on the terminal it was handed, and the
// directory is there afterwards.
func TestCloneCmdClonesOnTheTerminalItIsGiven(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("MISE_STATE_DIR", t.TempDir())
	repo := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	path := filepath.Join(t.TempDir(), "demo")
	var screen bytes.Buffer
	c := &cloneCmd{project: revier.Project{Name: "demo", Path: path, GitURL: repo}}
	c.SetStdout(&screen)
	if err := c.Run(); err != nil {
		t.Fatalf("Run: %v\n%s", err, screen.String())
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Errorf("no clone at %s: %v", path, err)
	}
	if !strings.Contains(screen.String(), "cloning") {
		t.Errorf("terminal = %q, want the clone on it", screen.String())
	}
}

// The screen returns to the list when git exits, so the reason a clone
// failed has to come back in the error.
func TestCloneCmdFailureCarriesGitsReason(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	missing := filepath.Join(t.TempDir(), "absent.git")
	c := &cloneCmd{project: revier.Project{Name: "demo", Path: filepath.Join(t.TempDir(), "demo"), GitURL: missing}}
	c.SetStdout(&bytes.Buffer{})
	err := c.Run()
	if err == nil || !strings.Contains(err.Error(), "fatal:") {
		t.Fatalf("err = %v, want git's own reason in it", err)
	}
}
