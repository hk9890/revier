package tui_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// The cursor follows its project while the project is open, and keeps its
// place once the project closes (decisions.md D109).

// openWorld is world with the named projects' workspaces running too, and the
// surface over it. The rows are the last project, which needs the user, then
// the open ones, then the stopped ones, each group in file order.
func openWorld(t *testing.T, n int, open ...string) (*hosttest.FakeRuntime, tui.Model, map[string]revier.TargetRef) {
	t.Helper()
	rt, _, c, projects := world(t, n)
	refs := map[string]revier.TargetRef{}
	for _, name := range open {
		refs[name] = rt.Add("session:"+name, "kitty")
	}
	return rt, resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30), refs
}

func wantSelected(t *testing.T, m tui.Model, name, why string) {
	t.Helper()
	if row := selectedRow(t, m); !strings.Contains(row, name) {
		t.Errorf("selected %q, want %s: %s", row, name, why)
	}
}

// A project that closes goes down among the stopped rows, and the cursor
// stays where it was: on the row that moved up into its place.
func TestAClosedProjectLeavesTheCursorOnTheRowThatTookItsPlace(t *testing.T) {
	rt, m, refs := openWorld(t, 4, "project-00", "project-01")

	m, _ = press(m, "down")
	wantSelected(t, m, "project-00", "the first open row under the one that needs the user")

	rt.Remove(refs["project-00"])
	m = survey(m)
	wantSelected(t, m, "project-01", "the row that took project-00's place")
}

// The last open project has no open row under it, so the row that takes its
// place is a stopped one.
func TestTheLastOpenProjectClosedLeavesTheCursorInItsPlace(t *testing.T) {
	rt, m, refs := openWorld(t, 4, "project-01")

	m, _ = press(m, "down")
	wantSelected(t, m, "project-01", "the open row under the one that needs the user")

	rt.Remove(refs["project-01"])
	m = survey(m)
	wantSelected(t, m, "project-00", "the first stopped row, now in project-01's place")
}

// A project that stays open keeps the cursor when its row moves: one that no
// longer needs the user goes down among the open ones, and the cursor with it.
func TestTheCursorFollowsAnOpenProjectThatNoLongerNeedsTheUser(t *testing.T) {
	rt, m, _ := openWorld(t, 4, "project-00")
	wantSelected(t, m, "project-03", "the row that needs the user")

	rt.Retitle("1", "zsh")
	m = survey(m)
	if got := rows(m); len(got) < 2 || !strings.Contains(got[1], "project-03") {
		t.Fatalf("rows = %q, want project-03 under project-00 once its agent is gone", got)
	}
	wantSelected(t, m, "project-03", "it is still open")
}

// A stopped project was never closed under the cursor, so the cursor follows
// it when a project opening above moves its row.
func TestTheCursorFollowsAStoppedProjectWhenRowsMove(t *testing.T) {
	rt, m, _ := openWorld(t, 5, "project-00")

	for range 3 {
		m, _ = press(m, "down")
	}
	wantSelected(t, m, "project-02", "the second stopped row")

	rt.Add("session:project-03", "kitty")
	m = survey(m)
	wantSelected(t, m, "project-02", "it did not close, its row moved down")
}

// The pane's cursor was on a row of the project that closed, and the pane now
// shows another project: the cursor is back in the list, on no pane row.
func TestAClosedProjectTakesThePaneCursorBackToTheList(t *testing.T) {
	rt, m, refs := openWorld(t, 4, "project-00", "project-01")

	m, _ = press(m, "down")
	m, _ = press(m, "alt+t")
	if paneCursor(m) == "" {
		t.Fatalf("alt+t put the cursor on no target:\n%s", m.View())
	}

	rt.Remove(refs["project-00"])
	m = survey(m)
	wantSelected(t, m, "project-01", "the row that took project-00's place")
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q, want none: the project it was in closed", row)
	}
}

// A full shutdown closes every project, so the cursor has no place to keep:
// it goes to the first row.
func TestAFullShutdownPutsTheCursorOnTheFirstRow(t *testing.T) {
	_, m, _ := openWorld(t, 4, "project-00", "project-01")

	m, _ = press(m, "down")
	m, _ = press(m, "down")
	wantSelected(t, m, "project-01", "the second open row")

	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	m, cmd = press(m, "enter")
	m = run(m, cmd)
	m, _ = press(m, "esc")
	m = survey(m)
	wantSelected(t, m, "project-00", "the first row once everything closed")
}
