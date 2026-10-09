package revier_test

import (
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// A panel runs a program when the program is its command, or the script an
// interpreter was started on. An argument is data: a probe claiming `nvim
// claude` would have `revier agent prompt` type into the editor.
func TestPanelRunsTheProgramNotAnArgument(t *testing.T) {
	cases := []struct {
		name    string
		command []string
		want    bool
	}{
		{"the command", []string{"claude"}, true},
		{"by absolute path", []string{"/home/u/.local/bin/claude", "--resume"}, true},
		{"a script under node", []string{"node", "/home/u/.npm-global/bin/claude"}, true},
		{"a script under node, flags first", []string{"/usr/bin/node", "--no-warnings", "/opt/claude/cli/claude"}, true},
		{"a module under python", []string{"python3", "-m", "claude"}, true},
		{"a file an editor holds", []string{"nvim", "claude"}, false},
		{"a path a pager reads", []string{"less", "notes/claude"}, false},
		{"another script under node", []string{"node", "server.js", "claude"}, false},
		{"no command", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (revier.Panel{Command: tc.command}).Runs("claude"); got != tc.want {
				t.Errorf("Runs(claude) on %q = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

// One list of shells for every runtime, so a panel has the same kind whichever
// host reports it. A login shell carries a dash in argv[0], and a harness is
// never a shell: a probe says what it is.
func TestIsShellKnowsAShellByItsProgram(t *testing.T) {
	cases := []struct {
		program string
		want    bool
	}{
		{"zsh", true},
		{"-zsh", true},
		{"/usr/bin/bash", true},
		{"sh", true},
		{"fish", true},
		{"dash", true},
		{"ksh", true},
		{"claude", false},
		{"/home/u/.local/bin/opencode", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := revier.IsShell(tc.program); got != tc.want {
			t.Errorf("IsShell(%q) = %v, want %v", tc.program, got, tc.want)
		}
	}
}
