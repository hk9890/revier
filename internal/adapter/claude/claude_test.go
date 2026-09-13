package claude_test

import (
	"context"
	"errors"
	"testing"

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
		{"marker variable", revier.Panel{Vars: map[string]string{"CS_TAB": "1"}}, true},
		{"marker unset", revier.Panel{Vars: map[string]string{"CS_TAB": "0"}}, false},
		{"foreground command", revier.Panel{Command: []string{"claude"}}, true},
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

// The glyph is the level. CS_STATE is consulted only for attention, and never
// to claim work: a stale "busy" must not paint a running state on an idle pane.
func TestInspectStatus(t *testing.T) {
	p := &claude.Probe{}
	cases := []struct {
		name  string
		title string
		state string
		want  revier.Status
	}{
		{"braille spinner means running", "⠧ Investigating setup", "", revier.StatusRunning},
		{"circle spinner means running", "◑ Refactoring", "", revier.StatusRunning},
		{"at-rest glyph means idle", "✳ Ready", "", revier.StatusIdle},
		{"no glyph means idle", "zsh", "", revier.StatusIdle},
		{"empty title means idle", "", "", revier.StatusIdle},
		{"attn without a spinner is attention", "✳ Ready", "attn", revier.StatusAttention},
		{"a stale busy never claims running", "✳ Ready", "busy", revier.StatusIdle},
		{"waiting is idle", "✳ Ready", "waiting", revier.StatusIdle},
		{"the spinner wins over attn", "⠧ Working", "attn", revier.StatusRunning},
		{"an unknown glyph degrades to idle", "☀ Odd", "", revier.StatusIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panel := revier.Panel{Title: tc.title, Vars: map[string]string{"CS_STATE": tc.state}}
			got, err := p.Inspect(context.Background(), panel)
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
// else, so two agents in one directory are told apart. The ids come back in the
// panels' order, empty where nothing matched.
func TestSessions(t *testing.T) {
	p := &claude.Probe{Agents: agents(agentsJSON, nil)}
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
	want := []revier.SessionID{"b8f365f0-07ae-4464-867f-f8ac02c2f467", "", "39120ccd-8abd-434a-92c7-83ecac81fc32", ""}
	if len(got) != len(want) {
		t.Fatalf("Sessions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("panel %s = %q, want %q", panels[i].ID, got[i], want[i])
		}
	}
}

// A background session has no pid. It must not become the conversation of a
// panel that reports none.
func TestSessionsIgnoresBackgroundSessions(t *testing.T) {
	p := &claude.Probe{Agents: agents(agentsJSON, nil)}
	got, err := p.Sessions(context.Background(), []revier.Panel{{ID: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "" {
		t.Errorf("a panel with no pid was given %q", got[0])
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
			p := &claude.Probe{Agents: run}
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
