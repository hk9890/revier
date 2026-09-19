//go:build live

// Layer L4 for the surface's working directory: real git and real scripts,
// started after the directory the surface was started in is gone.
package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/adapter/execprobe"
	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/pkg/revier"
)

// inHome fails a script that does not run in the home directory. A script run
// in a removed directory fails it too, as git and claude fail there.
const inHome = `[ "$(pwd -P)" = "$(cd "$HOME" && pwd -P)" ] || exit 1`

// startedInRemovedWorktree starts the surface in a worktree of a project, as
// the user does, and then removes the worktree, as the user does once its
// branch is merged. The surface keeps running.
func startedInRemovedWorktree(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	worktree := filepath.Join(project, "worktree")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktree)
	if start, _ := tuiStart(tuiApp(project)); start != "demo" {
		t.Fatalf("start = %q, want demo", start)
	}
	if err := os.Remove(worktree); err != nil {
		t.Fatal(err)
	}
}

// onPath puts an executable named name first on PATH that runs body.
func onPath(t *testing.T, name, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// git cannot check out a clone from a removed directory, so without leaving
// it the surface could not clone a missing checkout until it was restarted.
func TestTUIClonesAfterItsStartDirectoryWasRemoved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	origin := filepath.Join(t.TempDir(), "origin")
	if err := os.Mkdir(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "README"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "README"},
		{"-c", "user.name=revier", "-c", "user.email=revier@example.invalid", "commit", "-q", "-m", "demo"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", origin}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	startedInRemovedWorktree(t)

	missing := filepath.Join(t.TempDir(), "checkout")
	cloned, err := checkout.Ensure(revier.Project{Name: "demo", Path: missing, GitURL: origin}, io.Discard)
	if err != nil || !cloned {
		t.Fatalf("Ensure = %v, %v; want a clone", cloned, err)
	}
	if _, err := os.Stat(filepath.Join(missing, "README")); err != nil {
		t.Errorf("the clone checked out nothing: %v", err)
	}
}

// The claude listing refuses to run in a removed directory, which blanked
// every agent of a surface started in a worktree once it was removed.
func TestTUIListsClaudeAgentsAfterItsStartDirectoryWasRemoved(t *testing.T) {
	onPath(t, "claude", inHome+"\n"+`echo '[{"pid":101,"status":"busy"}]'`)
	startedInRemovedWorktree(t)

	p := &claude.Probe{SessionsDir: t.TempDir()}
	got, err := p.Inspect(context.Background(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Status != revier.StatusRunning {
		t.Errorf("status = %v, want running", got.Status)
	}
}

// A [[probe]] script runs in the surface's directory; it must be one that
// still exists.
func TestTUIRunsAProbeAfterItsStartDirectoryWasRemoved(t *testing.T) {
	onPath(t, "aider-probe", inHome+"\n"+`echo '{"status":"idle"}'`)
	startedInRemovedWorktree(t)

	p := execprobe.New("aider", "aider-probe")
	got, err := p.Inspect(context.Background(), revier.Panel{Command: []string{"aider"}})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Status != revier.StatusIdle {
		t.Errorf("status = %v, want idle", got.Status)
	}
}
