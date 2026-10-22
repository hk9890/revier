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

	if sub := lines(m)[2]; !strings.Contains(sub, "revier was shut down at 2026-09-15 18:30. 1 project, 1 agent saved.") {
		t.Errorf("subtitle = %q, want when and what was saved", sub)
	}
	body := strings.Join(rows(m), "\n")
	for _, want := range []string{"Restore this session", "Pick another session", "Start fresh"} {
		if !strings.Contains(body, want) {
			t.Errorf("rows lack %q:\n%s", want, body)
		}
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
