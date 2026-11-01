//go:build live

// Layer L4: the kitten the host runs is a real process, a script first on
// PATH, and so are kitty and systemd-run. No kitty runs.
package kitty_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
// the session's login environment whoever asks first, and detached from
// revier where there is none. Scripts named kitty and systemd-run record how
// they were called.
func TestTheWorkspaceKittyStartsThroughTheServiceManager(t *testing.T) {
	for _, tc := range []struct {
		name    string
		systemd bool
		want    string
	}{
		{"systemd-run", true, "systemd-run --user --quiet --collect -- %s/kitty --listen-on unix:/s"},
		{"no systemd-run", false, "kitty --detach --listen-on unix:/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, out := t.TempDir(), filepath.Join(t.TempDir(), "argv")
			record := func(name string) {
				script := "#!/bin/sh\necho " + name + " \"$@\" > '" + out + "'\n"
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			record("kitty")
			if tc.systemd {
				record("systemd-run")
			}
			t.Setenv("PATH", bin)

			if err := (&kitty.Host{}).StartProcess(context.Background(), "--listen-on", "unix:/s"); err != nil {
				t.Fatalf("StartProcess: %v", err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("nothing ran: %v", err)
			}
			want := tc.want
			if tc.systemd {
				want = fmt.Sprintf(want, bin)
			}
			if got := strings.TrimSpace(string(b)); got != want {
				t.Errorf("ran %q, want %q", got, want)
			}
		})
	}
}
