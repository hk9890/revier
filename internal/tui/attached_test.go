package tui_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// A project whose only live instance is a window attached by hand is open:
// its row carries the running mark, its pane says running, and it sorts with
// the projects that are up (decisions.md D104). The surface surveys with no
// attachments - it reads those from state, so a claim shows at once - so the
// view alone does not know about them and the surface adds them.
func TestAProjectHeldOnlyByAnAttachedWindowReadsAsOpen(t *testing.T) {
	_, wm, c, projects := world(t, 3)
	ref := wm.Add("Pull requests", "chrome")
	root := stateWith(t, map[revier.ProjectName][]revier.TargetRef{"project-01": {ref}})
	m := resize(refreshed(t, c, projects, root, nil), 120, 24)

	held, closed := rowOf(t, m, "project-01"), rowOf(t, m, "project-00")
	if held > closed {
		t.Errorf("the attached project is row %d and the closed one row %d, want it above", held, closed)
	}
	th := theme.Default()
	if mark := rows(m)[held]; !strings.Contains(mark, th.Glyphs.Running) {
		t.Errorf("the row is %q, want the running mark", mark)
	}

	// The pane follows the cursor, so the facts are read on the row itself.
	m, _ = press(m, "down")
	if got := pane(m); !strings.Contains(got, "Status") || !strings.Contains(got, "running") {
		t.Errorf("the pane says:\n%s\nwant a running status", got)
	}
}

// rowOf is the index of the row naming the project.
func rowOf(t *testing.T, m tui.Model, name string) int {
	t.Helper()
	for i, r := range rows(m) {
		if strings.Contains(r, name+" ") {
			return i
		}
	}
	t.Fatalf("no row names %s:\n%s", name, strings.Join(rows(m), "\n"))
	return -1
}

// alt+t is offered where it does something. A project with no target row has
// no Targets section to reach, and a key in the footer that answers nothing
// is a key the user tries twice.
func TestTheFooterOffersTheTargetsKeyOnlyWhereThereAreTargets(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 24)
	if !strings.Contains(footer(m), "alt+t") {
		t.Errorf("footer = %q, want alt+t for a project with targets", footer(m))
	}

	for _, r := range "zzz" {
		m, _ = press(m, string(r))
	}
	if strings.Contains(footer(m), "alt+t") {
		t.Errorf("footer = %q with no project selected, want no alt+t", footer(m))
	}
}

// Tab in the Targets section goes back to the projects, and the footer says
// so: the targets are not a stop on the walk Tab makes (decisions.md D105).
func TestTheTargetsFooterSaysTabGoesBackToTheProjects(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 24)
	m, _ = press(m, "alt+t")
	if got := footer(m); !strings.Contains(got, "back to projects") {
		t.Errorf("footer = %q in the targets, want tab said to go back to the projects", got)
	}
}
