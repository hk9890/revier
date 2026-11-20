package tui_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// A project whose only live instance is a window attached by hand is open:
// its row carries the running mark, its pane says running, and it sorts with
// the projects that are up. The surface surveys with no attachments - it reads
// those from state, so a claim shows at once - so the view alone does not know
// about them and the surface adds them.
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

// A filter that matches no project leaves no row under the cursor, and the
// footer offers no key that acts on one: each would answer nothing. A filter
// that matches the project offers them again.
func TestTheFooterOffersTheRowKeysOnlyWhereThereIsARow(t *testing.T) {
	_, _, c, projects := world(t, 1)
	actions := []config.Action{{Key: "ctrl-y", Name: "sync", Run: []string{"true"}}}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), actions), 400, 24)

	none := typeInto(m, "zzz")
	got := footer(none)
	if !strings.Contains(got, "esc") {
		t.Errorf("footer = %q with no project selected, want esc", got)
	}
	for _, k := range []string{"open", "close", "alt+e", "alt+del", "ctrl+y"} {
		if strings.Contains(got, k) {
			t.Errorf("footer = %q with no project selected, want no %q", got, k)
		}
	}

	got = footer(typeInto(m, "00"))
	for _, k := range []string{"enter open", "del close", "alt+e edit", "alt+del delete"} {
		if !strings.Contains(got, k) {
			t.Errorf("footer = %q on a project, want %q", got, k)
		}
	}
}

// Tab in the Targets section goes back to the projects, and the footer says
// so: the targets are not a stop on the walk Tab makes.
func TestTheTargetsFooterSaysTabGoesBackToTheProjects(t *testing.T) {
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 24)
	m, _ = press(m, "alt+t")
	if got := footer(m); !strings.Contains(got, "back to projects") {
		t.Errorf("footer = %q in the targets, want tab said to go back to the projects", got)
	}
}
