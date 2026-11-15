//go:build live

// Layer L4: systemd-run, systemctl and the program are real processes,
// scripts first on PATH that record how they were called. No service manager
// is reached.
package userunit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/hk9890/revier/internal/userunit"
)

// A unit starts through systemd-run, with its properties and its environment,
// as one service, so two starts at once start one program. A start that
// fails because that service runs already starts nothing more. Where there is
// no systemd-run, or it fails otherwise, the detached start runs.
func TestAUnitStartsThroughTheServiceManager(t *testing.T) {
	const service = "systemd-run --user --quiet --collect --unit=revier-x --property=Type=forking --setenv=A=b -- /bin/prog --flag"
	const active = "systemctl --user --quiet is-active revier-x"
	const detached = "detached"
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
			record("systemctl", tc.systemctl)
			if tc.systemdRun != "" {
				record("systemd-run", tc.systemdRun)
			}
			t.Setenv("PATH", bin)

			unit := userunit.Unit{Name: "revier-x", Properties: []string{"Type=forking"}, Env: []string{"A=b"}}
			err := userunit.Start(t.Context(), unit, []string{"/bin/prog", "--flag"}, func(context.Context) error {
				f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				_, err = f.WriteString(detached + "\n")
				return errors.Join(err, f.Close())
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("nothing ran: %v", err)
			}
			if got := strings.Split(strings.TrimSpace(string(b)), "\n"); !slices.Equal(got, tc.want) {
				t.Errorf("ran %q, want %q", got, tc.want)
			}
		})
	}
}

// What the detached start returns is Start's.
func TestAFailedDetachedStartIsStartsError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	failed := errors.New("no room")
	if err := userunit.Start(t.Context(), userunit.Unit{Name: "revier-x"}, []string{"/bin/prog"}, func(context.Context) error { return failed }); !errors.Is(err, failed) {
		t.Errorf("Start = %v, want %v", err, failed)
	}
}

// Run runs its command in a process group of its own, so the ctrl+c a
// terminal sends to revier's group, which cancels a shutdown or a restore
// after its step, does not end the start that step makes.
func TestRunRunsInAProcessGroupOfItsOwn(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pgrp")
	if err := userunit.Run(t.Context(), "/bin/sh", "-c", "cut -d' ' -f5 /proc/$$/stat > '"+file+"'"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the command never ran: %v", err)
	}
	if pgrp, _ := strconv.Atoi(strings.TrimSpace(string(b))); pgrp == 0 || pgrp == syscall.Getpgrp() {
		t.Errorf("the command's process group = %d, want one other than revier's (%d)", pgrp, syscall.Getpgrp())
	}
}

// A command that fails is named in Run's error.
func TestRunNamesTheCommandThatFailed(t *testing.T) {
	if err := userunit.Run(t.Context(), "/bin/sh", "-c", "exit 3"); err == nil || !strings.HasPrefix(err.Error(), "sh: ") {
		t.Errorf("Run = %v, want an error that names sh", err)
	}
}
