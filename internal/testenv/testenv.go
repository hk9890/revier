// Package testenv keeps a test binary off the user's own revier files: the
// configuration, the state, the ssh configuration a link is checked
// against, and kitty's configuration, which `revier term` writes. A package whose code reaches any of them calls Run from its
// TestMain; see docs/TESTING.md. NoTools keeps one test off the tools on the
// PATH.
package testenv

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noHosts is the scratch configuration: it names no host, so a test that
// runs a command without its own configuration reaches no runtime and no
// window. The key binder and key writer do not read it.
const noHosts = "[hosts]\nruntime = [\"none\"]\nwindow = [\"none\"]\n"

// Run runs the tests with REVIER_CONFIG_HOME, REVIER_STATE_HOME,
// REVIER_SSH_CONFIG and KITTY_CONFIG_DIRECTORY pointing into one scratch
// directory, and returns the exit code for os.Exit. kitty's variable is the
// one of another tool set here: the user's kitty directory is often a link
// into a dotfiles repository, and a test that wrote through it would change
// a tracked file. A test sets its own with t.Setenv, which wins. One that
// forgets writes into the scratch directory, and Run fails the binary and
// names what was written.
func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "revier-test-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	config, err := prepare(dir)
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("REVIER_CONFIG_HOME", config)
	_ = os.Setenv("REVIER_STATE_HOME", filepath.Join(dir, "state"))
	_ = os.Setenv("REVIER_SSH_CONFIG", filepath.Join(dir, "ssh_config"))
	_ = os.Setenv("KITTY_CONFIG_DIRECTORY", filepath.Join(dir, "kitty"))

	code := m.Run()
	if written := writtenSince(dir, config); len(written) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: a test wrote outside its own roots; give it a t.Setenv of REVIER_CONFIG_HOME, REVIER_STATE_HOME, REVIER_SSH_CONFIG or KITTY_CONFIG_DIRECTORY:\n\t%s\n", strings.Join(written, "\n\t"))
		return 1
	}
	return code
}

// NoTools empties PATH for the test, so the code under test finds no tool to
// start: the test starts no process, and mise's trust list is left as it was.
func NoTools(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// prepare makes the scratch configuration under dir and returns its root.
func prepare(dir string) (config string, err error) {
	config = filepath.Join(dir, "config")
	if err := os.Mkdir(config, 0o755); err != nil {
		return "", err
	}
	return config, os.WriteFile(filepath.Join(config, "config.toml"), []byte(noHosts), 0o644)
}

// writtenSince lists every path under dir that prepare did not make, and
// config/config.toml where its content changed or it is gone.
func writtenSince(dir, config string) []string {
	file := filepath.Join(config, "config.toml")
	var written []string
	if b, err := os.ReadFile(file); err != nil || string(b) != noHosts {
		written = append(written, file)
	}
	_ = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		switch {
		case err != nil:
			written = append(written, fmt.Sprintf("%s: %v", path, err))
		case path == dir, path == config, path == file:
		default:
			written = append(written, path)
		}
		return nil
	})
	return written
}
