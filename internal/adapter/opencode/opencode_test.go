package opencode_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/adapter/opencode"
	"github.com/hk9890/revier/pkg/revier"
)

func TestMatch(t *testing.T) {
	p := &opencode.Probe{}
	cases := []struct {
		name  string
		panel revier.Panel
		want  bool
	}{
		{"foreground command", revier.Panel{Command: []string{"opencode"}}, true},
		{"absolute path", revier.Panel{Command: []string{"/home/user/.local/bin/opencode", "--continue"}}, true},
		{"a claude pane is not opencode", revier.Panel{Command: []string{"claude"}, Vars: map[string]string{"CS_TAB": "1"}}, false},
		{"a shell", revier.Panel{Command: []string{"zsh"}}, false},
		{"empty", revier.Panel{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Match(tc.panel); got != tc.want {
				t.Errorf("Match = %v, want %v", got, tc.want)
			}
		})
	}
}

// opencode exposes no state signal (package comment), so the probe reports
// presence and never guesses.
func TestInspectReportsUnknown(t *testing.T) {
	p := &opencode.Probe{}
	for _, title := range []string{"OpenCode", "⠧ something", ""} {
		got, err := p.Inspect(context.Background(), revier.Panel{Title: title, Command: []string{"opencode"}})
		if err != nil {
			t.Fatalf("Inspect: %v", err)
		}
		if got.Status != revier.StatusUnknown || got.Harness != "opencode" {
			t.Errorf("title %q: state = %+v, want unknown/opencode", title, got)
		}
	}
}
