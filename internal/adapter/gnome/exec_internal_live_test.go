//go:build live

// Layer L4: what execRun runs, as a real sh, with no display.
package gnome

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A command that leaves a child holding its output does not hold the call
// past its end: the wait for the output is bounded, as the ssh and claude
// adapters bound theirs. The command exited 0, so its change was applied and
// the call reports no failure.
func TestACommandWhoseChildHoldsItsOutputEnds(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	t.Cleanup(func() {
		raw, _ := os.ReadFile(pidFile)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := execRun(ctx, "sh", "-c", `sleep 5 & echo $! > "$0"; echo started`, pidFile)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("execRun took %v, want it over within the wait delay", took)
	}
	if string(out) != "started\n" || err != nil {
		t.Errorf("execRun = %q, %v; want what the command wrote", out, err)
	}
}
