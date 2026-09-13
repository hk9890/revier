package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// An agent that ran in a tab target is named with its own reason. The reason
// for a dropped agent - no agent panel declared - would send the reader to look
// for a declaration a tab cannot have.
func TestResumeNoteNamesAnAgentInATabTarget(t *testing.T) {
	note := resumeNote("open", []core.AgentOutcome{core.AgentInTab}, errors.New("unused"))
	if want := "open, 1 agent not resumed: it ran in a tab target"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
	if strings.Contains(note, "no agent panel declared") {
		t.Errorf("note %q gives the dropped agent's reason", note)
	}
}
