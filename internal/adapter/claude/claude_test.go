package claude_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/pkg/revier"
)

func TestMatch(t *testing.T) {
	p := &claude.Probe{}
	cases := []struct {
		name  string
		panel revier.Panel
		want  bool
	}{
		{"foreground command", revier.Panel{Command: []string{"claude"}}, true},
		{"the old marker variable alone claims nothing", revier.Panel{Command: []string{"zsh"}, Vars: map[string]string{"CS_TAB": "1"}}, false},
		{"absolute path command", revier.Panel{Command: []string{"/home/hans/.local/bin/claude"}}, true},
		{"an npm install, under node", revier.Panel{Command: []string{"node", "/home/hans/.npm-global/bin/claude"}}, true},
		{"a file called claude in an editor", revier.Panel{Command: []string{"nvim", "internal/adapter/claude"}}, false},
		{"a shell is not an agent", revier.Panel{Command: []string{"zsh"}}, false},
		{"empty panel", revier.Panel{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Match(tc.panel); got != tc.want {
				t.Errorf("Match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	cases := map[string]revier.Status{
		"busy":    revier.StatusRunning,
		"waiting": revier.StatusAttention,
		"idle":    revier.StatusIdle,
		"":        revier.StatusUnknown,
		"shell":   revier.StatusUnknown,
		"paused":  revier.StatusUnknown,
	}
	for in, want := range cases {
		if got := claude.Status(in); got != want {
			t.Errorf("Status(%q) = %v, want %v", in, got, want)
		}
	}
}

// statusJSON lists one interactive session per status, by pid, beside a
// background session, which has none.
const statusJSON = `[
  {"id": "22e0eb3a", "kind": "background", "sessionId": "22e0eb3a-0c4c", "state": "blocked"},
  {"pid": 101, "kind": "interactive", "sessionId": "a", "status": "busy"},
  {"pid": 102, "kind": "interactive", "sessionId": "b", "status": "waiting", "waitingFor": "permission prompt"},
  {"pid": 103, "kind": "interactive", "sessionId": "c", "status": "idle"}
]`

// The state is the listing's, found by pid. The title says nothing about it:
// a spinner on an idle session and a rest glyph on a busy one both read as the
// listing says. A pane Claude does not list is unknown, not idle.
func TestInspectReadsTheListing(t *testing.T) {
	p := &claude.Probe{SessionsDir: t.TempDir()}
	p.SetAgents(agents(statusJSON, nil))
	cases := []struct {
		name  string
		panel revier.Panel
		want  revier.Status
	}{
		{"busy is running", revier.Panel{PID: 101, Title: "✳ Ready"}, revier.StatusRunning},
		{"waiting is attention", revier.Panel{PID: 102, Title: "✳ Ready"}, revier.StatusAttention},
		{"idle is idle", revier.Panel{PID: 103, Title: "⠧ Working"}, revier.StatusIdle},
		{"a pid not listed is unknown", revier.Panel{PID: 999}, revier.StatusUnknown},
		{"no pid is unknown", revier.Panel{}, revier.StatusUnknown},
		{"the old hook variable is not read", revier.Panel{PID: 103, Vars: map[string]string{"CS_STATE": "attn"}}, revier.StatusIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Inspect(context.Background(), tc.panel)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if got.Status != tc.want {
				t.Errorf("Status = %v, want %v", got.Status, tc.want)
			}
			if got.Harness != "claude" {
				t.Errorf("Harness = %q", got.Harness)
			}
		})
	}
}

func TestInspectKeepsTheTitleAsActivity(t *testing.T) {
	p := &claude.Probe{SessionsDir: t.TempDir()}
	p.SetAgents(agents(statusJSON, nil))
	got, err := p.Inspect(context.Background(), revier.Panel{PID: 101, Title: "⠧ Investigating setup"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity != "Investigating setup" {
		t.Errorf("Activity = %q", got.Activity)
	}
}

// A listing that cannot be had is the probe's error; the core turns it into
// unknown for the panel.
func TestInspectReportsWhatItCannotRead(t *testing.T) {
	p := &claude.Probe{SessionsDir: t.TempDir()}
	p.SetAgents(agents("", errors.New("exec: claude: not found")))
	if _, err := p.Inspect(context.Background(), revier.Panel{PID: 101}); err == nil {
		t.Error("Inspect returned no error")
	}
}

// counted answers from a listing the test can change, and counts the runs.
type counted struct {
	out  string
	runs int
}

func (c *counted) agents(context.Context) ([]byte, error) {
	c.runs++
	return []byte(c.out), nil
}

// clock is a time the test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// touch creates the file if it is missing and sets its modification time.
func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func inspect(t *testing.T, p *claude.Probe, pid int) revier.Status {
	t.Helper()
	got, err := p.Inspect(context.Background(), revier.Panel{PID: pid})
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

// The command runs again only when an answer may have changed: a session file
// written, added or removed, or the listing MaxAge old. Every survey of a
// quiet desktop reuses the last answer.
func TestInspectRunsTheListingOnlyWhenASessionChanged(t *testing.T) {
	dir := t.TempDir()
	start := time.Unix(1_800_000_000, 0)
	file := filepath.Join(dir, "101.json")
	touch(t, file, start)
	src := &counted{out: `[{"pid": 101, "status": "idle"}]`}
	clk := &clock{t: start}
	p := &claude.Probe{SessionsDir: dir}
	p.SetAgents(src.agents)
	p.SetNow(clk.now)

	if got := inspect(t, p, 101); got != revier.StatusIdle || src.runs != 1 {
		t.Fatalf("first read: %v after %d runs, want idle after 1", got, src.runs)
	}

	// Nothing written: the next surveys reuse the answer, even a stale one.
	src.out = `[{"pid": 101, "status": "busy"}]`
	clk.t = start.Add(time.Second)
	inspect(t, p, 101)
	clk.t = start.Add(2 * time.Second)
	if got := inspect(t, p, 101); got != revier.StatusIdle || src.runs != 1 {
		t.Fatalf("quiet directory: %v after %d runs, want the cached idle after 1", got, src.runs)
	}

	// Claude Code rewrites its file on a status change.
	touch(t, file, start.Add(2*time.Second))
	if got := inspect(t, p, 101); got != revier.StatusRunning || src.runs != 2 {
		t.Fatalf("file rewritten: %v after %d runs, want running after 2", got, src.runs)
	}

	// A session that starts adds a file.
	src.out = `[{"pid": 101, "status": "busy"}, {"pid": 102, "status": "waiting"}]`
	touch(t, filepath.Join(dir, "102.json"), start)
	if got := inspect(t, p, 102); got != revier.StatusAttention || src.runs != 3 {
		t.Fatalf("file added: %v after %d runs, want attention after 3", got, src.runs)
	}

	// A session that ends removes one.
	src.out = `[{"pid": 101, "status": "busy"}]`
	if err := os.Remove(filepath.Join(dir, "102.json")); err != nil {
		t.Fatal(err)
	}
	if got := inspect(t, p, 102); got != revier.StatusUnknown || src.runs != 4 {
		t.Fatalf("file removed: %v after %d runs, want unknown after 4", got, src.runs)
	}
}

// A run the caller's deadline cut short is not kept: the next caller with
// time left runs the command again rather than read the timeout for MaxAge.
func TestInspectDoesNotKeepARunItsCallerCancelled(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	runs := 0
	run := func(ctx context.Context) ([]byte, error) {
		runs++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []byte(`[{"pid": 101, "status": "idle"}]`), nil
	}
	p := &claude.Probe{SessionsDir: filepath.Join(t.TempDir(), "absent")}
	p.SetAgents(run)
	p.SetNow((&clock{t: start}).now)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Inspect(cancelled, revier.Panel{PID: 101}); err == nil {
		t.Fatal("Inspect on a cancelled context returned no error")
	}
	if got := inspect(t, p, 101); got != revier.StatusIdle || runs != 2 {
		t.Errorf("after a cancelled run: %v after %d runs, want idle after 2", got, runs)
	}
}

// A change the directory does not show is still seen, MaxAge late.
func TestInspectRunsTheListingAtLeastEveryMaxAge(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	src := &counted{out: `[{"pid": 101, "status": "idle"}]`}
	clk := &clock{t: start}
	p := &claude.Probe{SessionsDir: filepath.Join(t.TempDir(), "absent")}
	p.SetAgents(src.agents)
	p.SetNow(clk.now)

	inspect(t, p, 101)
	src.out = `[{"pid": 101, "status": "waiting"}]`
	clk.t = start.Add(claude.MaxAge - time.Millisecond)
	if got := inspect(t, p, 101); got != revier.StatusIdle || src.runs != 1 {
		t.Fatalf("before MaxAge: %v after %d runs, want the cached idle after 1", got, src.runs)
	}
	clk.t = start.Add(claude.MaxAge)
	if got := inspect(t, p, 101); got != revier.StatusAttention || src.runs != 2 {
		t.Fatalf("at MaxAge: %v after %d runs, want attention after 2", got, src.runs)
	}
}

// The sessions directory follows Claude Code's own configuration home.
func TestInspectWatchesClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	start := time.Unix(1_800_000_000, 0)
	src := &counted{out: `[{"pid": 101, "status": "idle"}]`}
	p := &claude.Probe{}
	p.SetAgents(src.agents)
	p.SetNow((&clock{t: start}).now)

	inspect(t, p, 101)
	touch(t, filepath.Join(home, "sessions", "101.json"), start)
	inspect(t, p, 101)
	if src.runs != 2 {
		t.Errorf("a file under $CLAUDE_CONFIG_DIR/sessions ran the listing %d times, want 2", src.runs)
	}
}

func TestActivityStripsTheGlyph(t *testing.T) {
	cases := map[string]string{
		"⠧ Investigating setup": "Investigating setup",
		"✳ Ready":               "Ready",
		"✳Ready":                "Ready",
		"no glyph here":         "no glyph here",
		"":                      "",
		// Claude's default title is no summary, with or without the glyph.
		"✳ Claude Code":             "",
		"Claude Code":               "",
		"⠧ Claude Code refactoring": "Claude Code refactoring",
	}
	for in, want := range cases {
		if got := claude.Activity(in); got != want {
			t.Errorf("Activity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpinnerRanges(t *testing.T) {
	for _, r := range []rune{0x2800, 0x28FF, 0x25D0, 0x25D3} {
		if !claude.IsSpinner(r) {
			t.Errorf("IsSpinner(%U) = false, want true", r)
		}
	}
	for _, r := range []rune{0x27FF, 0x2900, 0x25CF, 0x25D4, 0x2733} {
		if claude.IsSpinner(r) {
			t.Errorf("IsSpinner(%U) = true, want false", r)
		}
	}
}

// The probe is Resumable, which is detected by type assertion and so is not
// checked by the compiler anywhere else.
var _ revier.Resumable = (*claude.Probe)(nil)

// agentsJSON is `claude agents --json` as Claude Code 2.1 prints it: a
// background session with no pid, and interactive sessions with the pid of
// their process.
const agentsJSON = `[
  {"id": "22e0eb3a", "cwd": "/w", "kind": "background", "startedAt": 1786802305922,
   "sessionId": "22e0eb3a-0c4c-4857-ba01-5503c5ccee83", "name": "review", "state": "blocked"},
  {"pid": 583601, "cwd": "/a", "kind": "interactive", "startedAt": 1789118141564,
   "sessionId": "39120ccd-8abd-434a-92c7-83ecac81fc32", "name": "a", "status": "idle"},
  {"pid": 610851, "cwd": "/a", "kind": "interactive", "startedAt": 1789118532755,
   "sessionId": "b8f365f0-07ae-4464-867f-f8ac02c2f467", "name": "b", "status": "waiting"}
]`

func agents(out string, err error) func(context.Context) ([]byte, error) {
	return func(context.Context) ([]byte, error) { return []byte(out), err }
}

// A pane is matched to its conversation by the pid of its process and nothing
// else, so two agents in one directory are told apart. The conversations come
// back in the panels' order, each with the directory Claude lists it in, and
// zero where nothing matched.
func TestSessions(t *testing.T) {
	p := &claude.Probe{}
	p.SetAgents(agents(agentsJSON, nil))
	panels := []revier.Panel{
		{ID: "1", PID: 610851},
		{ID: "2", PID: 999999}, // a pid Claude does not list: claude typed into a shell
		{ID: "3", PID: 583601}, // same directory as the first, a different conversation
		{ID: "4"},              // a runtime that could not see the process
	}
	got, err := p.Sessions(context.Background(), panels)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	want := []revier.Conversation{
		{ID: "b8f365f0-07ae-4464-867f-f8ac02c2f467", Dir: "/a"},
		{},
		{ID: "39120ccd-8abd-434a-92c7-83ecac81fc32", Dir: "/a"},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Sessions = %+v, want %+v", got, want)
	}
}

// A background session has no pid. It must not become the conversation of a
// panel that reports none.
func TestSessionsIgnoresBackgroundSessions(t *testing.T) {
	p := &claude.Probe{}
	p.SetAgents(agents(agentsJSON, nil))
	got, err := p.Sessions(context.Background(), []revier.Panel{{ID: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != (revier.Conversation{}) {
		t.Errorf("a panel with no pid was given %+v", got[0])
	}
}

// Claude Code missing, or printing something else, is the probe's error to
// return; the core turns it into agents that restore empty.
func TestSessionsReportsWhatItCannotRead(t *testing.T) {
	for name, run := range map[string]func(context.Context) ([]byte, error){
		"the command failed": agents("", errors.New("exec: claude: not found")),
		"not json":           agents("Usage: claude agents [options]", nil),
	} {
		t.Run(name, func(t *testing.T) {
			p := &claude.Probe{}
			p.SetAgents(run)
			if _, err := p.Sessions(context.Background(), []revier.Panel{{PID: 583601}}); err == nil {
				t.Error("Sessions returned no error")
			}
		})
	}
}

// The configured arguments are kept: a project that runs its agent with a
// model flag keeps the flag across a restore.
func TestResumeCommand(t *testing.T) {
	p := &claude.Probe{}
	cases := []struct {
		name string
		spec revier.PanelSpec
		want []string
	}{
		{"the bare harness", revier.PanelSpec{}, []string{"claude", "--resume", "abc-123"}},
		{
			"the project's own flags",
			revier.PanelSpec{Command: []string{"claude", "--model", "opus"}},
			[]string{"claude", "--model", "opus", "--resume", "abc-123"},
		},
		{
			"an npm install, under node",
			revier.PanelSpec{Command: []string{"node", "/home/hans/.npm-global/bin/claude"}},
			[]string{"node", "/home/hans/.npm-global/bin/claude", "--resume", "abc-123"},
		},
		{
			"a continue flag, dropped",
			revier.PanelSpec{Command: []string{"claude", "--continue", "--model", "opus", "-c"}},
			[]string{"claude", "--model", "opus", "--resume", "abc-123"},
		},
		{
			"a resume of its own, with and without a value",
			revier.PanelSpec{Command: []string{"claude", "-r", "old", "--resume", "--model", "opus", "--resume=older"}},
			[]string{"claude", "--model", "opus", "--resume", "abc-123"},
		},
		{
			"a session id of its own",
			revier.PanelSpec{Command: []string{"claude", "--session-id", "0000", "--session-id=1111"}},
			[]string{"claude", "--resume", "abc-123"},
		},
		{
			"a wrapper's own flags, kept",
			revier.PanelSpec{Command: []string{"bash", "-c", "exec -a claude bash \"$0\" \"$@\"", "/tmp/agent.sh", "-c"}},
			[]string{"bash", "-c", "exec -a claude bash \"$0\" \"$@\"", "/tmp/agent.sh", "-c", "--resume", "abc-123"},
		},
		{
			"flags after claude under node, dropped",
			revier.PanelSpec{Command: []string{"node", "-r", "tsx", "/opt/bin/claude", "-c"}},
			[]string{"node", "-r", "tsx", "/opt/bin/claude", "--resume", "abc-123"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.ResumeCommand(tc.spec, "abc-123")
			if len(got) != len(tc.want) {
				t.Fatalf("ResumeCommand = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ResumeCommand = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// The spec's own slice must survive: it is the project's, read again on every
// later keypress.
func TestResumeCommandDoesNotEditTheSpec(t *testing.T) {
	spec := revier.PanelSpec{Command: []string{"claude", "--model", "opus"}}
	(&claude.Probe{}).ResumeCommand(spec, "abc-123")
	if len(spec.Command) != 3 {
		t.Errorf("the spec now reads %v, want it untouched", spec.Command)
	}
}
