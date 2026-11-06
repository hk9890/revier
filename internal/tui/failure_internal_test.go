package tui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
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

// A clone failure is for the git_url and the path it was tried with: once
// the file says otherwise, the pane offers the clone afresh.
func TestAFailedCloneIsForgottenWhenTheFileChanges(t *testing.T) {
	p := revier.Project{Name: "alpha", Path: "/p/alpha", GitURL: "https://github.com/o/r/tree/main/x"}
	next, _ := bare(Model{}).cloned(clonedMsg{project: core.Project{Project: p}, err: errors.New("boom")})
	m := next.(Model)
	if last, ok := m.lastClone(p); !ok || last.why != "boom" || last.ran {
		t.Fatalf("lastClone = %+v, %v; want the failure, refused before git", last, ok)
	}
	p.GitURL = "https://github.com/o/r"
	if last, ok := m.lastClone(p); ok {
		t.Errorf("lastClone = %+v after git_url changed, want nothing", last)
	}
}

// A close that finds nothing to close is a normal outcome its handler logs
// at INFO, so the footer writes no ERROR line for it.
func TestACloseOfNothingIsNotLoggedAsAFailure(t *testing.T) {
	if !loggedAlready(closePlannedMsg{}) {
		t.Error("a close with nothing to close is logged as a tui ERROR")
	}
}
