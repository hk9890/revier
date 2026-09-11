package main

import (
	"fmt"
	"os/exec"
	"testing"
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
