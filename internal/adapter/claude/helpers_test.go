package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/pkg/revier"
)

// transcripts is a Claude config directory in a test: a probe that keeps a
// listing naming pid 101 as session "s1", and a place to write that session's
// transcript under a project directory.
func transcripts(t *testing.T) (*claude.Probe, string) {
	t.Helper()
	return transcriptsIn(t, t.TempDir(), "")
}

// transcriptsIn is transcripts under a given config directory, with the
// session listed as working in cwd.
func transcriptsIn(t *testing.T, root, cwd string) (*claude.Probe, string) {
	t.Helper()
	p := &claude.Probe{SessionsDir: filepath.Join(root, "sessions")}
	p.SetAgents(func(context.Context) ([]byte, error) {
		return []byte(`[{"pid": 101, "kind": "interactive", "sessionId": "s1", "status": "idle", "cwd": "` + cwd + `"}]`), nil
	})
	if _, err := p.Inspect(t.Context(), revier.Panel{PID: 101}); err != nil {
		t.Fatal(err)
	}
	return p, root
}

// said is a transcript line in which the agent says text.
func said(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":"` + text + `"}}`
}

// writeTranscript writes a session's transcript, one line per entry, under a
// project directory named dir.
func writeTranscript(t *testing.T, root, dir, session string, lines ...string) {
	t.Helper()
	path := filepath.Join(root, "projects", dir, session+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
