// Package testenv keeps a test binary off the user's own revier files: the
// configuration, the state, and the ssh configuration a link is checked
// against. A package whose code reaches any of them calls Run from its
// TestMain; see docs/TESTING.md.
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
// runs a command without its own configuration reaches no desktop.
const noHosts = "[hosts]\nruntime = [\"none\"]\nwindow = [\"none\"]\n"

// Run runs the tests with REVIER_CONFIG_HOME, REVIER_STATE_HOME and
// REVIER_SSH_CONFIG pointing into one scratch directory, and returns the exit
// code for os.Exit. A test sets its own with t.Setenv, which wins. One that
// forgets writes into the scratch directory, and Run fails the binary and
// names what was written.
func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "revier-test-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	config := filepath.Join(dir, "config")
	if err := os.Mkdir(config, 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte(noHosts), 0o644); err != nil {
		panic(err)
	}
	_ = os.Setenv("REVIER_CONFIG_HOME", config)
	_ = os.Setenv("REVIER_STATE_HOME", filepath.Join(dir, "state"))
	_ = os.Setenv("REVIER_SSH_CONFIG", filepath.Join(dir, "ssh_config"))

	code := m.Run()
	if written := writtenSince(dir, config); len(written) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: a test wrote outside its own roots; give it a t.Setenv of REVIER_CONFIG_HOME, REVIER_STATE_HOME or REVIER_SSH_CONFIG:\n\t%s\n", strings.Join(written, "\n\t"))
		return 1
	}
	return code
}

// writtenSince lists every path under dir that Run did not make itself, with
// config/config.toml listed only where its content changed.
func writtenSince(dir, config string) []string {
	var written []string
	_ = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		switch {
		case err != nil:
			written = append(written, fmt.Sprintf("%s: %v", path, err))
		case path == dir, path == config:
		case path == filepath.Join(config, "config.toml"):
			if b, err := os.ReadFile(path); err != nil || string(b) != noHosts {
				written = append(written, path)
			}
		default:
			written = append(written, path)
		}
		return nil
	})
	return written
}
