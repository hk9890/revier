package main

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func report() core.KeyReport {
	return core.KeyReport{
		Desktop: "gnome",
		Rows: []core.KeyRow{
			{Chord: "alt+space", Target: core.PickerTarget, Status: core.KeyActive, HeldBy: "revier: picker", Command: `sh -lc "revier-popup"`},
			{Chord: "ctrl+shift+o", Target: "editor", Status: core.KeyTaken, HeldBy: "to-editor-window"},
			{Chord: "ctrl+shift+u", Target: "home", Status: core.KeyFree},
		},
	}
}

func printed(r core.KeyReport) string {
	var b strings.Builder
	printKeys(&b, r)
	return b.String()
}

func TestEveryRowReachesTheTable(t *testing.T) {
	out := printed(report())
	for _, want := range []string{
		"KEY", "TARGET", "STATUS", "HELD BY",
		"alt+space", "picker", "active", "revier: picker",
		"ctrl+shift+o", "editor", "taken", "to-editor-window",
		"ctrl+shift+u", "home", "free",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not carry %q:\n%s", want, out)
		}
	}
}

// A column left empty would read as a value that is missing rather than one
// that is not there.
func TestNobodyHoldingAKeyPrintsADash(t *testing.T) {
	for _, line := range strings.Split(printed(report()), "\n") {
		if strings.HasPrefix(line, "ctrl+shift+u") && !strings.HasSuffix(strings.TrimRight(line, " "), "-") {
			t.Errorf("free row = %q, want a dash for nobody", line)
		}
	}
}

// Two rows with the same target look like two targets unless the mark and the
// explanation are both there.
func TestAConflictIsMarkedAndExplained(t *testing.T) {
	r := report()
	r.Rows = append(r.Rows,
		core.KeyRow{Chord: "ctrl+shift+e", Target: "editor", Status: core.KeyFree, Conflict: true, Projects: []revier.ProjectName{"setup"}},
	)
	r.Rows[1].Conflict = true
	r.Rows[1].Projects = []revier.ProjectName{"revier", "beads"}

	out := printed(r)
	if !strings.Contains(out, "editor (conflict)") {
		t.Errorf("the row is not marked:\n%s", out)
	}
	for _, want := range []string{"revier, beads", "setup", "on more than one key"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not explain %q:\n%s", want, out)
		}
	}
}

// The other direction. The mark alone does not say which disagreement a row is
// in, so each is explained the way it runs.
func TestTwoTargetsOnOneKeyAreExplainedTheOtherWayRound(t *testing.T) {
	r := core.KeyReport{Rows: []core.KeyRow{
		{Chord: "ctrl+shift+u", Target: "home", Status: core.KeyActive, Conflict: true, Projects: []revier.ProjectName{"revier"}},
		{Chord: "ctrl+shift+u", Target: "terminal", Status: core.KeyStale, Conflict: true, Projects: []revier.ProjectName{"setup"}},
	}}
	out := printed(r)
	if !strings.Contains(out, "ctrl+shift+u is asked for by more than one target") {
		t.Errorf("the disagreement is not explained:\n%s", out)
	}
	if strings.Contains(out, "on more than one key") {
		t.Errorf("explained in the wrong direction:\n%s", out)
	}
	for _, want := range []string{"home", "terminal"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not name %q:\n%s", want, out)
		}
	}
}

// A shortcut on a chord nothing asks for any more still fires. It has its own
// block, or it is invisible.
func TestOrphansGetTheirOwnBlock(t *testing.T) {
	r := report()
	r.Orphaned = []core.KeyRow{
		{Chord: "ctrl+shift+y", Status: core.KeyActive, HeldBy: "revier: diff", Command: `sh -lc "revier-go diff"`},
	}
	out := printed(r)
	// The name is what finds the entry in the desktop's settings, and the
	// command is what says what it still does. Both are needed to remove it.
	for _, want := range []string{"ORPHANED", "ctrl+shift+y", "revier: diff", `sh -lc "revier-go diff"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not carry %q:\n%s", want, out)
		}
	}
}

func TestNoOrphansMeansNoBlock(t *testing.T) {
	if out := printed(report()); strings.Contains(out, "ORPHANED") {
		t.Errorf("an empty block was printed:\n%s", out)
	}
}
