//go:build live

// Layer L4: the kitten the host runs is a real process, a script first on
// PATH. No kitty runs.
package kitty_test

import (
	"context"
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
