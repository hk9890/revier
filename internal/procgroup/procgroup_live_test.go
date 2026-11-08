//go:build live

package procgroup_test

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/procgroup"
)

// The end of the context kills the background child with the command, so
// the output it holds closes at once. The long WaitDelay shows that the
// return comes from that kill and not from giving up on the output.
func TestCommandContextEndsTheWholeGroupWithTheContext(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	c := procgroup.CommandContext(ctx, "sh", "-c", "sleep 10 & sleep 10")
	c.Stdout = &out
	c.WaitDelay = time.Minute

	start := time.Now()
	err := procgroup.Run(c)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Run returned after %s, want soon after the deadline", took)
	}
	if err == nil {
		t.Error("Run = nil, want the command killed")
	}
}
