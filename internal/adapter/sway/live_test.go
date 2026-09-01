//go:build live

// Layer L5: a real sway on the headless wlroots backend. Each test starts its
// own compositor on a socket of its own and stops it in cleanup; nothing here
// reaches a display. Skips when sway or foot, the client it opens windows
// with, is not installed.
package sway_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// A runtime dir of the test's own, so the compositor's Wayland socket is
	// the only one in it and needs no log parsing to find.
	runtime := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(runtime, 0o700); err != nil {
		t.Fatal(err)
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
	// Clients reach the compositor through the Wayland socket in the runtime
	// dir; both go to every client the host launches.
	h := &sway.Host{Socket: socket}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		display := waylandSocket(runtime)
		if display != "" && h.Probe(context.Background()) == nil {
			h.Env = []string{"WAYLAND_DISPLAY=" + display, "XDG_RUNTIME_DIR=" + runtime}
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "sway.log"))
	t.Fatalf("sway did not open a display and answer on %s within 15s:\n%s", socket, log)
	return nil
}

// waylandSocket names the display socket sway created in the runtime dir.
func waylandSocket(runtime string) string {
	entries, err := os.ReadDir(runtime)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "wayland-") && !strings.HasSuffix(e.Name(), ".lock") {
			return e.Name()
		}
	}
	return ""
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
			t.Fatalf("no open event for %s within 3s", "revier-test-watched")
		}
	}
}

// Claim-on-appear against a real compositor: a window that appears within the
// claim window after a launch, matching no declared target, is claimed on the
// polling path and on the event path; one appearing with no recent launch is
// not.
func TestClaimOnAppearAgainstSway(t *testing.T) {
	h := compositor(t)
	c := &core.Core{Window: h}
	home := window("revier-test-home")
	p, err := core.PrepareProject(revier.Project{Name: "revier", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Window: &home},
	}})
	if err != nil {
		t.Fatal(err)
	}
	projects := []core.Project{p}

	before, err := c.Survey(ctx(t), projects)
	if err != nil {
		t.Fatal(err)
	}
	// The launch: xdg-open from the workspace, say. The stray window that
	// appears is not any declared target.
	launchedAt := time.Now()
	stray := window("revier-test-stray")
	if _, err := h.Open(ctx(t), stray); err != nil {
		t.Fatal(err)
	}
	strayInst := waitFor(t, h, stray.Match)
	after, err := c.Survey(ctx(t), projects)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := c.Claim(before.Windows, after.Windows, launchedAt, time.Now(), projects)
	if !ok || ref.ID != strayInst.Ref.ID {
		t.Fatalf("Claim = %+v, %v; want the stray window %s", ref, ok, strayInst.Ref.ID)
	}
	if _, ok := c.Claim(before.Windows, after.Windows, launchedAt.Add(-time.Minute), time.Now(), projects); ok {
		t.Error("a window appearing a minute after the launch must not be claimed")
	}
	if _, ok := c.Claim(after.Windows, after.Windows, time.Now(), time.Now(), projects); ok {
		t.Error("with no new window there is nothing to claim")
	}

	// The event path: the watcher reports the next window, and the same
	// bounds decide.
	wctx, cancel := context.WithCancel(ctx(t))
	defer cancel()
	events, err := h.Watch(wctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	launchedAt = time.Now()
	if _, err := h.Open(ctx(t), window("revier-test-stray-2")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Kind != revier.WindowOpened {
				continue
			}
			if ev.Instance.Class != "revier-test-stray-2" {
				continue
			}
			if !c.ClaimEvent(ev.Instance, launchedAt, time.Now(), projects) {
				t.Fatal("the window that appeared after the launch should be claimed")
			}
			if c.ClaimEvent(ev.Instance, launchedAt.Add(-time.Minute), time.Now(), projects) {
				t.Error("a stale launch must not claim")
			}
			return
		case <-deadline:
			t.Fatal("no open event within 3s")
		}
	}
}
