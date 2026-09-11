package main

import (
	"fmt"
	"os/exec"
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
