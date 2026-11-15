//go:build live

package hc_test

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

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/shell"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// world gives the test a private hc: its config, state, runtime directory and
// socket in a scratch directory, set in this process's environment, which
// the core Open starts takes. The user's own core and service manager are
// never reached. A terminal's shell is sh, not the user's: their shell's
// startup files would run in it. No core runs yet; the test's first Open starts it, and the
// cleanup stops it and its holder.
func world(t *testing.T) *hc.Host {
	t.Helper()
	if _, err := hc.Program(); err != nil {
		t.Skip("herdr-core is not installed; L4 needs it")
	}
	// Named after the test, cut short: a Unix socket path longer than 107
	// bytes cannot be bound, and t.TempDir carries the test's whole name.
	dir, err := os.MkdirTemp("", "rvc-"+testenv.ShortName(t)+"-")
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(dir, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock")
	// No service manager is reached: the core starts detached, off the
	// user's own manager, as Open starts it where there is none. revier's
	// state directory holds what the core's start says.
	testenv.NoServiceManager(t, run)
	for name, value := range map[string]string{
		"XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "HC_SOCKET": sock, "SHELL": "/bin/sh",
		"HC_SESSION": "", "HC_TERMINAL_ID": "", "HC_TERMINAL_SOCKET": "", "HC_SCOPE": "", "REVIER_STATE_HOME": filepath.Join(dir, "revier"),
	} {
		t.Setenv(name, value)
	}
	h := &hc.Host{Socket: sock}
	t.Cleanup(func() {
		stop(h)
		_ = os.RemoveAll(dir)
	})
	return h
}

// stop stops the core, which ends every program it runs, and then makes
// sure the core and its holder are gone: a pid still alive at the deadline
// is killed, and one gone by then is left alone, as its number may be
// another process's by now.
func stop(h *hc.Host) {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var status struct {
		PID     int `json:"pid"`
		Holders []struct {
			PID int `json:"pid"`
		} `json:"holders"`
	}
	if err := h.Request(c, "server.status", struct{}{}, &status); err != nil {
		return
	}
	pids := []int{status.PID}
	for _, holder := range status.Holders {
		pids = append(pids, holder.PID)
	}
	_ = h.Request(c, "server.stop", struct{}{}, nil)
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// waitFor polls the listing until ok accepts it: hc reads a terminal's
// foreground process about twice a second.
func waitFor(t *testing.T, h *hc.Host, ok func([]revier.Instance) bool) []revier.Instance {
	t.Helper()
	c := ctx(t)
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := h.Instances(c)
		if err != nil {
			t.Fatalf("Instances: %v", err)
		}
		if ok(got) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("instances never settled: %+v", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func press(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, nil)
	return res, err
}

// listing is the instance's panels as hc lists them, in its order.
func listing(t *testing.T, h *hc.Host, ref revier.TargetRef) []revier.Panel {
	t.Helper()
	instances, err := h.Instances(ctx(t))
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	for _, in := range instances {
		if in.Ref == ref {
			return in.Panels
		}
	}
	t.Fatalf("no instance %s: %+v", ref.ID, instances)
	return nil
}

// waitForRecords waits until the states of the instance's records, live or
// finished, sorted and joined by a space, are want: "" for none.
func waitForRecords(t *testing.T, c context.Context, h *hc.Host, ref revier.TargetRef, want string) {
	t.Helper()
	_, id, _ := strings.Cut(ref.ID, "/")
	deadline := time.Now().Add(10 * time.Second)
	for {
		var list struct {
			Terminals []struct {
				State string `json:"state"`
			} `json:"terminals"`
		}
		params := map[string]any{"all": true, "labels": map[string]string{"revier.instance": id}}
		if err := h.Request(c, "terminal.list", params, &list); err != nil {
			t.Fatalf("terminal.list: %v", err)
		}
		var states []string
		for _, r := range list.Terminals {
			states = append(states, r.State)
		}
		slices.Sort(states)
		if got := strings.Join(states, " "); got == want {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("records of %s = %q, want %q", ref.ID, got, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// windowCmd runs argv as a view's window runs it, in a terminal script
// gives it.
func windowCmd(script string, argv []string) *exec.Cmd {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shell.Quote(a)
	}
	return exec.Command(script, "-qec", strings.Join(quoted, " "), "/dev/null")
}
