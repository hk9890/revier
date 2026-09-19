//go:build live

// Layer L4: the listing command as a real process, with a fake claude first
// on PATH.
package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/pkg/revier"
)

// fakeClaude puts a claude first on PATH that runs body as a shell script.
func fakeClaude(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// hungClaude puts a claude first on PATH that never answers, with a child
// that keeps its stdout open.
func hungClaude(t *testing.T) {
	t.Helper()
	fakeClaude(t, "sleep 30 &\nsleep 30")
}

// A claude whose child keeps stdout open past the deadline does not hold the
// probe: Inspect returns soon after the context ends, and the next Inspect is
// not left waiting on the lock.
func TestInspectReturnsWhenAChildOfClaudeHoldsItsOutput(t *testing.T) {
	hungClaude(t)
	p := &claude.Probe{SessionsDir: t.TempDir()}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := p.Inspect(ctx, revier.Panel{PID: 101}); err == nil {
		t.Fatal("Inspect returned no error")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("Inspect took %v, want it back within a few seconds of its deadline", took)
	}
}

// A caller with no deadline of its own still gets an answer: the listing has
// its own bound.
func TestInspectReturnsWithoutADeadlineOfItsCaller(t *testing.T) {
	hungClaude(t)
	p := &claude.Probe{SessionsDir: t.TempDir()}

	start := time.Now()
	if _, err := p.Inspect(context.Background(), revier.Panel{PID: 101}); err == nil {
		t.Fatal("Inspect returned no error")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("Inspect took %v, want the listing's own bound", took)
	}
}
