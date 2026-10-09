//go:build live

// Layer L4: what the GNOME host starts is a real process. No GNOME session and
// no wctl: Open launches and asks wctl nothing.
package gnome_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/gnome"
	"github.com/hk9890/revier/pkg/revier"
)

// A launched application outlives the keypress that started it. The context
// bounds the host calls around a launch and never the application: a TUI
// activation ends it the moment the new window is bound. The application also
// gets a session of its own, so the hangup of the terminal revier runs in -
// the popup closing - does not reach it.
func TestOpenOutlivesTheKeypress(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	_, err := (&gnome.Host{}).Open(ctx, revier.Realization{
		Launch: []string{"sh", "-c", `echo $$ > "$0"; exec sleep 30`, pidFile},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var pid int
	for deadline := time.Now().Add(2 * time.Second); pid == 0 && time.Now().Before(deadline); {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the launched command never started")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	cancel()
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the application died with the keypress context: %v", err)
	}
	// /proc/<pid>/stat: "pid (comm) state ppid pgrp session ...", and comm
	// may hold spaces, so the fields are counted from its closing paren.
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	after := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	if sid, _ := strconv.Atoi(after[3]); sid != pid {
		t.Errorf("session = %d, want one of its own (%d)", sid, pid)
	}
}
