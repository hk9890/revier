// What the host's Probe says of the herdr it finds, against a caller or a
// socket of the test's own in place of the server, and an empty herdr on
// PATH. No herdr runs, so the default layer runs it.
package herdr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/herdr"
)

// A server older than revier works with fails the probe, so host selection
// moves on to the next runtime instead of failing at every keypress.
func TestProbeRefusesAServerOlderThanTheOldestSupported(t *testing.T) {
	fakeHerdrOnPath(t)
	for version, wantErr := range map[string]bool{"0.9.4": true, "0.9.5": false, "0.10.0": false} {
		h := &herdr.Host{}
		h.SetCaller(func(_ context.Context, _ string, _ any) (int, []byte, error) {
			return 9, []byte(`{"version":"` + version + `"}`), nil
		})
		err := h.Probe(context.Background())
		if wantErr != (err != nil) {
			t.Errorf("%s: Probe = %v, want an error %v", version, err, wantErr)
		}
		if err != nil && (!strings.Contains(err.Error(), version) || !strings.Contains(err.Error(), "0.9.5")) {
			t.Errorf("%s: Probe = %v, want both versions named", version, err)
		}
	}
}

// With no server to ask, the binary is enough: Open checks the server it
// starts.
func TestProbeNeedsNoRunningServer(t *testing.T) {
	fakeHerdrOnPath(t)
	h := &herdr.Host{Socket: filepath.Join(t.TempDir(), "none.sock")}
	if err := h.Probe(context.Background()); err != nil {
		t.Errorf("Probe = %v, want herdr usable", err)
	}
}

// A server that listens and fails the ping fails the probe: Open starts a
// server only where none listens, so it would fail on this one at every
// keypress.
func TestProbeRefusesAServerThatFailsThePing(t *testing.T) {
	fakeHerdrOnPath(t)
	sock := serve(t, func(map[string]any) string {
		return `{"id":"revier","error":{"code":"unknown_method","message":"no such method"}}`
	})
	h := &herdr.Host{Socket: sock}
	if err := h.Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "unknown_method") {
		t.Errorf("Probe = %v, want the failed ping named", err)
	}
}

// fakeHerdrOnPath puts an empty herdr alone on PATH.
func fakeHerdrOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "herdr"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
