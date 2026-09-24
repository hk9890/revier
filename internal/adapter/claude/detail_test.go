package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/pkg/revier"
)

// transcripts is a Claude config directory in a test: a probe whose listing
// names pid 101 as session "s1", and a place to write that session's
// transcript under a project directory.
func transcripts(t *testing.T) (*claude.Probe, string) {
	t.Helper()
	root := t.TempDir()
	p := &claude.Probe{SessionsDir: filepath.Join(root, "sessions")}
	p.SetAgents(func(context.Context) ([]byte, error) {
		return []byte(`[{"pid": 101, "kind": "interactive", "sessionId": "s1", "status": "idle"}]`), nil
	})
	return p, root
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

// Detail is the last thing the agent itself wrote, with when: a later tool
// call, a subagent's text, the user's prompt, a system line, a field Claude
// Code added and a line that is not JSON are all passed over.
func TestDetailIsTheLastTextTheAgentWrote(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-home-user-dev-demo", "s1",
		`{"type":"user","message":{"role":"user","content":"fix it"}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"an older answer"}]}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:05:00Z","newField":{"x":1},"message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"  Fixed, and pushed as PR #117.  "}]}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:06:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"git push"}}]}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-24T10:07:00Z","message":{"role":"assistant","content":[{"type":"text","text":"a subagent's report"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`not json at all`,
		`{"type":"system","subtype":"turn_duration"}`,
	)
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if d.Message != "Fixed, and pushed as PR #117." {
		t.Errorf("Message = %q, want the last text the agent wrote", d.Message)
	}
	if want := time.Date(2026, 9, 24, 10, 5, 0, 0, time.UTC); !d.At.Equal(want) {
		t.Errorf("At = %v, want %v", d.At, want)
	}
}

// A message given as a plain string is read, and one of several text blocks
// is read whole.
func TestDetailReadsEveryShapeOfContent(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"a plain string", `"all done"`, "all done"},
		{"several text blocks", `[{"type":"text","text":"first part"},{"type":"tool_use","name":"Read"},{"type":"text","text":"second part"}]`, "first part\n\nsecond part"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, root := transcripts(t)
			writeTranscript(t, root, "-demo", "s1",
				`{"type":"assistant","message":{"role":"assistant","content":`+tc.content+`}}`)
			d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
			if err != nil {
				t.Fatal(err)
			}
			if d.Message != tc.want {
				t.Errorf("Message = %q, want %q", d.Message, tc.want)
			}
		})
	}
}

// The transcript is found under whichever project directory holds it, not
// under a name spelled from the cwd: an agent that moved with /cd is filed
// under the directory it moved to.
func TestDetailFindsTheTranscriptUnderAnyProjectDirectory(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-somewhere-else-entirely", "s1",
		`{"type":"assistant","message":{"role":"assistant","content":"moved and done"}}`)
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "moved and done" {
		t.Errorf("Detail = %+v, %v; want the transcript found", d, err)
	}
}

// Only the end of a long transcript is read: the last message is near the
// end, and a transcript can be many megabytes. A line cut by the window's
// start is passed over, not misread.
func TestDetailReadsOnlyTheTranscriptsTail(t *testing.T) {
	p, root := transcripts(t)
	padding := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 1<<10) + `"}}`
	early := `{"type":"assistant","message":{"role":"assistant","content":"said long ago"}}`
	var lines []string
	lines = append(lines, early)
	for range 600 { // 600 KiB of prompts after it: past the window
		lines = append(lines, padding)
	}
	writeTranscript(t, root, "-demo", "s1", lines...)
	if d, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err == nil {
		t.Errorf("Detail = %+v, want an error: the only message is before the window", d)
	}

	lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":"said just now"}}`)
	writeTranscript(t, root, "-demo", "s1", lines...)
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "said just now" {
		t.Errorf("Detail = %+v, %v; want the message at the end", d, err)
	}
}

// Every way there is nothing to show is an error, which the pane shows as
// nothing: a pane whose process no session lists, a session with no
// transcript, and a transcript in which the agent has said nothing.
func TestDetailFailsWhenThereIsNothingToShow(t *testing.T) {
	t.Run("the pid is not listed", func(t *testing.T) {
		p, root := transcripts(t)
		writeTranscript(t, root, "-demo", "s1", `{"type":"assistant","message":{"role":"assistant","content":"x"}}`)
		if _, err := p.Detail(context.Background(), revier.Panel{PID: 202}); err == nil {
			t.Error("Detail succeeded for a pid no session lists")
		}
	})
	t.Run("a panel with no pid", func(t *testing.T) {
		p, _ := transcripts(t)
		if _, err := p.Detail(context.Background(), revier.Panel{}); err == nil {
			t.Error("Detail succeeded for a panel with no pid")
		}
	})
	t.Run("no transcript", func(t *testing.T) {
		p, _ := transcripts(t)
		if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err == nil {
			t.Error("Detail succeeded with no transcript")
		}
	})
	t.Run("nothing said", func(t *testing.T) {
		p, root := transcripts(t)
		writeTranscript(t, root, "-demo", "s1",
			`{"type":"user","message":{"role":"user","content":"hello?"}}`,
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read"}]}}`)
		if _, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err == nil {
			t.Error("Detail succeeded for a transcript with no text from the agent")
		}
	})
}
