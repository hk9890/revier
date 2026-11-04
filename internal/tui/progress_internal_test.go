package tui

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// A cancel during the wait after the last close ends the wait, not a step:
// the line says so rather than naming a step that runs.
func TestACancelledWaitSaysTheWaitEnds(t *testing.T) {
	w := walk{cancelled: true, last: core.Progress{Phase: core.PhaseWait}}
	if line := w.line(); !strings.Contains(line, "no longer waiting") || !strings.Contains(line, "ctrl+c again quits") {
		t.Errorf("line = %q, want the wait ended and the way out said", line)
	}
	w.last = core.Progress{Phase: core.PhaseClose, N: 1, Of: 2, Label: "editor"}
	if line := w.line(); !strings.Contains(line, "once the step that runs is done") {
		t.Errorf("line = %q, want the step that runs to finish", line)
	}
}
