package tui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

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
// the walk. It opens what the session recorded.
func TestRestoreOnTheStartDialogClearsTheShutdownAndRestores(t *testing.T) {
	m, rt, root, s := shutDownWorld(t)
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
	if p := pane(m); !strings.Contains(p, "Restored") || !strings.Contains(p, s.ID) {
		t.Errorf("pane = %q, want what the restore of %s came to", p, s.ID)
	}
	if !opened(rt, "session:project-00") {
		t.Error("the restore did not open project-00's home")
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
