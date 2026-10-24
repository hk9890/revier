//go:build live

package herdr_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/hk9890/revier/internal/adapter/herdr"
	"github.com/hk9890/revier/pkg/revier"
)

// server starts a herdr server of this test's own, headless, with its config,
// state and socket in a scratch directory, and stops it when the test ends.
// Nothing attaches a client, and the user's own herdr is never reached: the
// environment that names it is left out of the server's. A pane's shell is
// sh, not the user's: their shell's startup files would run in every pane,
// and could attach their own tmux or hold a pane's foreground.
func server(t *testing.T) *herdr.Host {
	t.Helper()
	bin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr not installed; L4 needs it")
	}
	// Named after the test, cut short: a Unix socket path longer than 107
	// bytes cannot be bound, and t.TempDir carries the test's whole name.
	dir, err := os.MkdirTemp("", "rvh-"+shortName(t)+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	cmd := exec.Command(bin, "server")
	cmd.Env = append(scrubbed(os.Environ()),
		"XDG_CONFIG_HOME="+dir, "XDG_STATE_HOME="+dir, "HERDR_SOCKET_PATH="+sock, "SHELL=/bin/sh")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start herdr server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})

	h := &herdr.Host{Socket: sock}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ping, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := h.Request(ping, "ping", struct{}{}, nil)
		cancel()
		if err == nil {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("herdr server on %s never answered ping: %v", sock, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// herdrEnv names what points herdr at another server or tells it it runs
// inside one.
var herdrEnv = []string{
	"HERDR_ENV", "HERDR_SESSION", "HERDR_SOCKET_PATH", "HERDR_CONFIG_PATH",
	"HERDR_PANE_ID", "HERDR_TAB_ID", "HERDR_WORKSPACE_ID",
}

// scrubbed is the environment without herdrEnv, and without the directories
// a test server is given its own of.
func scrubbed(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(herdrEnv, name) || name == "XDG_CONFIG_HOME" || name == "XDG_STATE_HOME" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// shortName is the test's name with its letters and digits alone, at most 40
// of them: enough to tell which test left a socket behind.
func shortName(t *testing.T) string {
	name := strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return -1
	}, strings.TrimPrefix(t.Name(), "Test"))
	return name[:min(len(name), 40)]
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// waitFor polls the listing until ok accepts it: a pane's process starts
// after the request that made the pane returns.
func waitFor(t *testing.T, h *herdr.Host, ok func([]revier.Instance) bool) {
	t.Helper()
	c := ctx(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		if ok(got) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances never settled: %+v", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
