//go:build live

// Layer L4 for checkout: real git against repositories in temporary
// directories. Nothing reaches the network - every origin is a local bare
// repository - and mise's trust list is redirected to a temporary directory.
package checkout_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/pkg/revier"
)

// bare makes a bare repository with one commit and returns its path.
func bare(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("MISE_STATE_DIR", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	work, repo := t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "-q", "--bare", repo)
	git(t, "", "clone", "-q", repo, work)
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "README")
	git(t, work, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "one")
	git(t, work, "push", "-q", "origin", "HEAD")
	return repo
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// A missing directory with a git_url comes back as a clone of it, with git's
// own output on the writer the user is watching.
func TestEnsureClonesAMissingDirectory(t *testing.T) {
	repo := bare(t)
	path := filepath.Join(t.TempDir(), "nested", "demo")
	var out bytes.Buffer

	cloned, err := checkout.Ensure(revier.Project{Name: "demo", Path: path, GitURL: repo}, &out)
	if err != nil || !cloned {
		t.Fatalf("Ensure = %v, %v\n%s", cloned, err, out.String())
	}
	if _, err := os.Stat(filepath.Join(path, "README")); err != nil {
		t.Errorf("the clone has no README: %v", err)
	}
	if !strings.Contains(out.String(), "cloning") {
		t.Errorf("output = %q, want the clone announced", out.String())
	}
}

// A directory that is there is left alone: no clone, no output.
func TestEnsureLeavesAnExistingDirectoryAlone(t *testing.T) {
	var out bytes.Buffer
	cloned, err := checkout.Ensure(revier.Project{Name: "demo", Path: t.TempDir(), GitURL: "/nowhere.git"}, &out)
	if err != nil || cloned || out.Len() > 0 {
		t.Fatalf("Ensure = %v, %v, %q; want nothing done", cloned, err, out.String())
	}
}

// With no git_url there is nothing to clone, and the error says which field
// is missing.
func TestEnsureWithoutGitURLNamesTheField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone")
	_, err := checkout.Ensure(revier.Project{Name: "demo", Path: path}, &bytes.Buffer{})
	if !errors.Is(err, checkout.ErrNoGitURL) || !strings.Contains(err.Error(), "git_url") {
		t.Fatalf("err = %v, want ErrNoGitURL naming git_url", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("nothing may be created when there is nothing to clone")
	}
}

func TestEnsureReportsAFailedClone(t *testing.T) {
	bare(t)
	path := filepath.Join(t.TempDir(), "demo")
	_, err := checkout.Ensure(revier.Project{Name: "demo", Path: path, GitURL: filepath.Join(t.TempDir(), "absent.git")}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("err = %v, want the clone failure", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed clone must not leave a directory that looks like a checkout")
	}
}

func TestOriginReadsTheRemote(t *testing.T) {
	repo := bare(t)
	work := filepath.Join(t.TempDir(), "w")
	git(t, "", "clone", "-q", repo, work)
	if got := checkout.Origin(work); got != repo {
		t.Errorf("Origin = %q, want %q", got, repo)
	}
	if got := checkout.Origin(t.TempDir()); got != "" {
		t.Errorf("Origin of a directory with no repository = %q, want empty", got)
	}
}

// A remote with a token in it is not copied into a project file.
func TestOriginDropsARemoteWithCredentials(t *testing.T) {
	bare(t)
	work := t.TempDir()
	git(t, work, "init", "-q")
	git(t, work, "remote", "add", "origin", "https://user:s3cret@example.invalid/a.git")
	if got := checkout.Origin(work); got != "" {
		t.Errorf("Origin = %q, want the credential URL dropped", got)
	}
}
