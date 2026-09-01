//go:build live

// Layer L5: a real sway on the headless wlroots backend. Each test starts its
// own compositor on a socket of its own and stops it in cleanup; nothing here
// reaches a display. Skips when sway or foot, the client it opens windows
// with, is not installed.
package sway_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/sway"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// compositor starts a headless sway and returns a Host on its socket.
func compositor(t *testing.T) *sway.Host {
	t.Helper()
	for _, bin := range []string{"sway", "swaymsg", "foot"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed; L5 needs it", bin)
		}
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "sway.sock")
	cfg := filepath.Join(dir, "config")
	if err := os.WriteFile(cfg, []byte("# headless test compositor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join(dir, "runtime")
		if err := os.MkdirAll(runtime, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sway", "-c", cfg)
	cmd.Env = append(os.Environ(),
		"WLR_BACKENDS=headless", "WLR_LIBINPUT_NO_DEVICES=1", "WLR_RENDERER=pixman",
		"SWAYSOCK="+socket, "XDG_RUNTIME_DIR="+runtime, "WAYLAND_DISPLAY=", "DISPLAY=",
	)
	logf, err := os.Create(filepath.Join(dir, "sway.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("swaymsg", "-s", socket, "exit").Run()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logf.Close()
	})
	h := &sway.Host{Socket: socket}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.Probe(context.Background()); err == nil {
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "sway.log"))
	t.Fatalf("sway did not answer on %s within 15s:\n%s", socket, log)
	return nil
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// window is a realization that opens a foot terminal with a class of its own.
func window(class string) revier.Realization {
	return revier.Realization{
		Launch: []string{"foot", "--app-id", class, "--title", class, "sh", "-c", "sleep 60"},
		Match:  revier.Match{Class: "^" + class + "$"},
	}
}

// waitFor polls Instances until the match finds a window.
func waitFor(t *testing.T, h *sway.Host, m revier.Match) revier.Instance {
	t.Helper()
	cm, err := m.Compile()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		instances, err := h.Instances(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, inst := range instances {
			if cm.Matches(inst) {
				return inst
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no window matched %+v within 10s", m)
	return revier.Instance{}
}

func TestProbeFindsSway(t *testing.T) {
	if err := compositor(t).Probe(ctx(t)); err != nil {
		t.Fatal(err)
	}
}

// The invariant every host owes the core: what Open creates, Match finds -
// and then Focus raises it and Focused reads it back.
func TestOpenThenMatchFindsItAndFocusRoundTrips(t *testing.T) {
	h := compositor(t)
	first, second := window("revier-test-a"), window("revier-test-b")
	for _, r := range []revier.Realization{first, second} {
		if _, err := h.Open(ctx(t), r); err != nil {
			t.Fatalf("Open: %v", err)
		}
	}
	a := waitFor(t, h, first.Match)
	b := waitFor(t, h, second.Match)

	for _, want := range []revier.Instance{a, b, a} {
		if err := h.Focus(ctx(t), want.Ref); err != nil {
			t.Fatalf("Focus %s: %v", want.Ref.ID, err)
		}
		got, err := h.Focused(ctx(t))
		if err != nil {
			t.Fatalf("Focused: %v", err)
		}
		if got.ID != want.Ref.ID {
			t.Fatalf("Focused = %s, want %s", got.ID, want.Ref.ID)
		}
	}
}

// The whole product against a real compositor: run-or-raise a window target,
// toggle back to a window home, raise on the third press.
func TestCoreRunOrRaiseAgainstSway(t *testing.T) {
	h := compositor(t)
	c := &core.Core{Window: h}
	home, diff := window("revier-test-home"), window("revier-test-diff")
	p, err := core.PrepareProject(revier.Project{Name: "revier", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Window: &home},
		{Name: "diff", Key: "ctrl-shift-d", Window: &diff},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Go(ctx(t), p, "home"); err != nil {
		t.Fatalf("open home: %v", err)
	}
	homeInst := waitFor(t, h, home.Match)
	if _, err := c.Go(ctx(t), p, "diff"); err != nil {
		t.Fatalf("open diff: %v", err)
	}
	diffInst := waitFor(t, h, diff.Match)
	// A new window takes focus in sway; the second press is the round trip.
	if err := h.Focus(ctx(t), diffInst.Ref); err != nil {
		t.Fatal(err)
	}
	back, err := c.Go(ctx(t), p, "diff")
	if err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if back.ID != homeInst.Ref.ID {
		t.Fatalf("toggle-back returned %s, want home %s", back.ID, homeInst.Ref.ID)
	}
	again, err := c.Go(ctx(t), p, "diff")
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.ID != diffInst.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.ID, diffInst.Ref.ID)
	}
	instances, _ := h.Instances(ctx(t))
	if len(instances) != 2 {
		t.Errorf("got %d windows, want 2: run-or-raise must not duplicate", len(instances))
	}
}

// The watcher reports a window within a second of it appearing.
func TestWatchReportsAnOpenedWindow(t *testing.T) {
	h := compositor(t)
	wctx, cancel := context.WithCancel(ctx(t))
	defer cancel()
	events, err := h.Watch(wctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let the subscription settle
	r := window("revier-test-watched")
	if _, err := h.Open(ctx(t), r); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Kind == revier.WindowOpened && ev.Instance.Class == "revier-test-watched" {
				return
			}
		case <-deadline:
			t.Fatal(fmt.Sprintf("no open event for %s within 3s", "revier-test-watched"))
		}
	}
}
