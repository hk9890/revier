package popupswitch_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/popupswitch"
)

// listen binds a socket in a directory of the test's own and closes it at
// the end.
func listen(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "popup.sock")
	ln, err := popupswitch.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, path
}

// await runs Await on ln and reports its answer, or fails the test when none
// comes in time.
func await(t *testing.T, ln net.Listener) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- popupswitch.Await(ln) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Await returned nothing in 5s")
		return nil
	}
}

// The socket is in the runtime directory, and there is none where the
// environment names no absolute one: a relative one resolves where every user
// can enter.
func TestPathIsInTheRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if path, err := popupswitch.Path(); err != nil || path != "/run/user/1000/revier-popup.sock" {
		t.Errorf("Path = %q, %v; want /run/user/1000/revier-popup.sock", path, err)
	}
	for _, dir := range []string{"", "run/user/1000"} {
		t.Setenv("XDG_RUNTIME_DIR", dir)
		if path, err := popupswitch.Path(); !errors.Is(err, popupswitch.ErrNoRuntimeDir) {
			t.Errorf("XDG_RUNTIME_DIR=%q: Path = %q, %v; want ErrNoRuntimeDir", dir, path, err)
		}
	}
}

// A press says switch, and the wait for it returns. The socket is the user's
// alone, and closing it removes its file.
func TestASwitchReachesTheSurfaceThatListens(t *testing.T) {
	ln, path := listen(t)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket = %v, %v; want mode 0600", info, err)
	}
	if err := popupswitch.Switch(t.Context(), path); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if err := await(t, ln); err != nil {
		t.Fatalf("Await: %v", err)
	}
	_ = ln.Close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file after the close: %v, want it removed", err)
	}
}

// A connection that says anything but switch, or nothing, is no press: the
// wait goes on to the next one.
func TestAWaitSkipsWhatIsNoSwitch(t *testing.T) {
	ln, path := listen(t)
	for _, said := range []string{"", "hello\n"} {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write([]byte(said))
		_ = conn.Close()
	}
	if err := popupswitch.Switch(t.Context(), path); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if err := await(t, ln); err != nil {
		t.Fatalf("Await: %v", err)
	}
	_ = ln.Close()
	if err := await(t, ln); err == nil {
		t.Error("Await on a closed socket returned no error")
	}
}

// With no surface listening a press fails, so the caller raises the popup
// in its place.
func TestASwitchWithNoSurfaceFails(t *testing.T) {
	if err := popupswitch.Switch(t.Context(), filepath.Join(t.TempDir(), "popup.sock")); err == nil {
		t.Error("Switch with no socket returned no error")
	}
}

// The file a killed surface left is taken over by the next; a socket a
// surface still answers on is not, so two popups never take each other's
// presses.
func TestListenTakesOverAStaleSocketAndNoLiveOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "popup.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()
	ln, err := popupswitch.Listen(path)
	if err != nil {
		t.Fatalf("Listen over a stale socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	if second, err := popupswitch.Listen(path); err == nil {
		_ = second.Close()
		t.Fatal("a second Listen took over a live socket")
	}
	if err := popupswitch.Switch(t.Context(), path); err != nil {
		t.Fatalf("Switch after the refused Listen: %v", err)
	}
	if err := await(t, ln); err != nil {
		t.Fatalf("Await: %v", err)
	}
}
