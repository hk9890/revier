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

// keyProject is a project shaped like the ones on this machine: a home target
// and an editor target, each on the chord the shell session tool uses.
func keyProject(t *testing.T, name revier.ProjectName) core.Project {
	t.Helper()
	return prepared(t, revier.Project{
		Name: name,
		Path: "/home/user/dev/github/" + string(name),
		Targets: []revier.Target{
			{
				Name: "home", Home: true, Key: "ctrl-shift-u",
				Runtime: &revier.Realization{
					Launch: []string{"kitty"},
					Match:  revier.Match{Title: "^session:" + string(name) + "$"},
				},
			},
			{
				Name: "editor", Key: "ctrl-shift-o",
				Window: &revier.Realization{
					Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
				},
			},
		},
	})
}

func keysOf(t *testing.T, binder revier.KeyBinder, projects ...core.Project) core.KeyReport {
	t.Helper()
	c := &core.Core{KeyBinder: binder}
	report, err := c.Keys(context.Background(), projects, "alt+space")
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	return report
}

func row(t *testing.T, report core.KeyReport, chord core.Chord) core.KeyRow {
	t.Helper()
	for _, r := range report.Rows {
		if r.Chord == chord {
			return r
		}
	}
	t.Fatalf("no row for %q in %+v", chord, report.Rows)
	return core.KeyRow{}
}

// The state on this machine before any switch-over: the shell session tool
// holds all three chords, and none of them is revier's.
func TestEveryWantedChordIsReportedTaken(t *testing.T) {
	binder := hosttest.NewKeys("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "$HOME/setup/scripts/sessions/os-fzf-popup.sh"`, "start-session-selector"),
		hosttest.Custom("<Shift><Control>u", `sh -lc "$HOME/setup/scripts/sessions/os-to-session.sh"`, "to-session-terminal"),
		hosttest.Custom("<Shift><Control>o", `sh -lc "$HOME/setup/scripts/sessions/os-to-editor.sh"`, "to-editor-window"),
	)
	report := keysOf(t, binder, keyProject(t, "revier"))

	if report.Desktop != "gnome" {
		t.Errorf("desktop = %q, want gnome", report.Desktop)
	}
	if len(report.Rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(report.Rows), report.Rows)
	}
	// The picker first: it is the key a user checks first.
	if report.Rows[0].Target != core.PickerTarget {
		t.Errorf("first row is %q, want the picker", report.Rows[0].Target)
	}
	for _, r := range report.Rows {
		if r.Status != core.KeyTaken {
			t.Errorf("%s: status = %q, want taken", r.Chord, r.Status)
		}
	}
	if held := row(t, report, "ctrl+shift+o").HeldBy; held != "to-editor-window" {
		t.Errorf("ctrl+shift+o held by %q, want the shortcut's own name", held)
	}
	if binder.ListCalls != 1 {
		t.Errorf("List called %d times, want 1", binder.ListCalls)
	}
}

// The chord a project declares carries the command revier would install, so
// what `keys install` will write is visible before it writes it.
func TestActiveAndStaleAreTheSameOwnerDifferentCommand(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker"),
		hosttest.Custom("<Shift><Control>o", `sh -lc "revier go edit --picker"`, "revier: editor"),
	), keyProject(t, "revier"))

	if got := row(t, report, "alt+space").Status; got != core.KeyActive {
		t.Errorf("alt+space = %q, want active", got)
	}
	// An older install bound the target under a name it no longer has.
	// The chord is revier's, and it runs the wrong thing.
	stale := row(t, report, "ctrl+shift+o")
	if stale.Status != core.KeyStale {
		t.Errorf("ctrl+shift+o = %q, want stale", stale.Status)
	}
	if stale.Command != `sh -lc "revier go editor --picker"` {
		t.Errorf("command = %q, want what revier would install", stale.Command)
	}
}

// An entry in the desktop's store but switched off fires on no press and
// appears nowhere in the settings UI. It is reported for exactly that reason.
func TestSwitchedOffEntryIsInertAndNotFree(t *testing.T) {
	off := hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker")
	off.Enabled = false
	report := keysOf(t, hosttest.NewKeys("gnome", off), keyProject(t, "revier"))

	if got := row(t, report, "alt+space").Status; got != core.KeyInert {
		t.Errorf("alt+space = %q, want inert", got)
	}
}

// A shortcut that fires beats one that does not, whoever owns it: what fires
// is what the user experiences on a press.
func TestASwitchedOnShortcutWinsOverASwitchedOffOne(t *testing.T) {
	off := hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker")
	off.Enabled = false
	report := keysOf(t, hosttest.NewKeys("gnome",
		off,
		hosttest.Custom("<Alt>space", `sh -lc "something-else"`, "somebody-else"),
	), keyProject(t, "revier"))

	got := row(t, report, "alt+space")
	if got.Status != core.KeyTaken || got.HeldBy != "somebody-else" {
		t.Errorf("alt+space = %q held by %q, want taken by somebody-else", got.Status, got.HeldBy)
	}
}

// A desktop default on the chord cannot be claimed without changing a GNOME
// setting, so it reads differently from a chord nobody holds.
func TestADesktopDefaultIsNeitherFreeNorTaken(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
	), keyProject(t, "revier"))

	got := row(t, report, "alt+space")
	if got.Status != core.KeyBuiltin {
		t.Errorf("alt+space = %q, want builtin", got.Status)
	}
	for _, want := range []string{"org.gnome.desktop.wm.keybindings", "activate-window-menu"} {
		if !strings.Contains(got.HeldBy, want) {
			t.Errorf("held by %q, want it to name %q", got.HeldBy, want)
		}
	}
}

// revier's shortcut is right, and a desktop default still fires on the same
// press. Install plans a clear that needs --force for it, so calling it active
// would say the key is done while the default still runs.
func TestADesktopDefaultBesideRevierOwnShortcutIsNotActive(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker"),
		hosttest.Builtin("<Alt>space", "org.gnome.desktop.wm.keybindings", "activate-window-menu"),
	), keyProject(t, "revier"))

	got := row(t, report, "alt+space")
	if got.Status != core.KeyBuiltin {
		t.Errorf("alt+space = %q, want builtin: the desktop default still fires", got.Status)
	}
	for _, want := range []string{"revier: picker", "activate-window-menu"} {
		if !strings.Contains(got.HeldBy, want) {
			t.Errorf("held by %q, want it to name %q", got.HeldBy, want)
		}
	}
}

func TestAChordNobodyHoldsIsFree(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome"), keyProject(t, "revier"))
	for _, r := range report.Rows {
		if r.Status != core.KeyFree || r.HeldBy != "" {
			t.Errorf("%s = %q held by %q, want free and nobody", r.Chord, r.Status, r.HeldBy)
		}
	}
}

// A target one project declares is still a target revier wants a key for.
// This is the `web` case on this machine: one project of eighty-nine has it.
func TestAChordOneProjectDeclaresIsStillWanted(t *testing.T) {
	only := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "home", Home: true, Key: "ctrl-shift-u",
				Runtime: &revier.Realization{Launch: []string{"kitty"}, Match: revier.Match{Title: "^session:setup$"}},
			},
			{
				Name: "web", Key: "ctrl-shift-i",
				Window: &revier.Realization{Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}},
			},
		},
	})
	report := keysOf(t, hosttest.NewKeys("gnome"), keyProject(t, "revier"), only)

	web := row(t, report, "ctrl+shift+i")
	if web.Target != "web" || web.Status != core.KeyFree {
		t.Fatalf("web row = %+v", web)
	}
	if len(web.Projects) != 1 || web.Projects[0] != "setup" {
		t.Errorf("web asked for by %v, want only setup", web.Projects)
	}
	// The chord two projects share names both, in configuration order.
	home := row(t, report, "ctrl+shift+u")
	if len(home.Projects) != 2 {
		t.Errorf("home asked for by %v, want both projects", home.Projects)
	}
}

// A target refused at load wants no chord: the key it carries may be the one
// it was refused for, held by another target, and asked for it would read as
// a conflict that stops every project's keys (decisions.md D85).
func TestARefusedTargetWantsNoChord(t *testing.T) {
	p := prepared(t, revier.Project{
		Name: "demo", Path: "/home/user/demo",
		Targets: []revier.Target{
			{
				Name: "home", Home: true, Key: "ctrl-shift-u",
				Runtime: &revier.Realization{Launch: []string{"kitty"}, Match: revier.Match{Title: "^session:demo$"}},
			},
			{
				Name: "pulls", Key: "ctrl-shift-u",
				Window: &revier.Realization{Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}},
			},
		},
	})
	p.Refuse(1, errors.New(`targets "home" and "pulls" share key "ctrl+shift+u"`))
	report := keysOf(t, hosttest.NewKeys("gnome"), keyProject(t, "revier"), p)

	home := row(t, report, "ctrl+shift+u")
	if home.Target != "home" || home.Conflict {
		t.Errorf("home row = %+v, want it unmarked: the refused target asks for nothing", home)
	}
	for _, r := range report.Rows {
		if r.Target == "pulls" {
			t.Errorf("row %+v, want no row for the refused target", r)
		}
	}
}

// Projects that disagree produce a row each, both marked. One row would hide
// the disagreement, and refusing would make a diagnostic command fail exactly
// when it is needed.
func TestProjectsThatDisagreeProduceTwoMarkedRows(t *testing.T) {
	other := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "editor", Key: "ctrl-shift-e",
				Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}},
			},
		},
	})
	report := keysOf(t, hosttest.NewKeys("gnome"), keyProject(t, "revier"), other)

	var editors []core.KeyRow
	for _, r := range report.Rows {
		if r.Target == "editor" {
			editors = append(editors, r)
		}
	}
	if len(editors) != 2 {
		t.Fatalf("got %d editor rows, want 2: %+v", len(editors), report.Rows)
	}
	for _, r := range editors {
		if !r.Conflict {
			t.Errorf("%s is not marked as a conflict", r.Chord)
		}
		if len(r.Projects) != 1 {
			t.Errorf("%s asked for by %v, want one project", r.Chord, r.Projects)
		}
	}
}

// The other direction of the same disagreement. Two targets on one chord
// print one row active and one stale, and a user repairing the stale one
// breaks the working one.
func TestTwoTargetsOnOneChordAreBothMarked(t *testing.T) {
	other := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "terminal", Key: "ctrl-shift-u",
				Runtime: &revier.Realization{Launch: []string{"kitty"}, Match: revier.Match{Title: "^t$"}},
			},
		},
	})
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Shift><Control>u", `sh -lc "revier go home --picker"`, "revier: workspace"),
	), keyProject(t, "revier"), other)

	var onTheChord []core.KeyRow
	for _, r := range report.Rows {
		if r.Chord == "ctrl+shift+u" {
			onTheChord = append(onTheChord, r)
		}
	}
	if len(onTheChord) != 2 {
		t.Fatalf("got %d rows on ctrl+shift+u, want 2: %+v", len(onTheChord), report.Rows)
	}
	for _, r := range onTheChord {
		if !r.Conflict {
			t.Errorf("target %q on ctrl+shift+u is not marked as a conflict", r.Target)
		}
	}
}

// A target that is renamed or deleted leaves revier's shortcut behind, still
// firing, on a chord nothing asks for any more.
func TestAChordRevierHoldsAndNoLongerWantsIsAnOrphan(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Shift><Control>y", `sh -lc "revier go diff --picker"`, "revier: diff"),
		hosttest.Custom("<Shift><Control>u", `sh -lc "revier go home --picker"`, "revier: workspace"),
		hosttest.Custom("<Super>k", `sh -lc "somebody-else"`, "not-revier"),
	), keyProject(t, "revier"))

	if len(report.Orphaned) != 1 {
		t.Fatalf("got %d orphans, want 1: %+v", len(report.Orphaned), report.Orphaned)
	}
	orphan := report.Orphaned[0]
	if orphan.Chord != "ctrl+shift+y" {
		t.Errorf("orphan = %q, want ctrl+shift+y", orphan.Chord)
	}
	if orphan.Command != `sh -lc "revier go diff --picker"` {
		t.Errorf("orphan command = %q, want what is installed there now", orphan.Command)
	}
	if orphan.Status != core.KeyActive {
		t.Errorf("orphan status = %q, want active: it still fires", orphan.Status)
	}
	// The wanted chord is not an orphan, and somebody else's shortcut is
	// never revier's business.
	if got := row(t, report, "ctrl+shift+u").Status; got != core.KeyActive {
		t.Errorf("ctrl+shift+u = %q, want active", got)
	}
}

// A machine with no desktop is a normal outcome, not a failure.
func TestNoKeyBinderIsASentinel(t *testing.T) {
	c := &core.Core{}
	_, err := c.Keys(context.Background(), nil, "alt+space")
	if !errors.Is(err, core.ErrNoKeyBinder) {
		t.Fatalf("err = %v, want ErrNoKeyBinder", err)
	}
}

func TestAFailedReadIsReported(t *testing.T) {
	binder := hosttest.NewKeys("gnome")
	binder.ListErr = errors.New("gsettings is not answering")
	c := &core.Core{KeyBinder: binder}
	if _, err := c.Keys(context.Background(), nil, "alt+space"); err == nil {
		t.Fatal("want the read failure to reach the caller")
	}
}

// GNOME fires every switched-on shortcut on a chord, which is why README's
// step 3 asks a user to switch the old ones off. Reporting the first holder
// and dropping the rest calls the chord active while somebody else's command
// runs on the same press - the one state this command exists to catch.
func TestASecondSwitchedOnHolderIsNotHidden(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Shift><Control>u", `sh -lc "revier go home --picker"`, "revier: workspace"),
		hosttest.Custom("<Shift><Control>u", `sh -lc "$HOME/setup/scripts/sessions/os-to-session.sh"`, "to-session-terminal"),
	), keyProject(t, "revier"))

	got := row(t, report, "ctrl+shift+u")
	if got.Status != core.KeyTaken {
		t.Errorf("status = %q, want taken: somebody else's shortcut fires too", got.Status)
	}
	for _, want := range []string{"revier: workspace", "to-session-terminal"} {
		if !strings.Contains(got.HeldBy, want) {
			t.Errorf("held by %q, want it to name %q", got.HeldBy, want)
		}
	}
}

// The picker holds a chord like a target does. A target that declares the
// trigger key puts two rows on one chord - the picker active, the target
// stale - and repairing the stale one breaks the key that opens revier.
func TestATargetOnTheTriggerChordIsMarked(t *testing.T) {
	clash := prepared(t, revier.Project{
		Name: "setup", Path: "/home/user/setup",
		Targets: []revier.Target{
			{
				Name: "menu", Key: "alt-space",
				Window: &revier.Realization{Launch: []string{"rofi"}, Match: revier.Match{Class: "^rofi$"}},
			},
		},
	})
	report := keysOf(t, hosttest.NewKeys("gnome",
		hosttest.Custom("<Alt>space", `sh -lc "revier popup"`, "revier: picker"),
	), keyProject(t, "revier"), clash)

	var onTheChord []core.KeyRow
	for _, r := range report.Rows {
		if r.Chord == "alt+space" {
			onTheChord = append(onTheChord, r)
		}
	}
	if len(onTheChord) != 2 {
		t.Fatalf("got %d rows on alt+space, want 2: %+v", len(onTheChord), report.Rows)
	}
	for _, r := range onTheChord {
		if !r.Conflict {
			t.Errorf("target %q on alt+space is not marked as a conflict", r.Target)
		}
	}
}
