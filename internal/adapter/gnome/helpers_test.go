package gnome_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/gnome"
)

// wctl stands in for the wctl binary: it records the arguments of every
// call, joined, and answers with what reply returns for them. With no reply
// every call succeeds and prints nothing.
type wctl struct {
	calls []string
	reply func(args string) ([]byte, error)
}

// host is a GNOME host that runs w in place of wctl.
func (w *wctl) host() *gnome.Host {
	h := &gnome.Host{}
	h.SetRunner(func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		w.calls = append(w.calls, joined)
		if w.reply == nil {
			return nil, nil
		}
		return w.reply(joined)
	})
	return h
}

// refused is what the host gets from a wctl that exits non-zero: the command,
// the exit status, and what wctl wrote to its standard error.
func refused(args string, code int, stderr string) error {
	return fmt.Errorf("wctl %s: exit status %d: %s", args, code, stderr)
}

// onGNOME puts a wctl on PATH and names GNOME as the desktop, so Probe gets
// as far as asking the recorder.
func onGNOME(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wctl"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
}

// versionReply answers `version --json` with doc, failing with code when it
// is not zero, as wctl does; every other call succeeds and prints nothing.
func versionReply(doc string, code int) func(string) ([]byte, error) {
	return func(args string) ([]byte, error) {
		if args != "version --json" {
			return nil, nil
		}
		if code != 0 {
			return []byte(doc), refused(args, code, "")
		}
		return []byte(doc), nil
	}
}
