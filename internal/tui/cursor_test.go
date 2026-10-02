package tui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
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
	// A row is two lines, and the pane beside the rows names project-03 too:
	// the second row's name is the third line, left of the pane.
	second := ""
	if got := rows(m); len(got) > 2 {
		second, _, _ = strings.Cut(got[2], "│")
	}
	if !strings.Contains(second, "project-03") {
		t.Fatalf("second row = %q, want project-03 under project-00 once its agent is gone", second)
	}
	wantSelected(t, m, "project-03", "it is still open")
}

// A host that could not list has not said its projects closed (decisions.md
// D89): the cursor stays on its project, in the pane too, and is still on it
// when the host answers again.
func TestAHostThatCannotListDoesNotTakeTheCursorOffItsProject(t *testing.T) {
	rt, m, _ := openWorld(t, 4, "project-00", "project-01")

	m, _ = press(m, "down")
	m, _ = press(m, "down")
	wantSelected(t, m, "project-01", "the second open row")
	inPane, _ := press(m, "alt+t")

	rt.SetInstancesErr(errors.New("socket timed out"))
	m = survey(m)
	wantSelected(t, m, "project-01", "its host has not said it closed")
	if paneCursor(survey(inPane)) == "" {
		t.Errorf("the pane cursor went back to the list, want it kept on a target of project-01")
	}

	rt.SetInstancesErr(nil)
	m = survey(m)
	wantSelected(t, m, "project-01", "it is open, as it was")
}

// A row no survey has answered for drew nothing as open: an attachment in
// state whose window is gone makes the first survey no close, and the cursor
// stays on the project the user moved it to.
func TestTheFirstSurveyDroppingAStaleAttachmentIsNoClose(t *testing.T) {
	_, wm, c, projects := world(t, 4)
	ref := wm.Add("Pull requests", "chrome")
	wm.Remove(ref)
	root := stateWith(t, map[revier.ProjectName][]revier.TargetRef{"project-01": {ref}})
	m := resize(tui.New(c, projects, root, &config.Config{}, time.Second, theme.Default(), ""), 150, 30)

	m, _ = press(m, "down")
	wantSelected(t, m, "project-01", "the second row in file order")

	m = survey(m)
	wantSelected(t, m, "project-01", "the user moved the cursor to it before the survey")
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
