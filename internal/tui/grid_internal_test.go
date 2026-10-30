package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// column is the cell a word starts at in a rendered row.
func column(t *testing.T, row, word string) int {
	t.Helper()
	plain := ansi.Strip(row)
	i := strings.Index(plain, word)
	if i < 0 {
		t.Fatalf("row %q has no %q", plain, word)
	}
	return lipgloss.Width(plain[:i])
}

// The pane's targets and attached windows share one grid: the state glyph
// starts in the same cell in both.
func TestThePaneLinesUpTargetsAndAttachedWindows(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	g := m.theme.Glyphs
	const w = 60
	target := m.detailRow(targetRow{target: revier.TargetView{
		Name: "home", Key: "ctrl+shift+u", Available: true, Ref: revier.TargetRef{Host: "rt", ID: "1"},
	}}, w, false, false)
	attached := m.detailRow(targetRow{attached: revier.TargetRef{Host: "wm", ID: "2", Title: "Pull requests"}}, w, false, false)
	if a, b := column(t, target, g.Running+" running"), column(t, attached, g.Running+" running"); a != b {
		t.Errorf("target state at %d, attached state at %d", a, b)
	}
}

// secondLine is the second line of an agent's row of project p, w cells wide,
// as plain text.
func secondLine(a revier.AgentView, p revier.ProjectName, w int) string {
	row := agentLines(theme.Default(), newAgentItem(a, revier.Project{Name: p}, revier.AgentDetail{}, time.Now()), nil, w, false, false, false, false)
	_, second, _ := strings.Cut(ansi.Strip(row), "\n")
	return second
}

// A second line short of room cuts the project and the worktree, and keeps
// the name whole.
func TestANarrowAgentRowKeepsTheName(t *testing.T) {
	a := revier.AgentView{State: revier.AgentState{
		Harness: "claude", Name: "revier-c0", Dir: "/p/revier/.claude/worktrees/agent-key-starts"}}
	width := func(left string) int {
		return 2 + listedStateWidth + lipgloss.Width(left) + gridGap + lipgloss.Width("revier-c0 ")
	}
	for _, c := range []struct {
		width int
		want  string
	}{
		{80, "revier · agent-key-starts"},
		{width("revier · agent-k…"), "revier · agent-k…"},
	} {
		row := strings.TrimSpace(secondLine(a, "revier", c.width))
		if !strings.HasPrefix(row, c.want+" ") || !strings.HasSuffix(row, " revier-c0") {
			t.Errorf("w=%d: second line = %q, want %q and the whole name", c.width, row, c.want)
		}
	}
}

// No row is wider than the pane, however long its title, so a click and the
// hover land on the row drawn under the pointer.
func TestAPaneRowFitsThePane(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	title := strings.Repeat("a very long window title ", 4)
	long := newAgentItem(revier.AgentView{Target: "a-long-target-name", Panel: "1234", State: revier.AgentState{
		Harness: "cursor-agent-with-a-long-name", Name: "a-long-session-name", Activity: title, Dir: "/p/" + title,
	}}, revier.Project{Name: "a-long-project-name"}, revier.AgentDetail{}, time.Now())
	for _, w := range []int{20, 42, 60} {
		rows := []string{
			m.detailRow(targetRow{attached: revier.TargetRef{Host: "wm", ID: "2", Title: title}}, w, false, false),
			m.detailRow(targetRow{target: revier.TargetView{Name: "a-long-target-name", Key: "ctrl+shift+alt+u"}}, w, false, false),
			detailAgent(m.spun(), long.agent, w),
			agentLines(m.spun(), long, nil, w, true, false, true, false),
			agentLines(m.spun(), long, nil, w, true, false, true, true),
		}
		for _, row := range rows {
			for _, line := range strings.Split(row, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("w=%d: line %q is %d cells", w, ansi.Strip(line), got)
				}
			}
		}
	}
}

// The letters a query matched are lit where they are, after a character
// wider than a byte too: an activity line carries glyphs and dashes.
func TestTheMatchedLettersAreLitAfterAWideCharacter(t *testing.T) {
	upper := lipgloss.NewStyle().Transform(strings.ToUpper)
	label := "✳ fix remote"
	// The label holds fix whole and fre apart: the positions of the first are
	// wholeMatch's, and those of the second the scorer's.
	for query, want := range map[string]string{"fix": "✳ FIX remote", "fre": "✳ Fix REmote"} {
		ranks := ranked(ungrouped)(query, []string{label})
		if len(ranks) != 1 {
			t.Fatalf("filter ranks for %s = %v, want one match", query, ranks)
		}
		if got := highlight(label, ranks[0].MatchedIndexes, lipgloss.NewStyle(), upper); got != want {
			t.Errorf("highlight = %q, want the letters of %s lit: %q", got, query, want)
		}
	}

	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	m.theme.Match = upper
	it := newAgentItem(revier.AgentView{State: revier.AgentState{Harness: "claude", Activity: "— wide dash, then words"}}, revier.Project{Name: "alpha"}, revier.AgentDetail{}, time.Now())
	matches := ranked(ungrouped)("then", []string{it.FilterValue()})[0].MatchedIndexes
	if got := ansi.Strip(agentLines(m.spun(), it, matches, 60, false, false, false, true)); !strings.Contains(got, "THEN words") {
		t.Errorf("agent row = %q, want the letters of then lit", got)
	}
}

// A query that matches the second line lights its letters there: the
// worktree, and the name, in the list and in the pane, which leaves the
// project out.
func TestTheMatchedLettersAreLitOnTheSecondLine(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	m.theme.Match = lipgloss.NewStyle().Transform(strings.ToUpper)
	it := newAgentItem(revier.AgentView{State: revier.AgentState{
		Harness: "claude", Name: "revier-c0", Activity: "one", Dir: "/p/revier/.claude/worktrees/delete-key"}}, revier.Project{Name: "revier"}, revier.AgentDetail{}, time.Now())
	for query, want := range map[string]string{"delete": "DELETE-key", "c0": "revier-C0"} {
		matches := ranked(ungrouped)(query, []string{it.FilterValue()})[0].MatchedIndexes
		for _, inPane := range []bool{false, true} {
			if got := ansi.Strip(agentLines(m.spun(), it, matches, 80, false, false, false, inPane)); !strings.Contains(got, want) {
				t.Errorf("%s, in pane %v: agent row = %q, want %q", query, inPane, got, want)
			}
		}
	}
}

// A name that holds the query whole has those letters lit, not the scattered
// ones the scorer prefers: it gives the p after the dash more than the p
// beside the a.
func TestAWholeMatchLightsItsOwnLetters(t *testing.T) {
	upper := lipgloss.NewStyle().Transform(strings.ToUpper)
	ranks := ranked(ungrouped)("cap", []string{"Cap-pool"})
	if len(ranks) != 1 {
		t.Fatalf("filter ranks = %v, want one match", ranks)
	}
	if got := highlight("Cap-pool", ranks[0].MatchedIndexes, lipgloss.NewStyle(), upper); got != "CAP-pool" {
		t.Errorf("highlight = %q, want the letters of cap lit together", got)
	}
}
