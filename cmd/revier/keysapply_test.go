package main

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

func installPlan() core.KeyPlan {
	return core.KeyPlan{Desktop: "gnome", Steps: []core.KeyStep{
		{Chord: "alt+space", Target: "picker", Action: core.KeyOK},
		{Chord: "ctrl+shift+o", Target: "editor", Action: core.KeyTakeOver, HeldBy: "to-editor-window"},
		{Chord: "ctrl+shift+u", Target: "home", Action: core.KeyTakeOver, HeldBy: "to-session-terminal"},
		{Chord: "ctrl+shift+i", Target: "web", Action: core.KeyCreate},
	}}
}

func plan(p core.KeyPlan, sub string, dryRun, force bool) string {
	var b strings.Builder
	printPlan(&b, p, sub, dryRun, force)
	return b.String()
}

// Every key gets a line on every run. A run that printed only what it changed
// would leave the reader counting to find out about the rest.
func TestEveryKeyGetsALineWhetherOrNotItChanged(t *testing.T) {
	out := plan(installPlan(), "install", false, false)
	for _, want := range []string{"Alt+Space", "Ctrl+Shift+O", "Ctrl+Shift+U", "Ctrl+Shift+I"} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no line for %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "revier:"); n != 4 {
		t.Errorf("got %d named keys, want 4:\n%s", n, out)
	}
}

// A key nobody took says who has it and what takes it. Without both, the user
// has to guess why nothing happened.
func TestASkippedKeyNamesItsHolderAndTheWayPastIt(t *testing.T) {
	out := plan(installPlan(), "install", false, false)
	for _, want := range []string{
		"skipped: to-editor-window holds it",
		"skipped: to-session-terminal holds it",
		"2 keys are held by something else. --force takes them.",
		"keybindings: no (1/4 installed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

func TestAForcedRunSaysWhatItSwitchedOff(t *testing.T) {
	p := installPlan()
	for i := range p.Steps {
		p.Steps[i].Done = p.Steps[i].Action.Writes()
	}
	out := plan(p, "install", false, true)

	for _, want := range []string{
		"created, to-editor-window switched off",
		"created, to-session-terminal switched off",
		"keybindings: yes (4/4 installed)",
		"revier keys uninstall",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--force takes") {
		t.Errorf("it still offers --force after a forced run:\n%s", out)
	}
}

// A dry run has to read as something that has not happened yet, or it is
// indistinguishable from a run that did.
func TestADryRunSaysNothingHappened(t *testing.T) {
	out := plan(installPlan(), "install", true, true)
	if !strings.Contains(out, "dry run: nothing is changed") {
		t.Errorf("no dry-run notice:\n%s", out)
	}
	if !strings.Contains(out, "would be created") {
		t.Errorf("a dry run reports work in the past tense:\n%s", out)
	}
}

// Clearing a GNOME setting is the one action nothing else puts back, so the
// command that does is printed beside it.
func TestClearingADesktopDefaultPrintsItsUndo(t *testing.T) {
	undo := "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu"
	p := core.KeyPlan{Steps: []core.KeyStep{{
		Chord: "alt+space", Target: "picker", Action: core.KeyClear,
		HeldBy: "org.gnome.desktop.wm.keybindings activate-window-menu",
		Undo:   undo, Done: true,
	}}}
	out := plan(p, "install", false, true)
	if !strings.Contains(out, "undo: "+undo) {
		t.Errorf("the undo is not printed:\n%s", out)
	}
}

// A failure names its key and its reason. One line saying the run failed would
// not say which key to look at.
func TestAFailureIsReportedAgainstItsOwnKey(t *testing.T) {
	p := installPlan()
	p.Steps[3].Err = "dconf is not answering"
	out := plan(p, "install", false, true)
	if !strings.Contains(out, "failed: dconf is not answering") {
		t.Errorf("the reason is not printed:\n%s", out)
	}
	if !strings.Contains(out, "Ctrl+Shift+I") {
		t.Errorf("the failing key is not named:\n%s", out)
	}
}

func TestUninstallCountsWhatItReleased(t *testing.T) {
	p := core.KeyPlan{Steps: []core.KeyStep{
		{Chord: "alt+space", Target: "picker", Action: core.KeyRemove, HeldBy: "revier: picker", Done: true},
		{Chord: "ctrl+shift+u", Target: "home", Action: core.KeyAbsent},
	}}
	out := plan(p, "uninstall", false, false)
	for _, want := range []string{"removed", "not installed", "keys: 1 of 1 released"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}
}

// A step that failed after clearing the desktop setting is the one whose
// reader most needs the way back. Printing the undo only on success would hide
// it exactly where it matters.
func TestTheUndoIsPrintedEvenWhenTheStepFailed(t *testing.T) {
	undo := "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu"
	p := core.KeyPlan{Steps: []core.KeyStep{{
		Chord: "alt+space", Target: "picker", Action: core.KeyClear,
		HeldBy: "org.gnome.desktop.wm.keybindings activate-window-menu",
		Undo:   undo, Err: "dconf is not answering",
	}}}
	out := plan(p, "install", false, true)
	if !strings.Contains(out, "undo: "+undo) {
		t.Errorf("the undo is not printed after a failure:\n%s", out)
	}
}

// A key nobody was allowed to take was not cleared, so there is nothing to
// undo and the line would be a false alarm.
func TestNoUndoIsPrintedForAKeyForceWasNotGivenFor(t *testing.T) {
	p := core.KeyPlan{Steps: []core.KeyStep{{
		Chord: "alt+space", Target: "picker", Action: core.KeyClear,
		HeldBy: "org.gnome.desktop.wm.keybindings activate-window-menu",
		Undo:   "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu",
	}}}
	if out := plan(p, "install", false, false); strings.Contains(out, "undo:") {
		t.Errorf("an undo is offered for a key nothing was taken from:\n%s", out)
	}
}
