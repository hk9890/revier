package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

const (
	sessionSelector = `sh -lc "$HOME/setup/scripts/sessions/os-fzf-popup.sh"`
	sessionTerminal = `sh -lc "$HOME/setup/scripts/sessions/os-to-session.sh"`
	sessionEditor   = `sh -lc "$HOME/setup/scripts/sessions/os-to-editor.sh"`
)

// theShellTool is this machine before any switch-over: three custom shortcuts,
// none of them revier's.
func theShellTool() *hosttest.FakeWriter {
	return hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", sessionSelector, "start-session-selector"),
		hosttest.Custom("<Shift><Control>u", sessionTerminal, "to-session-terminal"),
		hosttest.Custom("<Shift><Control>o", sessionEditor, "to-editor-window"),
	)
}

func planInstall(t *testing.T, w *hosttest.FakeWriter, projects ...core.Project) (*core.Core, core.KeyPlan) {
	t.Helper()
	c := &core.Core{KeyBinder: w}
	plan, err := c.PlanInstallKeys(context.Background(), projects, "alt+space")
	if err != nil {
		t.Fatalf("PlanInstallKeys: %v", err)
	}
	return c, plan
}

func step(t *testing.T, plan core.KeyPlan, chord core.Chord) core.KeyStep {
	t.Helper()
	for _, s := range plan.Steps {
		if s.Chord == chord {
			return s
		}
	}
	t.Fatalf("no step for %q in %+v", chord, plan.Steps)
	return core.KeyStep{}
}

// Without force, install adds only what is free. A key somebody else holds is
// left exactly as it was: revier does not take a key nobody gave it.
func TestInstallWithoutForceTakesNothingFromAnybody(t *testing.T) {
	w := theShellTool()
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	if got := step(t, plan, "ctrl+shift+u").Action; got != core.KeyTakeOver {
		t.Errorf("ctrl+shift+u action = %q, want a take over", got)
	}
	done := c.ApplyKeys(context.Background(), plan, false)

	for _, s := range done.Steps {
		if s.Action.Blocked() && s.Done {
			t.Errorf("%s was taken without --force", s.Chord)
		}
	}
	if len(w.Calls) != 0 {
		t.Errorf("the desktop was changed: %v", w.Calls)
	}
	// The shell tool's shortcuts are untouched, and still the ones that fire.
	for _, chord := range []string{"<Alt>space", "<Shift><Control>u", "<Shift><Control>o"} {
		held := w.Held(chord)
		if len(held) != 1 || strings.Contains(held[0].Command, "revier") {
			t.Errorf("%s is held by %+v, want the shell tool's own shortcut", chord, held)
		}
	}
}

// A machine where some keys are free and some are not gets the free ones. All
// or nothing would leave a user with no keys because of one they cannot have.
func TestInstallWithoutForceStillTakesTheFreeKeys(t *testing.T) {
	w := theShellTool()
	c, plan := planInstall(t, w, keyProject(t, "revier"), theWebProject(t))

	done := c.ApplyKeys(context.Background(), plan, false)

	web := step(t, done, "ctrl+shift+i")
	if web.Action != core.KeyCreate || !web.Done {
		t.Fatalf("the free key was not taken: %+v", web)
	}
	held := w.Held("<Shift><Control>i")
	if len(held) != 1 || held[0].Command != `sh -lc "revier go web --picker"` {
		t.Errorf("ctrl+shift+i is held by %+v, want revier's own shortcut", held)
	}
	if done.Installed() != 1 {
		t.Errorf("installed %d keys, want 1", done.Installed())
	}
}

// With force the key becomes revier's, and the shortcut that held it stops
// firing without losing a word of itself: `os init` puts it back.
func TestForceTakesTheKeyAndDestroysNothing(t *testing.T) {
	w := theShellTool()
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	done := c.ApplyKeys(context.Background(), plan, true)
	for _, s := range done.Steps {
		if !s.Done || s.Err != "" {
			t.Errorf("%s: done=%v err=%q", s.Chord, s.Done, s.Err)
		}
	}

	held := w.Held("<Shift><Control>u")
	if len(held) != 1 || held[0].Command != `sh -lc "revier go home --picker"` {
		t.Fatalf("ctrl+shift+u is held by %+v, want revier's own shortcut alone", held)
	}
	// The evicted shortcut is still there, switched off, with its command
	// intact. Nothing was stored anywhere, because nothing was destroyed.
	var evicted *revier.Binding
	for i, b := range w.Bindings {
		if b.Label == "to-session-terminal" {
			evicted = &w.Bindings[i]
		}
	}
	if evicted == nil {
		t.Fatal("the evicted shortcut was deleted, not switched off")
	}
	if evicted.Enabled {
		t.Error("the evicted shortcut still fires")
	}
	if evicted.Command != sessionTerminal {
		t.Errorf("the evicted shortcut's command = %q, want it untouched", evicted.Command)
	}
}

// A shortcut that is in the desktop's store but switched off fires on no
// press, so taking its key does not need it touched.
func TestASwitchedOffShortcutIsNotEvicted(t *testing.T) {
	off := hosttest.Custom("<Shift><Control>u", sessionTerminal, "to-session-terminal")
	off.Enabled = false
	w := hosttest.NewWriter("gnome", off)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "ctrl+shift+u")
	if s.Action != core.KeyCreate {
		t.Fatalf("action = %q, want create: nothing fires on that key", s.Action)
	}
	c.ApplyKeys(context.Background(), plan, false)
	for _, call := range w.Calls {
		if strings.HasPrefix(call, "disable") {
			t.Errorf("a shortcut that fires on no press was switched off: %v", w.Calls)
		}
	}
}

// Clearing a desktop default changes a setting of GNOME rather than another
// program's shortcut, so the step carries the one command that returns it.
func TestClearingADesktopDefaultCarriesItsUndo(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "alt+space")
	if s.Action != core.KeyClear {
		t.Fatalf("action = %q, want clear", s.Action)
	}
	for _, want := range []string{"gsettings reset", "org.gnome.desktop.wm.keybindings", "activate-window-menu"} {
		if !strings.Contains(s.Undo, want) {
			t.Errorf("undo = %q, want it to name %q", s.Undo, want)
		}
	}
	c.ApplyKeys(context.Background(), plan, true)
	if got := w.Held("<Alt>space"); len(got) != 1 || got[0].Source != revier.BindingCustom {
		t.Errorf("alt+space is held by %+v, want revier's own shortcut", got)
	}
}

// Running install twice is not a second install. The second run has nothing to
// do, so it writes nothing.
func TestInstallingTwiceChangesNothingTheSecondTime(t *testing.T) {
	w := theShellTool()
	c, plan := planInstall(t, w, keyProject(t, "revier"))
	c.ApplyKeys(context.Background(), plan, true)
	w.Calls = nil

	_, again := planInstall(t, w, keyProject(t, "revier"))
	for _, s := range again.Steps {
		if s.Action != core.KeyOK {
			t.Errorf("%s = %q on the second run, want ok", s.Chord, s.Action)
		}
	}
	c.ApplyKeys(context.Background(), again, true)
	if len(w.Calls) != 0 {
		t.Errorf("the second run changed the desktop: %v", w.Calls)
	}
}

// An older install left the target under a name it no longer has. The chord is
// revier's, so the command is rewritten where it already is, without --force
// and without a second entry.
func TestAnOldCommandIsRewrittenInPlace(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Shift><Control>o", `sh -lc "revier go edit --picker"`, "revier-editor"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "ctrl+shift+o")
	if s.Action != core.KeyUpdate {
		t.Fatalf("action = %q, want update", s.Action)
	}
	c.ApplyKeys(context.Background(), plan, false)

	held := w.Held("<Shift><Control>o")
	if len(held) != 1 {
		t.Fatalf("ctrl+shift+o is held by %d shortcuts, want 1: %+v", len(held), held)
	}
	if held[0].Command != `sh -lc "revier go editor --picker"` {
		t.Errorf("command = %q, want the current one", held[0].Command)
	}
}

// Uninstall removes what revier wrote, including a shortcut on a key nothing
// asks for any more, and touches nothing else.
func TestUninstallRemovesOnlyRevierOwnShortcuts(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier-picker"),
		hosttest.Custom("<Shift><Control>y", `sh -lc "revier go diff --picker"`, "revier-diff"),
		hosttest.Custom("<Shift><Control>o", sessionEditor, "to-editor-window"),
	)
	c := &core.Core{KeyBinder: w}
	plan, err := c.PlanUninstallKeys(context.Background(), []core.Project{keyProject(t, "revier")}, "alt+space")
	if err != nil {
		t.Fatalf("PlanUninstallKeys: %v", err)
	}
	done := c.ApplyKeys(context.Background(), plan, false)

	// The orphan is covered: a renamed target must not leave a shortcut that
	// nothing removes.
	if got := step(t, done, "ctrl+shift+y").Action; got != core.KeyRemove {
		t.Errorf("the orphan = %q, want remove", got)
	}
	for _, b := range w.Bindings {
		if strings.Contains(b.Command, "revier") {
			t.Errorf("%+v survived the uninstall", b)
		}
	}
	if len(w.Bindings) != 1 || w.Bindings[0].Label != "to-editor-window" {
		t.Errorf("bindings after uninstall = %+v, want the shell tool's own left alone", w.Bindings)
	}
}

// A user's own shortcut that runs revier among other things is theirs. Read as
// revier's, install rewrote it without --force and uninstall deleted it.
func TestAShortcutThatMerelyRunsRevierIsNotRevierOwn(t *testing.T) {
	wrapper := `sh -lc "revier go editor --picker && notify-send editor"`
	popup := `sh -c "revier popup; logger picked"`
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Shift><Control>o", wrapper, "my-editor"),
		hosttest.Custom("<Super>p", popup, "my-picker"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	if got := step(t, plan, "ctrl+shift+o").Action; got != core.KeyTakeOver {
		t.Errorf("ctrl+shift+o action = %q, want a take over: the shortcut is the user's", got)
	}
	c.ApplyKeys(context.Background(), plan, false)

	back, err := c.PlanUninstallKeys(context.Background(), []core.Project{keyProject(t, "revier")}, "alt+space")
	if err != nil {
		t.Fatalf("PlanUninstallKeys: %v", err)
	}
	c.ApplyKeys(context.Background(), back, false)

	for label, command := range map[string]string{"my-editor": wrapper, "my-picker": popup} {
		held := false
		for _, b := range w.Bindings {
			if b.Label == label && b.Command == command && b.Enabled {
				held = true
			}
		}
		if !held {
			t.Errorf("%s was rewritten, switched off or deleted: %+v", label, w.Bindings)
		}
	}
}

// A key moves between targets: home leaves ctrl+shift+u for ctrl+shift+j, and
// web takes ctrl+shift+u. web rewrites the entry revier-home in place, so
// home's new key written to the same entry would overwrite one of the two -
// and both steps would still report done.
func TestAKeyMovingBetweenTargetsLosesNeither(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Shift><Control>u", `sh -lc "revier go home --picker"`, "revier-home"),
	)
	moved := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "home", Home: true, Key: "ctrl-shift-j",
				Runtime: &revier.Realization{Launch: []string{"kitty"}, Match: revier.Match{Title: "^session:setup$"}},
			},
			{
				Name: "web", Key: "ctrl-shift-u",
				Window: &revier.Realization{Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}},
			},
		},
	})
	c, plan := planInstall(t, w, moved)
	done := c.ApplyKeys(context.Background(), plan, false)
	for _, s := range done.Steps {
		if s.Err != "" || (!s.Done && s.Action != core.KeyOK) {
			t.Errorf("%s: done=%v err=%q", s.Chord, s.Done, s.Err)
		}
	}

	for chord, command := range map[string]string{
		"<Shift><Control>j": `sh -lc "revier go home --picker"`,
		"<Shift><Control>u": `sh -lc "revier go web --picker"`,
	} {
		held := w.Held(chord)
		if len(held) != 1 || held[0].Command != command {
			t.Errorf("%s is held by %+v, want %s alone", chord, held, command)
		}
	}
}

// An entry revier names is revier's whatever it runs. An older revier's keys
// ran scripts that are gone; read as somebody else's, --force switched them
// off and wrote a second entry beside each (decisions.md D77).
func TestAnOlderReviersEntriesAreRewrittenInPlace(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier-popup"`, "revier-picker"),
		hosttest.Custom("<Shift><Control>o", `sh -lc "revier-go editor"`, "revier-editor"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))
	for _, chord := range []core.Chord{"alt+space", "ctrl+shift+o"} {
		if s := step(t, plan, chord); s.Action.Blocked() || s.Own != core.KeyUpdate {
			t.Errorf("%s: action %q, own %q; want revier's own entry updated without --force", chord, s.Action, s.Own)
		}
	}
	c.ApplyKeys(context.Background(), plan, false)

	for chord, command := range map[string]string{
		"<Alt>space":        `sh -lc "revier popup"`,
		"<Shift><Control>o": `sh -lc "revier go editor --picker"`,
	} {
		if held := w.Held(chord); len(held) != 1 || held[0].Command != command {
			t.Errorf("%s is held by %+v, want %s alone", chord, held, command)
		}
	}
	for _, b := range w.Bindings {
		if strings.Contains(b.Command, "revier-") {
			t.Errorf("%+v is left behind, want every old entry rewritten", b)
		}
	}
}

// Install then uninstall leaves the desktop as it was, except that what was
// evicted has to be switched back on by whatever wrote it. That is what `os
// init` is for, and it is why nothing is stored.
func TestUninstallDoesNotRestoreWhatItNeverWrote(t *testing.T) {
	w := theShellTool()
	c, plan := planInstall(t, w, keyProject(t, "revier"))
	c.ApplyKeys(context.Background(), plan, true)

	back, err := c.PlanUninstallKeys(context.Background(), []core.Project{keyProject(t, "revier")}, "alt+space")
	if err != nil {
		t.Fatalf("PlanUninstallKeys: %v", err)
	}
	c.ApplyKeys(context.Background(), back, false)

	if held := w.Held("<Shift><Control>u"); len(held) != 0 {
		t.Errorf("ctrl+shift+u is held by %+v, want nothing until os init runs", held)
	}
	// Every word of the shell tool's shortcut is still there to be switched on.
	found := false
	for _, b := range w.Bindings {
		if b.Label == "to-session-terminal" && b.Command == sessionTerminal {
			found = true
		}
	}
	if !found {
		t.Error("the shell tool's shortcut is gone, so os init has nothing to repair")
	}
}

// A disagreement in the configuration cannot be installed: writing either
// spelling of it is a guess. The report says the same thing and carries on,
// which is the difference between a diagnostic and a change.
func TestAPlanRefusesAConfigurationDisagreement(t *testing.T) {
	other := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "editor", Key: "ctrl-shift-e",
				Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}},
			},
		},
	})
	c := &core.Core{KeyBinder: theShellTool()}
	_, err := c.PlanInstallKeys(context.Background(), []core.Project{keyProject(t, "revier"), other}, "alt+space")
	if !errors.Is(err, core.ErrKeyConflict) {
		t.Fatalf("err = %v, want ErrKeyConflict", err)
	}
	if !strings.Contains(err.Error(), "editor") {
		t.Errorf("error = %q, want it to name the target", err)
	}
	// The same state is a report, not a refusal.
	if _, err := c.Keys(context.Background(), []core.Project{keyProject(t, "revier"), other}, "alt+space"); err != nil {
		t.Errorf("Keys refused a state it is supposed to report: %v", err)
	}
}

// The refusal names every spelling once, as a sentence: two are joined by
// "and", three by a comma and an "and".
func TestARefusalNamesEverySpellingOnce(t *testing.T) {
	editorWanting := func(name revier.ProjectName, key string) core.Project {
		return prepared(t, revier.Project{Name: name, Path: "/home/user/" + string(name), Targets: []revier.Target{
			{Name: "editor", Key: key, Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
		}})
	}
	c := &core.Core{KeyBinder: theShellTool()}
	for want, projects := range map[string][]core.Project{
		"editor wants ctrl+shift+e and editor wants ctrl+shift+o.":                            {editorWanting("setup", "ctrl-shift-e"), editorWanting("revier", "ctrl-shift-o")},
		"editor wants ctrl+shift+e, editor wants ctrl+shift+o and editor wants ctrl+shift+w.": {editorWanting("setup", "ctrl-shift-e"), editorWanting("revier", "ctrl-shift-o"), editorWanting("notes", "ctrl-shift-w")},
	} {
		_, err := c.PlanInstallKeys(context.Background(), projects, "alt+space")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to say %q", err, want)
		}
	}
}

// One key that cannot be written must not take the others with it. Stopping
// would hide the reason behind the first failure.
func TestOneFailedKeyDoesNotStopTheRest(t *testing.T) {
	w := hosttest.NewWriter("gnome")
	w.BindErr = errors.New("dconf is not answering")
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	done := c.ApplyKeys(context.Background(), plan, false)
	if len(done.Steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(done.Steps))
	}
	for _, s := range done.Steps {
		if s.Err == "" {
			t.Errorf("%s reported no error", s.Chord)
		}
		if !strings.Contains(s.Err, "dconf") {
			t.Errorf("%s error = %q, want the reason", s.Chord, s.Err)
		}
	}
}

// With --force the shortcut in the way is switched off before revier's is
// written. When the write then fails, the key may run nothing at all, and the
// error against it has to say what was switched off, or the user is left with
// a dead key and a reason that does not explain it.
func TestAFailedWriteNamesWhatItAlreadySwitchedOff(t *testing.T) {
	w := theShellTool()
	w.BindErr = errors.New("dconf is not answering")
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, c.ApplyKeys(context.Background(), plan, true), "ctrl+shift+u")
	for _, want := range []string{"dconf is not answering", "to-session-terminal", "switched off"} {
		if !strings.Contains(s.Err, want) {
			t.Errorf("error = %q, want it to name %q", s.Err, want)
		}
	}
}

// A desktop revier can read and not change must say so, rather than reporting
// a plan that quietly did nothing.
func TestADesktopThatCannotBeWrittenIsReported(t *testing.T) {
	c := &core.Core{KeyBinder: hosttest.NewKeys("gnome")}
	plan, err := c.PlanInstallKeys(context.Background(), []core.Project{keyProject(t, "revier")}, "alt+space")
	if err != nil {
		t.Fatalf("PlanInstallKeys: %v", err)
	}
	done := c.ApplyKeys(context.Background(), plan, true)
	for _, s := range done.Steps {
		if !strings.Contains(s.Err, "cannot be changed") {
			t.Errorf("%s error = %q, want it to say the desktop cannot be changed", s.Chord, s.Err)
		}
	}
}

func theWebProject(t *testing.T) core.Project {
	t.Helper()
	return prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "web", Key: "ctrl-shift-i",
				Window: &revier.Realization{Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}},
			},
		},
	})
}

// A desktop default sitting on a chord another program's shortcut also holds
// is still a setting nothing else puts back, so it carries its undo. The
// status names the shortcut, because that is what a user acts on first; what
// the step has to evict is read off the chord instead.
func TestADefaultEvictedBesideAShortcutStillCarriesItsUndo(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", sessionSelector, "start-session-selector"),
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "alt+space")
	if s.Action != core.KeyTakeOver {
		t.Fatalf("action = %q, want a take over", s.Action)
	}
	if !strings.Contains(s.Undo, "gsettings reset org.gnome.desktop.wm.keybindings activate-window-menu") {
		t.Errorf("undo = %q, want the line that returns the desktop default", s.Undo)
	}
	c.ApplyKeys(context.Background(), plan, true)
	if held := w.Held("<Alt>space"); len(held) != 1 || held[0].Source != revier.BindingCustom {
		t.Errorf("alt+space is held by %+v, want revier's own shortcut alone", held)
	}
}

// Two desktop defaults on one chord are two settings to put back, so both
// lines are printed. One of them alone would be a way back that does not work.
func TestTwoDefaultsOnOneChordCarryBothUndoLines(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
		hosttest.Builtin("<Alt>space", "org.gnome.shell.keybindings", "toggle-overview"),
	)
	_, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "alt+space")
	for _, want := range []string{"activate-window-menu", "toggle-overview"} {
		if !strings.Contains(s.Undo, want) {
			t.Errorf("undo = %q, want it to name %q", s.Undo, want)
		}
	}
}

// revier's own shortcut with an old command, and a desktop default on the same
// key. Rewriting the command alone would report the key as revier's while the
// default is still what fires, so the default has to go - and taking it needs
// --force like any other key revier was not given.
func TestAStaleShortcutUnderADesktopDefaultNeedsForce(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Shift><Control>o", `sh -lc "revier go edit --picker"`, "revier-editor"),
		hosttest.Builtin("<Shift><Control>o", "org.gnome.desktop.wm.keybindings", "toggle-maximized"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "ctrl+shift+o")
	if !s.Action.Blocked() {
		t.Fatalf("action = %q, want one that --force has to allow", s.Action)
	}
	if !strings.Contains(s.Undo, "toggle-maximized") {
		t.Errorf("undo = %q, want the line that returns the desktop default", s.Undo)
	}

	c.ApplyKeys(context.Background(), plan, false)
	for _, call := range w.Calls {
		if strings.HasPrefix(call, "disable") {
			t.Errorf("the desktop default was cleared without --force: %v", w.Calls)
		}
	}

	_, again := planInstall(t, w, keyProject(t, "revier"))
	c.ApplyKeys(context.Background(), again, true)
	held := w.Held("<Shift><Control>o")
	if len(held) != 1 || held[0].Command != `sh -lc "revier go editor --picker"` {
		t.Errorf("ctrl+shift+o is held by %+v, want revier's own shortcut alone", held)
	}
}

// Installing either spelling of a disagreement is a guess; removing what
// revier wrote is not. Refusing here would leave a user unable to give the
// keys back until the file that disagrees is fixed.
func TestUninstallDoesNotRefuseAConfigurationDisagreement(t *testing.T) {
	other := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "editor", Key: "ctrl-shift-e",
				Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}},
			},
		},
	})
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier-picker"),
	)
	c := &core.Core{KeyBinder: w}
	projects := []core.Project{keyProject(t, "revier"), other}

	plan, err := c.PlanUninstallKeys(context.Background(), projects, "alt+space")
	if err != nil {
		t.Fatalf("PlanUninstallKeys refused a state it can act on: %v", err)
	}
	c.ApplyKeys(context.Background(), plan, false)
	if len(w.Bindings) != 0 {
		t.Errorf("bindings after uninstall = %+v, want revier's own gone", w.Bindings)
	}
}

// A step removes everything of revier's on its chord, so two shortcuts on one
// chord are one step. Two would remove each entry twice and print the key
// twice.
func TestTwoOrphansOnOneChordAreOneStep(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Shift><Control>y", `sh -lc "revier go diff --picker"`, "revier-diff"),
		hosttest.Custom("<Shift><Control>y", `sh -lc "revier go review --picker"`, "revier-review"),
	)
	c := &core.Core{KeyBinder: w}
	plan, err := c.PlanUninstallKeys(context.Background(), []core.Project{keyProject(t, "revier")}, "alt+space")
	if err != nil {
		t.Fatalf("PlanUninstallKeys: %v", err)
	}

	n := 0
	for _, s := range plan.Steps {
		if s.Chord == "ctrl+shift+y" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("ctrl+shift+y got %d steps, want 1: %+v", n, plan.Steps)
	}
	c.ApplyKeys(context.Background(), plan, false)
	if len(w.Calls) != 2 {
		t.Errorf("calls = %v, want one remove per shortcut", w.Calls)
	}
}

// revier's own shortcut being right does not make the key revier's while a
// desktop default sits on the same chord: GNOME still has the setting, and a
// run that reported "ok" would be reporting a key that does not work.
func TestADesktopDefaultBesideRevierOwnShortcutIsNotOK(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier-picker"),
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "alt+space")
	if s.Action != core.KeyClear {
		t.Fatalf("action = %q, want clear: the desktop default still fires", s.Action)
	}
	if !strings.Contains(s.Undo, "activate-window-menu") {
		t.Errorf("undo = %q, want the reset for the setting it clears", s.Undo)
	}
	// And it needs --force, because clearing a GNOME setting always does.
	c.ApplyKeys(context.Background(), plan, false)
	for _, call := range w.Calls {
		if strings.HasPrefix(call, "disable") {
			t.Errorf("a GNOME setting was cleared without --force: %v", w.Calls)
		}
	}
}

// The state `os init` leaves behind: revier's shortcut is right, and the shell
// tool's is switched on again beside it, so both fire. The step takes the key
// from the shell tool alone - revier's own is never what is in the way - and
// says revier's own is already right rather than about to be created.
func TestAnotherShortcutBesideRevierOwnIsTheOnlyThingInTheWay(t *testing.T) {
	w := hosttest.NewWriter("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker"),
		hosttest.Custom("<Alt>space", sessionSelector, "start-session-selector"),
	)
	c, plan := planInstall(t, w, keyProject(t, "revier"))

	s := step(t, plan, "alt+space")
	if s.Action != core.KeyTakeOver {
		t.Fatalf("action = %q, want a take over", s.Action)
	}
	if s.HeldBy != "start-session-selector" {
		t.Errorf("held by = %q, want only the shell tool's shortcut", s.HeldBy)
	}
	if s.Own != core.KeyOK {
		t.Errorf("own = %q, want ok: revier's shortcut is already right", s.Own)
	}

	c.ApplyKeys(context.Background(), plan, true)
	held := w.Held("<Alt>space")
	if len(held) != 1 || held[0].Label != "revier: picker" {
		t.Errorf("alt+space is held by %+v, want revier's own shortcut alone", held)
	}
}
