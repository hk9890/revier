//go:build live

// Layer L4 for the commands that write a project file or clone a checkout:
// the real command paths, a real tmux, real git. Every origin is a local bare
// repository, so nothing reaches the network, and mise's trust list is
// redirected to a temporary directory.
package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

// fileScratch is scratch plus what these commands touch besides tmux: git,
// a mise trust list of their own, and a `claude` that is a sleep, because the
// template's workspace starts one and the real one must not start here.
func fileScratch(t *testing.T) (root, workdir string) {
	t.Helper()
	workdir = scratch(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("MISE_STATE_DIR", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return os.Getenv("REVIER_CONFIG_HOME"), workdir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// bareRepo is a bare repository with one commit in it.
func bareRepo(t *testing.T) string {
	t.Helper()
	work, repo := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, "", "init", "-q", "--bare", repo)
	gitIn(t, "", "clone", "-q", repo, work)
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "README")
	gitIn(t, work, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "one")
	gitIn(t, work, "push", "-q", "origin", "HEAD")
	return repo
}

// A checkout with an origin: `revier new` in it writes a project named after
// the directory, with the origin recorded, and the file loads.
func TestNewWritesAProjectForTheDirectory(t *testing.T) {
	root, _ := fileScratch(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	gitIn(t, "", "clone", "-q", bareRepo(t), dir)
	chdir(t, dir)

	out := capture(t, "new")
	file := filepath.Join(root, "projects", "fresh.toml")
	if !strings.Contains(out, file) {
		t.Errorf("new printed %q, want the file it wrote", out)
	}
	p := config.LoadProject(file, nil)
	if p.Path != dir || p.GitURL == "" {
		t.Errorf("project = %+v, want the directory and its origin", p.Project)
	}
}

// A directory that is already a project would become two projects fighting
// over it.
func TestNewRefusesADirectoryThatIsAlreadyAProject(t *testing.T) {
	_, workdir := fileScratch(t)
	chdir(t, workdir)
	err := run(io.Discard, []string{"new", "again"})
	if err == nil || !strings.Contains(err.Error(), `already project "demo"`) {
		t.Fatalf("err = %v, want a refusal naming the project", err)
	}
}

// `revier open <unknown>` in a directory is `revier new <unknown>` and then
// the open, in one step.
func TestOpenAnUnknownNameCreatesTheProjectAndOpensIt(t *testing.T) {
	root, _ := fileScratch(t)
	dir := t.TempDir()
	chdir(t, dir)

	out := capture(t, "open", "brandnew")
	if _, err := os.Stat(filepath.Join(root, "projects", "brandnew.toml")); err != nil {
		t.Fatalf("no project file written: %v\n%s", err, out)
	}
	if !strings.Contains(out, "brandnew:") {
		t.Errorf("open printed %q, want the workspace it opened", out)
	}
	if list := capture(t, "list"); !strings.Contains(list, "brandnew") || !strings.Contains(list, "*home") {
		t.Errorf("list = %q, want brandnew running", list)
	}
}

// A project whose directory is not on this machine is cloned from its
// git_url, and then opened.
func TestOpenClonesAMissingDirectory(t *testing.T) {
	root, workdir := fileScratch(t)
	path := filepath.Join(t.TempDir(), "checkouts", "gone")
	body := strings.ReplaceAll(projectTOML, "%PATH%", path)
	body = "git_url = \"" + bareRepo(t) + "\"\n" + body
	if err := os.WriteFile(filepath.Join(root, "projects", "gone.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, workdir)

	out := capture(t, "open", "gone")
	if _, err := os.Stat(filepath.Join(path, "README")); err != nil {
		t.Fatalf("the directory was not cloned: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "gone:") {
		t.Errorf("open printed %q, want the workspace opened after the clone", out)
	}
}

// Without a git_url there is nothing to clone from, and the message says
// which field would have made it possible.
func TestOpenAMissingDirectoryWithoutGitURLNamesTheField(t *testing.T) {
	root, _ := fileScratch(t)
	body := strings.ReplaceAll(projectTOML, "%PATH%", filepath.Join(t.TempDir(), "gone"))
	if err := os.WriteFile(filepath.Join(root, "projects", "gone.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(io.Discard, []string{"open", "gone"})
	if err == nil || !strings.Contains(err.Error(), "git_url") {
		t.Fatalf("err = %v, want a failure naming git_url", err)
	}
}
