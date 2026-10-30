package tui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// A restore whose targets all opened, and whose return to the project it was
// left on failed, names the return and the project: the restore did not fail.
func TestAFailedReturnHomeIsNotAFailedRestore(t *testing.T) {
	var sc sessionsScreen
	err := sc.restored(restoredMsg{id: "work", current: "alpha", back: errors.New("could not return to alpha: boom")})
	if got, want := short(err, true), "return home failed: alpha (alt+l for details)"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
}

// The footer is one line also for an action whose name has a line break.
func TestAFailedOperationIsOneLine(t *testing.T) {
	err := fail("action sync\nall", "alpha", errors.New("exit status 1"))
	if got, want := short(err, false), "action sync; all failed: alpha"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
}

// A go to an agent that no panel here shows is a refusal: the footer keeps its
// own text, which says why.
func TestAnAgentNoPanelShowsIsARefusal(t *testing.T) {
	for _, err := range []error{core.ErrAgentElsewhere, core.ErrAgentGone, core.ErrNoHome} {
		if !refused(core.Project{}, fmt.Errorf("alpha: %w", err)) {
			t.Errorf("%v is named a failure", err)
		}
	}
	if refused(core.Project{}, errors.New("kitty is not running")) {
		t.Error("a tool's failure is named a refusal")
	}
}

// A shutdown that ends while the logs screen stands over its wizard leaves the
// logs screen up, and Esc goes back to the surface, not to a finished wizard.
func TestAShutdownThatEndsUnderTheLogsScreenClosesItsWizard(t *testing.T) {
	m := bare(Model{})
	m.dialog, m.logs.from = dialogLogs, dialogShutdown
	next, _ := m.closed(shutdownMsg{}, false)
	got := next.(Model)
	if got.dialog != dialogLogs || got.logs.from != dialogNone {
		t.Errorf("dialog %v over %v, want the logs screen over the surface", got.dialog, got.logs.from)
	}
}

// A failed clone logged its own clone line, so the screen lists it once: the
// footer writes no tui line of its own.
func TestAFailedCloneIsLoggedOnce(t *testing.T) {
	if !loggedAlready(clonedMsg{err: errors.New("boom")}) {
		t.Error("a failed clone is logged by the footer too")
	}
}
