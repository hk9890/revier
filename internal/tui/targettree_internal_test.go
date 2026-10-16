package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// workspace is a home that opens with two tabs and the agent in focus, a tab
// inside it that it does not open with, and two windows, declared in an order
// that puts the tabs apart from their home.
func workspace() []revier.Target {
	return []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Tabs: []revier.TargetName{"tickets", "agent"}, Active: "agent"}},
		{Name: "logs", Runtime: &revier.Realization{Inside: "home"}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home"}},
		{Name: "editor", Window: &revier.Realization{}},
		{Name: "tickets", Runtime: &revier.Realization{Inside: "home"}},
		{Name: "web", Window: &revier.Realization{}},
	}
}

func views(declared []revier.Target) []revier.TargetView {
	out := make([]revier.TargetView, len(declared))
	for i, t := range declared {
		out[i] = revier.TargetView{Name: t.Name, Available: true}
	}
	return out
}

// A tab hangs under the target it is inside: first the tabs it opens with,
// in that order, then the rest in the order of the survey. The last ends the
// branch, and the one it opens in focus is marked.
func TestTabsHangUnderTheirContainer(t *testing.T) {
	rows := treeRows(views(workspace()), workspace())
	type shape struct {
		name   revier.TargetName
		kind   targetKind
		last   bool
		active bool
	}
	var got []shape
	for _, r := range rows {
		got = append(got, shape{r.target.Name, r.kind, r.last, r.active})
	}
	want := []shape{
		{"home", kindContainer, false, false},
		{"tickets", kindTab, false, false},
		{"agent", kindTab, false, true},
		{"logs", kindTab, true, false},
		{"editor", kindWindow, false, false},
		{"web", kindWindow, false, false},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

// With no tab listed, the container's first tab has the focus; a target the
// configuration does not declare is a window.
func TestTheFirstTabHasTheFocusWhenNoneIsNamed(t *testing.T) {
	declared := workspace()
	declared[0].Runtime.Active = ""
	rows := treeRows(append(views(declared), revier.TargetView{Name: "stray"}), declared)
	if !rows[1].active || rows[1].target.Name != "tickets" {
		t.Errorf("rows[1] = %+v, want tickets in focus", rows[1])
	}
	if last := rows[len(rows)-1]; last.target.Name != "stray" || last.kind != kindWindow {
		t.Errorf("last row = %+v, want stray as a window", last)
	}
}

// A tab whose container the survey does not report has no branch to hang
// from, and stays where it is.
func TestATabWithoutItsContainerStaysInPlace(t *testing.T) {
	declared := workspace()
	rows := treeRows(views(declared[1:]), declared)
	if rows[0].target.Name != "logs" || rows[0].kind != kindWindow {
		t.Errorf("rows[0] = %+v, want logs at the top", rows[0])
	}
}

// A tab its configuration refused - inside itself, inside a tab, or in a
// cycle - is still surveyed, and keeps a row of its own so its reason can be
// read and its entry dropped.
func TestARefusedTabKeepsItsRow(t *testing.T) {
	in := func(name, container revier.TargetName) revier.Target {
		return revier.Target{Name: name, Runtime: &revier.Realization{Inside: container}}
	}
	declared := []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{}},
		in("self", "self"),
		in("tab", "home"),
		in("nested", "tab"),
		in("a", "b"),
		in("b", "a"),
	}
	var got []revier.TargetName
	for _, r := range treeRows(views(declared), declared) {
		got = append(got, r.target.Name)
	}
	want := []revier.TargetName{"home", "tab", "self", "nested", "a", "b"}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// The row draws the kind in front of the name: the container's glyph, a
// branch for each tab and a corner for the last, a window's glyph. A set with
// no kind glyphs keeps the names in line with the tabs'.
func TestARowDrawsItsKind(t *testing.T) {
	for _, set := range []string{"unicode", "ascii"} {
		th, err := theme.Lookup(theme.DefaultTheme, set)
		if err != nil {
			t.Fatal(err)
		}
		m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, th, "")
		g := th.Glyphs
		blank := func(s string) string {
			if s == "" {
				return " "
			}
			return s
		}
		rows := treeRows(views(workspace()), workspace())
		want := []string{
			blank(g.Container) + " home",
			g.Branch + " tickets",
			g.Branch + " agent",
			g.LastBranch + " logs",
			blank(g.Window) + " editor",
			blank(g.Window) + " web",
		}
		for i, row := range rows {
			plain := ansi.Strip(m.detailRow(row, 60, false, false))
			if !strings.HasPrefix(plain, "  "+want[i]) {
				t.Errorf("%s: row %q, want it to start with %q", set, plain, "  "+want[i])
			}
		}
		if agent := ansi.Strip(m.detailRow(rows[2], 60, false, false)); !strings.Contains(agent, "opens first") {
			t.Errorf("%s: agent row %q, want it marked as the tab that opens first", set, agent)
		}
	}
}
