package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// The transcript under the directory Claude Code names from the session's
// working directory is the one read, though another directory holds a file of
// the same session written later.
func TestDetailReadsTheTranscriptUnderTheSessionsWorkingDirectory(t *testing.T) {
	p, root := transcriptsIn(t, t.TempDir(), "/home/user/dev/demo.app")
	writeTranscript(t, root, "-home-user-dev-demo-app", "s1", said("where it works"))
	writeTranscript(t, root, "-zzz-elsewhere", "s1", said("somewhere else"))
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "projects", "-zzz-elsewhere", "s1.jsonl"), later, later); err != nil {
		t.Fatal(err)
	}
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "where it works" {
		t.Errorf("Detail = %+v, %v; want the transcript under the working directory", d, err)
	}
}

// A session not filed under its working directory - one that entered a
// worktree stays under the directory it started in - is found in any project
// directory, and where it left a file in two, the one written last is read,
// whatever the directories are called.
func TestDetailReadsTheTranscriptWrittenLastWhenItIsNotUnderTheWorkingDirectory(t *testing.T) {
	p, root := transcriptsIn(t, t.TempDir(), "/home/user/dev/demo/.claude/worktrees/fix")
	writeTranscript(t, root, "-a-first-alphabetically", "s1", said("before the move"))
	writeTranscript(t, root, "-home-user-dev-demo", "s1", said("after the move"))
	earlier := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "projects", "-a-first-alphabetically", "s1.jsonl"), earlier, earlier); err != nil {
		t.Fatal(err)
	}
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "after the move" {
		t.Errorf("Detail = %+v, %v; want the transcript written last", d, err)
	}
}

// A character a file pattern would read as syntax, in the config directory's
// path, is only a character.
func TestDetailFindsATranscriptUnderAPathWithPatternCharacters(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude[work]*?")
	p, _ := transcriptsIn(t, root, "")
	writeTranscript(t, root, "-demo", "s1", said("found anyway"))
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "found anyway" {
		t.Errorf("Detail = %+v, %v; want the transcript found", d, err)
	}
}

// A transcript whose size and modification time did not change is not read
// again: the pane asks every refresh, and most refreshes change nothing.
func TestDetailDoesNotReadAnUnchangedTranscriptAgain(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1", said("first word"))
	path := filepath.Join(root, "projects", "-demo", "s1.jsonl")
	if d, _ := p.Detail(context.Background(), revier.Panel{PID: 101}); d.Message != "first word" {
		t.Fatalf("Detail = %q, want the first word", d.Message)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The same size and time, other bytes: a read would see them.
	writeTranscript(t, root, "-demo", "s1", said("other word"))
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if d, _ := p.Detail(context.Background(), revier.Panel{PID: 101}); d.Message != "first word" {
		t.Errorf("Detail = %q for an unchanged file, want the first read kept", d.Message)
	}
	later := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if d, _ := p.Detail(context.Background(), revier.Panel{PID: 101}); d.Message != "other word" {
		t.Errorf("Detail = %q for a changed file, want it read again", d.Message)
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

// A session the listing gives no working directory for is found under
// whichever project directory holds its transcript.
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
	if d, err := p.Detail(context.Background(), revier.Panel{PID: 101}); err != nil || d.Message != "" {
		t.Errorf("Detail = %+v, %v; want no message: the only one is before the window", d, err)
	}

	lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":"said just now"}}`)
	writeTranscript(t, root, "-demo", "s1", lines...)
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil || d.Message != "said just now" {
		t.Errorf("Detail = %+v, %v; want the message at the end", d, err)
	}
}

// The turn is what the user asked last and every tool the agent called since,
// in order: a call whose result is an error failed, and one with no result is
// still open. A call before the prompt is an earlier turn's, a subagent's call
// is its own work, and a line Claude Code wrote for itself is not a prompt.
func TestDetailIsTheTurnTheAgentIsIn(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1",
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t0","name":"Read","input":{"file_path":"/old"}}]}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":"the earlier answer"}}`,
		`{"type":"user","message":{"role":"user","content":"make the  tests\ngreen"}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"Base directory for this skill"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./...","description":"Run the tests"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"FAIL"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"tool_use","id":"s1","name":"Grep","input":{"pattern":"x"}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/src/a.go"}},{"type":"tool_use","id":"t3","name":"AskUserQuestion","input":{"questions":[{"question":"Merge now?"}]}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}`,
	)
	d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if d.Prompt != "make the tests green" {
		t.Errorf("Prompt = %q, want the user's last prompt on one line", d.Prompt)
	}
	want := []revier.ToolCall{
		{Name: "Bash", Input: "go test ./...", Failed: true},
		{Name: "Edit", Input: "/src/a.go"},
		{Name: "AskUserQuestion", Input: "Merge now?", Pending: true},
	}
	if !slices.Equal(d.Tools, want) {
		t.Errorf("Tools = %+v, want %+v", d.Tools, want)
	}
	if d.Message != "the earlier answer" {
		t.Errorf("Message = %q, want the last text, though it is older than the turn", d.Message)
	}
}

// A slash command is filed as tags and reads as it was typed, and a paste
// filed under tags leaves the words typed with it. What a command printed for
// the user alone, a shell command of the user's, a background task's report,
// the summary of a compacted conversation and an interrupt start no turn. A
// prompt that only quotes a command's tags is a prompt.
func TestDetailReadsAPromptAsItWasTyped(t *testing.T) {
	typed := `{"type":"user","message":{"role":"user","content":"typed"}}`
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"a slash command", []string{`{"type":"user","message":{"role":"user","content":"<command-message>ship</command-message>\n<command-name>/ship</command-name>\n<command-args>the fix</command-args>"}}`}, "/ship the fix"},
		{"a command's own output", []string{typed, `{"type":"user","message":{"role":"user","content":"<local-command-stdout>renamed</local-command-stdout>"}}`}, "typed"},
		{"a shell command", []string{typed, `{"type":"user","message":{"role":"user","content":"<bash-input>ls</bash-input>"}}`, `{"type":"user","message":{"role":"user","content":"<bash-stdout>a.go</bash-stdout><bash-stderr></bash-stderr>"}}`}, "typed"},
		{"a background task's report", []string{typed, `{"type":"user","message":{"role":"user","content":"<task-notification>\n<task-id>a1</task-id>\n<status>completed</status>\n</task-notification>"}}`}, "typed"},
		{"a compacted conversation's summary", []string{typed, `{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued from a previous conversation."}}`}, "typed"},
		{"an interrupt", []string{typed, `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`}, "typed"},
		{"a pasted file", []string{`{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 5000) + `"}}`}, strings.Repeat("x", 300)},
		{"a paste under tags", []string{`{"type":"user","message":{"role":"user","content":"<pasted_content id=\"2fb7\">func main() {}</pasted_content id=\"2fb7\">\n\n\nwhy does this fail"}}`}, "why does this fail"},
		{"a command's tags quoted", []string{`{"type":"user","message":{"role":"user","content":"what is <command-name>/ship</command-name> for"}}`}, "what is <command-name>/ship</command-name> for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, root := transcripts(t)
			writeTranscript(t, root, "-demo", "s1", tc.lines...)
			d, err := p.Detail(context.Background(), revier.Panel{PID: 101})
			if err != nil || d.Prompt != tc.want {
				t.Errorf("Prompt = %q, %v; want %q", d.Prompt, err, tc.want)
			}
		})
	}
}

// The tokens are the input of the agent's last answer that counted any: new,
// read from the cache and written to it. A subagent's answer is its own, and
// an answer Claude Code makes up itself counts none. The cache goes cold an
// hour after that answer.
func TestDetailCountsTheTokensOfTheLastAnswer(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1",
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":"older","usage":{"input_tokens":5,"cache_read_input_tokens":1000,"cache_creation_input_tokens":10}}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:06:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test"}}],"usage":{"input_tokens":2,"cache_read_input_tokens":61000,"cache_creation_input_tokens":1072,"output_tokens":145}}}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-24T10:07:00Z","message":{"role":"assistant","content":"a subagent","usage":{"input_tokens":9,"cache_read_input_tokens":9000,"cache_creation_input_tokens":0}}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:08:00Z","message":{"role":"assistant","model":"<synthetic>","content":"No response requested.","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
	)
	d, err := p.Detail(t.Context(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if d.Context != 62_074 {
		t.Errorf("Context = %d, want 62074", d.Context)
	}
	if want := time.Date(2026, 9, 24, 11, 6, 0, 0, time.UTC); !d.CacheUntil.Equal(want) {
		t.Errorf("CacheUntil = %v, want %v", d.CacheUntil, want)
	}
}

// An API error after the user's prompt is an answer Claude Code made up, which
// counts no tokens: the answer before the prompt still says them, and the
// cache it left.
func TestDetailCountsTheTokensPastAnAPIError(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1",
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":"done","usage":{"input_tokens":2,"cache_read_input_tokens":61000,"cache_creation_input_tokens":1072}}}`,
		`{"type":"user","timestamp":"2026-09-24T10:02:00Z","message":{"role":"user","content":"and now the docs"}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:02:05Z","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 Overloaded"}],"usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
	)
	d, err := p.Detail(t.Context(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC); d.Context != 62_074 || !d.CacheUntil.Equal(want) {
		t.Errorf("Context, CacheUntil = %d, %v; want 62074, %v", d.Context, d.CacheUntil, want)
	}
}

// After a compaction the next prompt reads the summary, not the conversation
// before it: no answer from before counts, until one after it does.
func TestDetailCountsNoTokensFromBeforeACompaction(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1",
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":"done","usage":{"input_tokens":2,"cache_read_input_tokens":180000,"cache_creation_input_tokens":0}}}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-09-24T10:01:00Z"}`,
		`{"type":"user","isCompactSummary":true,"timestamp":"2026-09-24T10:01:00Z","message":{"role":"user","content":"This session is being continued from a previous conversation."}}`,
	)
	d, err := p.Detail(t.Context(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if d.Context != 0 || !d.CacheUntil.IsZero() {
		t.Errorf("Context, CacheUntil = %d, %v; want none from before the compaction", d.Context, d.CacheUntil)
	}
}

// A usage that does not parse costs the tokens and nothing else, and an
// answer with no time says no cache.
func TestDetailReadsTheMessageOfAnAnswerWhoseUsageDoesNotParse(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1",
		`{"type":"assistant","message":{"role":"assistant","content":"counted, untimed","usage":{"input_tokens":5000}}}`,
		`{"type":"assistant","timestamp":"2026-09-24T10:00:00Z","message":{"role":"assistant","content":"spelled otherwise","usage":{"input_tokens":"many"}}}`,
	)
	d, err := p.Detail(t.Context(), revier.Panel{PID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if d.Message != "spelled otherwise" || d.Context != 5000 || !d.CacheUntil.IsZero() {
		t.Errorf("Detail = %+v; want the message, the untimed answer's tokens and no cache", d)
	}
}

// A transcript that counts no tokens says nothing of them or of the cache.
func TestDetailWithoutUsageSaysNoTokens(t *testing.T) {
	p, root := transcripts(t)
	writeTranscript(t, root, "-demo", "s1", said("no usage here"))
	d, err := p.Detail(t.Context(), revier.Panel{PID: 101})
	if err != nil || d.Context != 0 || !d.CacheUntil.IsZero() {
		t.Errorf("Detail = %+v, %v; want no tokens and no cache", d, err)
	}
}
