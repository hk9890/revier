package tui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// shutDownWorld is world with a full shutdown on record, naming a session of
// project-00's home and its agent, and the surface built over that state.
func shutDownWorld(t *testing.T) (m tui.Model, rt *hosttest.FakeRuntime, root string, s session.Session) {
	t.Helper()
	rt, _, c, projects := world(t, 2)
	root = t.TempDir()
	s = saved(t, root, "", 10, session.Project{Name: "project-00", Targets: []session.Target{
		{Name: "home", Agents: []session.Agent{{Harness: "claude"}}},
	}})
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), s.ID)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	return resize(refreshed(t, c, projects, root, nil), 150, 30), rt, root, s
}

// opened reports a realization of that name opened on the runtime.
func opened(rt *hosttest.FakeRuntime, name string) bool {
	return slices.ContainsFunc(rt.Opened, func(r revier.Realization) bool { return r.Name == name })
}

// After a full shutdown the first screen is the start dialog: when revier was
// shut down, what the session it saved holds, and the three ways on.
func TestTheStartDialogOffersTheRestoreAfterAFullShutdown(t *testing.T) {
	m, _, _, _ := shutDownWorld(t)

	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down at 2026-09-15 18:30.") {
		t.Errorf("subtitle = %q, want when revier was shut down", sub)
	}
	if p := pane(m); !strings.Contains(p, "1 project · 1 target · 1 agent") {
		t.Errorf("pane = %q, want what was saved", p)
	}
	body := strings.Join(rows(m), "\n")
	for _, want := range []string{"Restore this session", "Pick another session", "Start fresh"} {
		if !strings.Contains(body, want) {
			t.Errorf("rows lack %q:\n%s", want, body)
		}
	}
}

// The start dialog's line over the rule fits the list's column at 120
// columns, whole, and keeps a column of air before the pane's border.
func TestTheStartDialogTitleKeepsClearOfThePane(t *testing.T) {
	m, _, _, _ := shutDownWorld(t)
	m = resize(m, 120, 30)
	border := paneBorder(t, m)
	sub := []rune(lines(m)[2])
	if len(sub) <= border {
		t.Fatalf("subtitle line = %q, want the pane beside it", string(sub))
	}
	left := string(sub[:border])
	if !strings.Contains(left, "revier was shut down at 2026-09-15 18:30.") {
		t.Errorf("subtitle = %q, want when revier was shut down, whole, left of the pane", left)
	}
	if !strings.HasSuffix(left, " ") {
		t.Errorf("subtitle = %q, want a column of air before the pane's border", left)
	}
}

// With no full shutdown on record the surface opens on the projects.
func TestNoStartDialogWithoutAShutdown(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the surface", bar)
	}
}

// Restore takes the shutdown off the record as it starts, so a restore that
// never ends does not bring the dialog back, and the sessions screen shows
// the walk. It opens what the session recorded and ends on the project list.
func TestRestoreOnTheStartDialogClearsTheShutdownAndRestores(t *testing.T) {
	m, rt, root, _ := shutDownWorld(t)
	m, cmd := press(m, "enter")
	if sd := shutDownOnRecord(t, root); sd != nil {
		t.Errorf("shutdown on record = %+v once the restore started, want none", sd)
	}
	if bar := barLine(m); !strings.Contains(bar, "Sessions") {
		t.Errorf("bar = %q, want the sessions screen", bar)
	}
	if p := pane(m); !strings.Contains(p, "Restoring") {
		t.Errorf("pane = %q, want the walk shown", p)
	}
	m = run(m, cmd)
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the project list once the restore ended", bar)
	}
	if !opened(rt, "session:project-00") {
		t.Error("the restore did not open project-00's home")
	}
}

// A restore from the start dialog ends on the project list, with the cursor
// on the project the session was left on, and then raises the surface's own
// terminal over the windows it opened.
func TestARestoreFromTheStartDialogEndsOnTheListAndRaisesTheSurface(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	self := rt.Add("revier", "kitty", revier.Panel{ID: "9", Kind: revier.PanelShell})
	c.Self = func(p revier.Panel) bool { return p.ID == "9" }
	root := t.TempDir()
	s, _, err := session.Save(root, session.Session{
		Current: "project-01", At: time.Date(2026, 9, 15, 18, 10, 0, 0, time.Local),
		Projects: []session.Project{homeOf("project-00"), homeOf("project-01")},
	})
	if err != nil {
		t.Fatal(err)
	}
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), s.ID)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	m, cmd := press(m, "enter")
	m, raise := deliver(m, cmd)
	runAll(raise)

	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the project list once the restore ended", bar)
	}
	wantSelected(t, m, "project-01", "the project the session was left on")
	if len(rt.Focuses) == 0 || rt.Focuses[len(rt.Focuses)-1] != self {
		t.Errorf("Focuses = %v, want the surface's terminal %v raised last", rt.Focuses, self)
	}
}

// A restore whose targets do not open still ends on the project list, and
// the footer says how many did not open.
func TestARestoreWithTargetsThatFailEndsOnTheListAndSaysSo(t *testing.T) {
	m, rt, _, s := shutDownWorld(t)
	rt.OpenErr = errors.New("kitty refused")

	m, cmd := press(m, "enter")
	m = run(m, cmd)

	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the project list once the restore ended", bar)
	}
	if foot := footer(m); !strings.Contains(foot, "the restore of "+s.ID+": 1 target did not open") {
		t.Errorf("footer = %q, want the target that did not open said", foot)
	}
}

// Start fresh, and Esc as well, take the shutdown off the record and open
// nothing; the saved session stays.
func TestStartFreshClearsTheShutdownOnly(t *testing.T) {
	for _, keys := range [][]string{{"esc"}, {"down", "down", "enter"}} {
		m, rt, root, s := shutDownWorld(t)
		for _, k := range keys {
			m, _ = press(m, k)
		}
		if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
			t.Errorf("%v: bar = %q, want the surface", keys, bar)
		}
		if sd := shutDownOnRecord(t, root); sd != nil {
			t.Errorf("%v: shutdown on record = %+v, want none", keys, sd)
		}
		if all, _ := session.List(root); len(all) != 1 || all[0].ID != s.ID {
			t.Errorf("%v: sessions = %+v, want the saved one kept", keys, all)
		}
		if opened(rt, "session:project-00") {
			t.Errorf("%v: something was opened, want nothing", keys)
		}
	}
}

// Pick another session is the sessions screen; the shutdown stays on record
// until something opens.
func TestPickAnotherSessionOpensTheSessionsScreen(t *testing.T) {
	m, _, root, _ := shutDownWorld(t)
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	if bar := barLine(m); !strings.Contains(bar, "Sessions") || !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the sessions screen", bar)
	}
	if shutDownOnRecord(t, root) == nil {
		t.Error("no shutdown on record, want it kept until something opens")
	}
}

// A shutdown that saved no session offers none, though an older one is saved:
// that one is not what the shutdown closed.
func TestAShutdownThatSavedNoSessionOffersNone(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := t.TempDir()
	saved(t, root, "", 10, session.Project{Name: "project-00", Targets: []session.Target{{Name: "home"}}})
	st := &state.State{}
	st.MarkShutDown(time.Now(), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	if p := pane(m); !strings.Contains(p, "No session was saved.") {
		t.Errorf("pane = %q, want no session named", p)
	}
	if body := strings.Join(rows(m), "\n"); strings.Contains(body, "Restore this session") {
		t.Errorf("rows = %q, want no restore offered", body)
	}
}

// An open elsewhere while the dialog is up ends the shutdown, and the next
// read of the state takes the dialog down: it never offers a restore over
// what was opened since.
func TestAnOpenElsewhereTakesTheStartDialogDown(t *testing.T) {
	m, _, root, _ := shutDownWorld(t)
	if _, err := state.Update(root, func(st *state.State) bool {
		st.Launched("project-00", "home", time.Now())
		return true
	}); err != nil {
		t.Fatal(err)
	}
	m = survey(m)
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the surface once something opened", bar)
	}
}

// A surface that stays on the screen through a `revier shutdown` run
// elsewhere offers the restore at the next state it reads, but only over the
// plain surface: the screen the user is on stays, and the dialog comes once
// they are back.
func TestAVisibleSurfaceOffersTheRestoreOfAShutdownRunElsewhere(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+h")
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	offered := func(m tui.Model) bool {
		return strings.Contains(lines(m)[2], "revier was shut down at 2026-09-15 18:30.")
	}

	if m = survey(m); offered(m) {
		t.Error("the start dialog took the help screen's place, want the screen the user is on kept")
	}
	m, _ = press(m, "esc")
	if m = survey(m); !offered(m) {
		t.Errorf("subtitle = %q on the surface after the shutdown, want the start dialog", lines(m)[2])
	}
}

// A query on the agent list is an errand the user is in, as one on the
// projects is: the start dialog waits for it, so the Enter that picks an
// agent does not restore a session.
func TestAnAgentQueryHoldsTheStartDialogBack(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := switched(resize(refreshed(t, c, projects, root, nil), 150, 30))
	m, _ = press(m, "n")
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	if m = survey(m); strings.Contains(lines(m)[2], "revier was shut down") {
		t.Errorf("subtitle = %q over the agent query, want the query kept", lines(m)[2])
	}
}

// A start dialog up for one shutdown is put up again for a later one, so it
// offers the session the shutdown on record saved.
func TestTheStartDialogFollowsALaterShutdown(t *testing.T) {
	m, _, root, _ := shutDownWorld(t)
	later := saved(t, root, "", 20, session.Project{Name: "project-01", Targets: []session.Target{{Name: "home"}}})
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 16, 9, 15, 0, 0, time.Local), later.ID)
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	if m = survey(m); !strings.Contains(lines(m)[2], "revier was shut down at 2026-09-16 09:15.") {
		t.Errorf("subtitle = %q, want the later shutdown", lines(m)[2])
	}
	if p := pane(m); !strings.Contains(p, "session "+later.ID) {
		t.Errorf("pane = %q, want the session the later shutdown saved", p)
	}
}

// A shutdown offered once is not offered again by the next state read: the
// user who went on to the sessions screen and came back is not asked twice.
func TestAShutdownOfferedOnceIsNotOfferedAgainOnARefresh(t *testing.T) {
	m, _, _, _ := shutDownWorld(t)
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m, _ = press(m, "esc")

	if m = survey(m); strings.Contains(lines(m)[2], "revier was shut down") {
		t.Error("the start dialog came back on a refresh after the user left it")
	}
}

// A popup that a full shutdown left up, as `revier shutdown` leaves it while
// it is hidden, offers the restore on its next raise.
func TestARaisedPopupOffersTheRestoreOfAShutdownRecordedWhileHidden(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	m = m.Raised()
	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down at 2026-09-15 18:30.") {
		t.Errorf("subtitle = %q after the raise, want the start dialog", sub)
	}
}

// A delete still asked when the popup hid is not asked on the raise that
// offers the restore: the first key goes to the start dialog.
func TestARaiseDropsADeleteAskedBeforeTheHide(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	projects, _ := onDisk(t, []string{"alpha"}, nil, nil)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+d")
	if f := footer(m); !strings.Contains(f, "y: delete") {
		t.Fatalf("footer = %q, want the delete question", f)
	}
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}

	m = m.Raised()
	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down") {
		t.Fatalf("subtitle = %q after the raise, want the start dialog", sub)
	}
	if f := footer(m); strings.Contains(f, "y: delete") {
		t.Errorf("footer = %q after the raise, want no delete question", f)
	}
	m, _ = press(m, "esc")
	if sub := lines(m)[2]; strings.Contains(sub, "revier was shut down") {
		t.Errorf("subtitle = %q, want the start dialog closed by the first key", sub)
	}
}

// uncleanEnd writes what a surface last saw before an end with no shutdown:
// project-00's home, which is not open now.
func uncleanEnd(t *testing.T, root string) {
	t.Helper()
	if err := session.SaveSeen(root, session.Session{At: time.Date(2026, 9, 15, 18, 20, 0, 0, time.Local), Projects: []session.Project{
		{Name: "project-00", Targets: []session.Target{{Name: "home"}}},
	}}); err != nil {
		t.Fatal(err)
	}
}

// After an end with no shutdown that lost something, the first survey opens
// the start dialog on what was last seen, saved as a session. No record keeps
// it up, so the next refresh leaves it, and Restore opens what it holds.
func TestTheStartDialogOffersWhatWasLastSeenAfterAnUncleanEnd(t *testing.T) {
	root := t.TempDir()
	uncleanEnd(t, root)
	m, rt := seenWorld(t, root)
	m = seenSurvey(seenSurvey(m))

	if sub := lines(m)[2]; !strings.Contains(sub, "revier ended without a shutdown; last changed 2026-09-15 18:20.") {
		t.Fatalf("subtitle = %q, want the dialog after an end with no shutdown", sub)
	}
	all, err := session.List(root)
	if err != nil || len(all) != 1 || all[0].Reason != session.ReasonLastSeen {
		t.Fatalf("sessions = %+v, %v, want what was last seen saved", all, err)
	}
	if p := pane(m); !strings.Contains(p, all[0].ID) {
		t.Errorf("pane = %q, want the session %s offered", p, all[0].ID)
	}

	m, cmd := press(m, "enter")
	run(m, cmd)
	if !opened(rt, "session:project-00") {
		t.Error("the restore did not open project-00's home")
	}
}

// After a full shutdown its own dialog comes up as before, and what was last
// seen is not saved as a session.
func TestAFullShutdownStillOpensItsOwnDialog(t *testing.T) {
	root := t.TempDir()
	uncleanEnd(t, root)
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m, _ := seenWorld(t, root)
	m = seenSurvey(m)

	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down at 2026-09-15 18:30.") {
		t.Errorf("subtitle = %q, want the dialog of the full shutdown", sub)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want no last-seen save", all)
	}

	// While the shutdown is on record, what the surface sees is not written:
	// it would be left behind as what an end with no shutdown lost.
	forget(t, root)
	seenSurvey(seenSurvey(m))
	if s, ok := seenOnDisk(t, root); ok {
		t.Errorf("seen = %+v while a full shutdown is on record, want nothing written", s)
	}
}

// A query typed before the check answers holds the dialog after an end with
// no shutdown back, not away: it comes up at the first survey after the
// query is cleared.
func TestTheDialogAfterAnUncleanEndWaitsForThePlainSurface(t *testing.T) {
	root := t.TempDir()
	uncleanEnd(t, root)
	m, _ := seenWorld(t, root)
	m, _ = press(m, "x")
	m = seenSurvey(seenSurvey(m))
	if sub := lines(m)[2]; strings.Contains(sub, "revier ended without a shutdown") {
		t.Fatalf("subtitle = %q, want the dialog held back while a query is up", sub)
	}

	m, _ = press(m, "backspace")
	m = seenSurvey(m)
	if sub := lines(m)[2]; !strings.Contains(sub, "revier ended without a shutdown; last changed 2026-09-15 18:20.") {
		t.Errorf("subtitle = %q, want the dialog once the surface is plain", sub)
	}
}

// A shutdown recorded while the dialog after an end with no shutdown is held
// back takes its place: once the shutdown's own dialog is answered, the older
// end is not offered after it.
func TestAShutdownRecordedWhileTheUncleanDialogWaitsTakesItsPlace(t *testing.T) {
	root := t.TempDir()
	uncleanEnd(t, root)
	m, _ := seenWorld(t, root)
	m, _ = press(m, "x")
	m = seenSurvey(seenSurvey(m))

	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	m, _ = press(m, "backspace")
	m = seenSurvey(m)
	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down at 2026-09-15 18:30.") {
		t.Fatalf("subtitle = %q, want the dialog of the newer shutdown", sub)
	}

	m, cmd := press(m, "esc")
	if cmd != nil {
		m = run(m, cmd)
	}
	m = seenSurvey(seenSurvey(m))
	if sub := lines(m)[2]; strings.Contains(sub, "revier ended without a shutdown") {
		t.Errorf("subtitle = %q, want no offer of the end before the recorded shutdown", sub)
	}
}

// A restore made by hand while the dialog after an end with no shutdown is
// held back takes its place: the user has handled the sessions, so the end
// is not offered once the restore ends on the plain surface.
func TestARestoreMadeWhileTheUncleanDialogWaitsTakesItsPlace(t *testing.T) {
	root := t.TempDir()
	uncleanEnd(t, root)
	saved(t, root, "work", 10, homeOf("project-01"))
	m, _ := seenWorld(t, root)
	m, _ = press(m, "alt+s")
	m = seenSurvey(seenSurvey(m))
	if sub := lines(m)[2]; strings.Contains(sub, "revier ended without a shutdown") {
		t.Fatalf("subtitle = %q, want the dialog held back while the sessions screen is up", sub)
	}

	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Fatalf("bar = %q, want the project list once the restore ended", bar)
	}
	m = seenSurvey(seenSurvey(m))
	if sub := lines(m)[2]; strings.Contains(sub, "revier ended without a shutdown") {
		t.Errorf("subtitle = %q, want no offer of the end after a restore by hand", sub)
	}
}
