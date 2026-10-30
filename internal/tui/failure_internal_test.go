package tui

import (
	"errors"
	"testing"
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

// A failed clone logged its own clone line, so the screen lists it once: the
// footer writes no tui line of its own.
func TestAFailedCloneIsLoggedOnce(t *testing.T) {
	if !loggedAlready(clonedMsg{err: errors.New("boom")}) {
		t.Error("a failed clone is logged by the footer too")
	}
}
