//go:build live

// Layer L4 for the status runner: a real command, run through sh.
package tui

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// A command runs without a shell, and its output is read as a status.
func TestStatusRunsTheCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not installed")
	}
	s := core.Status{At: core.StatusProject, Run: []string{"sh", "-c", `echo "$REVIER_PROJECT"`}}
	msg := runStatus(alphaInfo, s, []string{"REVIER_PROJECT=alpha"})().(statusMsg)
	if msg.err != nil || msg.key != alphaInfo || !sameStatus(msg.ran, s) || !slices.Equal(texts(msg.segments), []string{"alpha"}) {
		t.Errorf("runStatus = %+v, want alpha", msg)
	}
}
