//go:build live

package procgroup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// KillAll ends a command that runs, and the child it started in its group,
// and refuses every later command: revier is ending, and a command started
// now would outlive it.
func TestKillAllEndsTheGroupsThatRunAndRefusesNewOnes(t *testing.T) {
	t.Cleanup(func() {
		groups.Lock()
		defer groups.Unlock()
		groups.ended = false
	})
	pidFile := filepath.Join(t.TempDir(), "child")
	c := exec.Command("sh", "-c", `sleep 300 & echo $! > "$0"; wait`, pidFile)
	ran := make(chan error, 1)
	go func() { ran <- Run(c) }()
	child := waitForPid(t, pidFile)

	KillAll()
	select {
	case err := <-ran:
		if err == nil {
			t.Error("the command ended without an error, want it killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("KillAll did not end the command")
	}
	deadline := time.Now().Add(5 * time.Second)
	for alive(child) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the command's child %d outlived KillAll", child)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := Run(exec.Command("true")); !errors.Is(err, ErrEnded) {
		t.Errorf("Run after KillAll = %v, want ErrEnded", err)
	}
}

// waitForPid is the pid the command wrote to path, once it has.
func waitForPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && perr == nil {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the command did not start its child")
	return 0
}

// alive reports a process that runs: a zombie waiting for its new parent to
// reap it has ended.
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}
