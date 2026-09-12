package claude_test

import (
	"context"
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

// The id comes off the panel and nowhere else. A pane with no variable holds
// no conversation this probe will name: the only other signal is the newest
// transcript for the pane's directory, which cannot tell two agents in one
// repository apart, and resuming the wrong conversation is worse than
// resuming none.
func TestSession(t *testing.T) {
	p := &claude.Probe{}
	cases := []struct {
		name  string
		panel revier.Panel
		want  revier.SessionID
		held  bool
	}{
		{"the hook has run", revier.Panel{Vars: map[string]string{"CS_SESSION": "abc-123"}}, "abc-123", true},
		{"no hook installed", revier.Panel{Vars: map[string]string{"CS_TAB": "1"}}, "", false},
		{"set but empty", revier.Panel{Vars: map[string]string{"CS_SESSION": ""}}, "", false},
		{"no variables at all", revier.Panel{}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, held, err := p.Session(context.Background(), tc.panel)
			if err != nil {
				t.Fatalf("Session: %v", err)
			}
			if got != tc.want || held != tc.held {
				t.Errorf("Session = (%q, %v), want (%q, %v)", got, held, tc.want, tc.held)
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
