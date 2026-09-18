package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// A tool a host drives failed. That is revier's own failure whatever status
// the tool exited with: the message is printed, and the status is revier's,
// so a tool's 3 cannot pass for the no-project status that the desktop
// binding turns into the picker.
func TestAToolFailureIsPrintedWithRevierStatus(t *testing.T) {
	err := fmt.Errorf("tmux: open home: tmux new-window: %w: no server running", &exec.ExitError{})
	if status, say := outcome(err); status != 1 || !say {
		t.Errorf("outcome = %d, %v; want 1, with the message printed", status, say)
	}
}

// `revier keys install` that left keys behind ends with its own status, 4,
// and says nothing more: the plan it printed has already named every key.
func TestAnIncompleteKeysInstallEndsWithStatusFourAndNoMoreWords(t *testing.T) {
	if status, say := outcome(fmt.Errorf("install: %w", errKeysIncomplete)); status != exitKeysIncomplete || say {
		t.Errorf("outcome = %d, %v; want %d, silent", status, say, exitKeysIncomplete)
	}
}

// An argument a command takes no place for is refused, not dropped: `revier
// new my project` would otherwise write a project called my.
func TestAnExtraArgumentIsRefused(t *testing.T) {
	root := t.TempDir()
	// No host is probed: the scratch config names none, so nothing here can
	// reach the desktop this runs on.
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[hosts]\nruntime = [\"none\"]\nwindow = [\"none\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
	t.Chdir(t.TempDir())

	for _, args := range [][]string{
		{"new", "my", "project"},
		{"open", "my", "project"},
		{"go", "editor", "extra"},
		{"run", "say", "extra"},
		{"attach", "extra"},
		{"status", "extra"},
	} {
		err := run(args)
		if err == nil || !strings.Contains(err.Error(), "usage: revier "+args[0]) {
			t.Errorf("revier %s: err = %v, want the usage", strings.Join(args, " "), err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "projects")); len(entries) > 0 {
		t.Errorf("a refused command wrote %d project files", len(entries))
	}
}

// An action's own failure passes its status on and adds nothing: the action
// has already said why, on the terminal it was given.
func TestAnActionsFailurePassesItsStatusOn(t *testing.T) {
	exit := &exec.ExitError{}
	status, say := outcome(fmt.Errorf("%w: %w", errActionFailed, exit))
	if say || status != exit.ExitCode() {
		t.Errorf("outcome = %d, %v; want %d with nothing printed", status, say, exit.ExitCode())
	}
}

// `revier list` names the agent the TUI row names: the first in the worst
// state, whatever that state is. An agent whose probe could not read it is
// still an agent, and what it was doing is still worth the column.
func TestListSumsAProjectUpByTheSameAgentAsTheTUI(t *testing.T) {
	agent := func(s revier.Status, activity string) revier.AgentView {
		return revier.AgentView{State: revier.AgentState{Status: s, Activity: activity}}
	}
	for _, tc := range []struct {
		agents []revier.AgentView
		want   string
	}{
		{[]revier.AgentView{agent(revier.StatusAttention, "first"), agent(revier.StatusAttention, "second")}, "attention: first"},
		{[]revier.AgentView{agent(revier.StatusUnknown, "reading")}, "unknown: reading"},
	} {
		if got := agentSummary(revier.ProjectView{Agents: tc.agents}); got != tc.want {
			t.Errorf("agentSummary = %q, want %q", got, tc.want)
		}
	}
}
