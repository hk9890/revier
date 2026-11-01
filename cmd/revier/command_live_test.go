//go:build live

// Layer L4 for the runner of a command target: what it reports of a command,
// and what it leaves running, is tested against real sh processes.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sh is the argv that runs script with sh, its arguments after it.
func sh(script string, args ...string) []string {
	return append([]string{"sh", "-c", script, "sh"}, args...)
}

// childOf is the pid a script wrote to file, killed when the test ends.
func childOf(t *testing.T, file string) int {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// A failed run reports its exit status and the last line it wrote to
// stderr; what it wrote to stdout goes nowhere.
func TestACommandsFailureNamesItsStatusAndItsLastLine(t *testing.T) {
	for name, tc := range map[string]struct {
		script, want string
	}{
		"last line":   {"echo out; echo first >&2; echo 'no page known' >&2; echo >&2; exit 3", "command exited 3: no page known"},
		"no stderr":   {"exit 1", "command exited 1"},
		"long stderr": {"i=0; while [ $i -lt 2000 ]; do echo 'noise noise noise' >&2; i=$((i+1)); done; echo last >&2; exit 2", "command exited 2: last"},
	} {
		t.Run(name, func(t *testing.T) {
			err := runTargetCommand(context.Background(), sh(tc.script), t.TempDir())
			if err == nil || err.Error() != tc.want {
				t.Errorf("run = %v, want %q", err, tc.want)
			}
		})
	}
}

// A run starts in the directory it is given, in a session of its own, with
// the null device on stdin.
func TestACommandRunsInItsDirectoryAndItsOwnSession(t *testing.T) {
	dir := t.TempDir()
	script := `[ "$(pwd -P)" = "$(cd "$1" && pwd -P)" ] || { echo "in $(pwd)" >&2; exit 1; }
set -- $(cat /proc/$$/stat); [ "$6" = "$$" ] || { echo "session $6" >&2; exit 1; }
[ "$(readlink /proc/$$/fd/0)" = /dev/null ] || { echo "stdin $(readlink /proc/$$/fd/0)" >&2; exit 1; }`
	if err := runTargetCommand(context.Background(), sh(script, dir), dir); err != nil {
		t.Error(err)
	}
}

// A directory that is gone is the run's failure.
func TestACommandInAMissingDirectoryDoesNotStart(t *testing.T) {
	err := runTargetCommand(context.Background(), sh("true"), filepath.Join(t.TempDir(), "gone"))
	if err == nil || !strings.Contains(err.Error(), "command did not start") {
		t.Errorf("run = %v, want it not started", err)
	}
}

// The end of the context kills the command alone: a child it detached lives
// on, as a browser it started must.
func TestTheDeadlineKillsTheCommandAndNotItsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := runTargetCommand(ctx, sh(`sleep 30 >/dev/null 2>&1 & echo $! > "$1"; sleep 30`, pidFile), t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run = %v, want the deadline", err)
	}
	if err := syscall.Kill(childOf(t, pidFile), 0); err != nil {
		t.Errorf("child: %v, want it alive", err)
	}
}

// A run the deadline ends reports the last line it wrote to stderr after
// the deadline.
func TestTheDeadlineNamesTheLastLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := runTargetCommand(ctx, sh(`echo 'waiting for a lock' >&2; exec sleep 30`), t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) || err.Error() != "context deadline exceeded: waiting for a lock" {
		t.Errorf("run = %v, want the deadline and the last line", err)
	}
}

// A child that holds stderr open holds the run for WaitDelay at most, and a
// command that exited 0 ran well.
func TestAChildHoldingStderrDoesNotHoldTheRun(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	start := time.Now()

	err := runTargetCommand(context.Background(), sh(`sleep 30 & echo $! > "$1"; exit 0`, pidFile), t.TempDir())
	childOf(t, pidFile)
	if err != nil {
		t.Errorf("run = %v, want success", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("run took %s, want it released after WaitDelay", took)
	}
}
