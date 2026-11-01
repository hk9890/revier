//go:build live

// Layer L4: the kitten the host runs is a real process, a script first on
// PATH, and so are kitty and systemd-run. No kitty runs.
package kitty_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/hk9890/revier/internal/adapter/kitty"
)

// kitten runs in a process group of its own, so the ctrl+c a terminal sends
// to revier's group, which cancels a shutdown or a restore after its step,
// does not end the call that step makes.
func TestKittenRunsInAProcessGroupOfItsOwn(t *testing.T) {
	bin := t.TempDir()
	file := filepath.Join(bin, "pgrp")
	script := "#!/bin/sh\ncut -d' ' -f5 /proc/$$/stat > '" + file + "'\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(bin, "kitten"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := &kitty.Host{}
	h.SetSockets(func() []string { return []string{"unix:/nonexistent/kitty"} })

	_, _ = h.Instances(context.Background())
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("kitten never ran: %v", err)
	}
	if pgrp, _ := strconv.Atoi(strings.TrimSpace(string(b))); pgrp == 0 || pgrp == syscall.Getpgrp() {
		t.Errorf("kitten's process group = %d, want one other than revier's (%d)", pgrp, syscall.Getpgrp())
	}
}

// The workspace kitty starts through the user's service manager, so it has
// the session's login environment whoever asks first, as one service, so two
// starts at once start one kitty. A start that fails because that service
// runs already starts nothing more. Where there is no systemd-run, or it
// fails otherwise, kitty starts detached from revier. Scripts named kitty,
// systemd-run and systemctl record how they were called, and exit as the case
// says.
func TestTheWorkspaceKittyStartsThroughTheServiceManager(t *testing.T) {
	const service = "systemd-run --user --quiet --collect --unit=revier-kitty -- %s/kitty --listen-on unix:/s"
	const active = "systemctl --user --quiet is-active revier-kitty"
	const detached = "kitty --detach --listen-on unix:/s"
	for _, tc := range []struct {
		name       string
		systemdRun string // the exit status of systemd-run; "" for none on PATH
		systemctl  string // the exit status of is-active
		want       []string
	}{
		{"started as a service", "0", "3", []string{service}},
		{"no systemd-run", "", "3", []string{detached}},
		{"the service runs already", "1", "0", []string{service, active}},
		{"no service manager", "1", "3", []string{service, active, detached}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, out := t.TempDir(), filepath.Join(t.TempDir(), "argv")
			record := func(name, status string) {
				script := "#!/bin/sh\necho " + name + " \"$@\" >> '" + out + "'\nexit " + status + "\n"
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			record("kitty", "0")
			record("systemctl", tc.systemctl)
			if tc.systemdRun != "" {
				record("systemd-run", tc.systemdRun)
			}
			t.Setenv("PATH", bin)

			if err := (&kitty.Host{}).StartProcess(context.Background(), "--listen-on", "unix:/s"); err != nil {
				t.Fatalf("StartProcess: %v", err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("nothing ran: %v", err)
			}
			var want []string
			for _, w := range tc.want {
				if strings.Contains(w, "%s") {
					w = fmt.Sprintf(w, bin)
				}
				want = append(want, w)
			}
			if got := strings.Split(strings.TrimSpace(string(b)), "\n"); !slices.Equal(got, want) {
				t.Errorf("ran %q, want %q", got, want)
			}
		})
	}
}

// kitty is usable where kitty and kitten are installed: Open starts the
// workspace kitty itself, so a running kitty whose socket does not answer, as
// one that asks a password, does not make revier fall back to another
// runtime, and Probe asks no socket. Without kitty there is nothing to start.
func TestKittyIsUsableWhereKittyAndKittenAreInstalled(t *testing.T) {
	bin, asked := t.TempDir(), filepath.Join(t.TempDir(), "asked")
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("kitten", "echo \"$@\" >> '"+asked+"'\nexit 1\n")
	t.Setenv("PATH", bin)
	h := &kitty.Host{}
	h.SetSockets(func() []string { return []string{"unix:/run/user/1000/kitty-4001"} })

	if err := h.Probe(context.Background()); err == nil {
		t.Error("Probe = nil without kitty on PATH, want an error: there is no kitty to start")
	}
	write("kitty", "exit 0\n")
	if err := h.Probe(context.Background()); err != nil {
		t.Errorf("Probe = %v, want kitty usable while a socket does not answer", err)
	}
	if b, err := os.ReadFile(asked); err == nil {
		t.Errorf("Probe ran kitten %q, want no socket asked", b)
	}
}
