package main

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

func installPlan() core.KeyPlan {
	return core.KeyPlan{Desktop: "gnome", Steps: []core.KeyStep{
		{Chord: "alt+space", Target: "picker", Action: core.KeyOK},
		{Chord: "ctrl+shift+o", Target: "editor", Action: core.KeyTakeOver, Own: core.KeyCreate, HeldBy: "to-editor-window"},
		{Chord: "ctrl+shift+u", Target: "home", Action: core.KeyTakeOver, Own: core.KeyCreate, HeldBy: "to-session-terminal"},
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

// One held key is still said, in the singular.
func TestOneSkippedKeyIsStillSaid(t *testing.T) {
	p := installPlan()
	p.Steps = p.Steps[:2] // the picker, and the one editor key something holds
	out := plan(p, "install", false, false)
	if want := "1 key is held by something else. --force takes it."; !strings.Contains(out, want) {
		t.Errorf("output does not say %q:\n%s", want, out)
	}
	if strings.Contains(plan(p, "install", false, true), "held by something else") {
		t.Errorf("a forced run has nothing held:\n%s", out)
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

// A key whose revier shortcut is already there and right, with an old one
// still firing beside it, is taken by switching the old one off. Saying
// "created" there would report work that did not happen.
func TestTakingAKeyRevierAlreadyHoldsDoesNotSayCreated(t *testing.T) {
	p := core.KeyPlan{Steps: []core.KeyStep{{
		Chord: "alt+space", Target: "picker", Action: core.KeyTakeOver, Own: core.KeyOK,
		HeldBy: "start-session-selector",
	}}}
	if out := plan(p, "install", true, true); !strings.Contains(out, "start-session-selector would be switched off") ||
		strings.Contains(out, "created") {
		t.Errorf("dry run:\n%s", out)
	}
	p.Steps[0].Done = true
	if out := plan(p, "install", false, true); !strings.Contains(out, "picker  start-session-selector switched off") {
		t.Errorf("run:\n%s", out)
	}
	p.Steps[0].Own = core.KeyUpdate
	if out := plan(p, "install", false, true); !strings.Contains(out, "command rewritten, start-session-selector switched off") {
		t.Errorf("stale shortcut:\n%s", out)
	}
}

// Clearing a GNOME setting is the one action nothing else puts back, so the
// command that does is printed beside it.
func TestClearingADesktopDefaultPrintsItsUndo(t *testing.T) {
	undo := "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu"
	p := core.KeyPlan{Steps: []core.KeyStep{{
		Chord: "alt+space", Target: "picker", Action: core.KeyClear, Own: core.KeyCreate,
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
		Chord: "alt+space", Target: "picker", Action: core.KeyClear, Own: core.KeyCreate,
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
		Chord: "alt+space", Target: "picker", Action: core.KeyClear, Own: core.KeyCreate,
		HeldBy: "org.gnome.desktop.wm.keybindings activate-window-menu",
		Undo:   "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu",
	}}}
	if out := plan(p, "install", false, false); strings.Contains(out, "undo:") {
		t.Errorf("an undo is offered for a key nothing was taken from:\n%s", out)
	}
}

// A key is printed as the settings window spells it. `<Alt>+` once parsed to a
// chord with an empty part, which the capital letter panicked on; and a
// capital taken off the first byte splits a letter that is more than one.
func TestAChordPrintsWhateverItsKeyIs(t *testing.T) {
	plus, err := core.ParseChord("<Alt>+")
	if err != nil {
		t.Fatalf("ParseChord: %v", err)
	}
	for _, tc := range []struct {
		in   core.Chord
		want string
	}{
		{plus, "Alt+Plus"},
		{"alt+,", "Alt+,"},
		{"ctrl+é", "Ctrl+É"},
		{"alt++", "Alt++"},
	} {
		if got := prettyChord(tc.in); got != tc.want {
			t.Errorf("prettyChord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
