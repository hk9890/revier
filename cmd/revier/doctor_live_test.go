//go:build live

// Layer L4 for `revier doctor`: scripts named systemd-run, systemctl and loginctl first
// on PATH say whether the user lingers. No service manager is reached, and
// the kitty and hc on PATH are never run.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Under hc, where the user's service manager runs the core and the user does
// not linger, doctor says that the core ends at the last logout, and how to
// keep it. A user who lingers, and a kitty user, read
// nothing of it.
func TestDoctorSaysTheHcCoreEndsAtTheLastLogout(t *testing.T) {
	t.Setenv("HC_SOCKET", filepath.Join(t.TempDir(), "s.sock"))
	const line = "the hc core stops at your last logout, and every agent with it; run loginctl enable-linger to keep it running"
	for _, c := range []struct {
		runtime, linger string
		want            bool
	}{
		{"hc", "no", true},
		{"hc", "yes", false},
		{"kitty", "no", false},
	} {
		kittyOnPath(t, true)
		bin := os.Getenv("PATH")
		for name, body := range map[string]string{
			"herdr-core":  "exit 1",
			"systemd-run": "exit 0",
			"systemctl":   "exit 0",
			"loginctl":    "echo " + c.linger,
		} {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		configRoot(t, "[hosts]\nruntime = [\""+c.runtime+"\"]\nwindow = [\"none\"]\n", map[string]string{"sound.toml": sound})
		var out strings.Builder
		if err := cmdDoctor(&out, nil); err != nil {
			t.Fatalf("cmdDoctor: %v", err)
		}
		if got := strings.Contains(out.String(), line); got != c.want {
			t.Errorf("runtime %s, linger %s: says the core ends at the last logout = %v, want %v:\n%s", c.runtime, c.linger, got, c.want, out.String())
		}
	}
}
