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
	m := NewWithoutWaits(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
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

// secondLine is the second line of the row of an agent of project p that said
// d, w cells wide at now, as plain text.
func secondLine(a revier.AgentView, p revier.ProjectName, d revier.AgentDetail, now time.Time, w int) string {
	row := agentLines(theme.Default(), newAgentItem(a, revier.Project{Name: p}, d, now), nil, w, false, false, false, false)
	_, second, _ := strings.Cut(ansi.Strip(row), "\n")
	return second
}

// A second line short of room cuts the project and the worktree, and keeps
// the tokens and the cache whole.
func TestANarrowAgentRowKeepsTheUsage(t *testing.T) {
	a := revier.AgentView{State: revier.AgentState{Harness: "claude", Dir: "/p/revier/.claude/worktrees/agent-key-starts"}}
	now := time.Now()
	d := revier.AgentDetail{Context: 62_074, CacheUntil: now.Add(40 * time.Minute)}
	usage := strings.TrimSpace(ansi.Strip(newAgentItem(a, revier.Project{}, d, now).usage(theme.Default(), func(s lipgloss.Style) lipgloss.Style { return s })))
	width := func(left string) int { return 2 + listedStateWidth + lipgloss.Width(left) + gridGap + usageWidth + 1 }
	for _, c := range []struct {
		width int
		want  string
	}{
		{80, "revier · agent-key-starts"},
		{width("revier · agent-k…"), "revier · agent-k…"},
	} {
		row := strings.TrimSpace(secondLine(a, "revier", d, now, c.width))
		if !strings.HasPrefix(row, c.want+" ") || !strings.HasSuffix(row, " "+usage) {
			t.Errorf("w=%d: second line = %q, want %q and the whole %q", c.width, row, c.want, usage)
		}
	}
}

// A row too narrow for the usage columns leaves them out rather than cut
// them.
func TestARowTooNarrowForTheUsageLeavesItOut(t *testing.T) {
	a := revier.AgentView{State: revier.AgentState{Harness: "claude"}}
	now := time.Now()
	d := revier.AgentDetail{Context: 62_074, CacheUntil: now.Add(40 * time.Minute)}
	if row := secondLine(a, "revier", d, now, 2+listedStateWidth+usageWidth); strings.Contains(row, "62k") {
		t.Errorf("second line = %q, want no usage", row)
	}
}

// The tokens and the cache stand in columns: whatever their widths, each
// ends at the same cell on every row, under the age.
func TestTheUsageColumnsLineUp(t *testing.T) {
	a := revier.AgentView{State: revier.AgentState{Harness: "claude"}}
	now := time.Now()
	ends := map[string]int{}
	for _, d := range []revier.AgentDetail{
		{Context: 840, CacheUntil: now.Add(3 * time.Minute)},
		{Context: 62_074, CacheUntil: now.Add(time.Hour)},
		{Context: 1_250_000, CacheUntil: now.Add(-time.Minute)},
		{Context: 75_000},
	} {
		row := secondLine(a, "revier", d, now, 80)
		tokens := shortTokens(d.Context)
		end := lipgloss.Width(row[:strings.Index(row, tokens)+len(tokens)])
		if was, ok := ends["tokens"]; ok && was != end {
			t.Errorf("%q: the tokens end at %d, want %d", row, end, was)
		}
		ends["tokens"] = end
		if minutes, _ := cacheLeft(now, d.CacheUntil); minutes != "" {
			if end := lipgloss.Width(strings.TrimRight(row, " ")); ends["cache"] != 0 && end != ends["cache"] {
				t.Errorf("%q: the cache ends at %d, want %d", row, end, ends["cache"])
			} else {
				ends["cache"] = end
			}
		}
	}
}

// The row leaves the harness's name to the pane: on a row it said the
// project a second time.
func TestAnAgentRowLeavesTheNameOut(t *testing.T) {
	a := revier.AgentView{State: revier.AgentState{Harness: "claude", Name: "revier-c0"}}
	if row := secondLine(a, "revier", revier.AgentDetail{}, time.Now(), 80); strings.Contains(row, "revier-c0") {
		t.Errorf("second line = %q, want no name", row)
	}
}

// The tokens and the cache are gray; the cache alone takes the warning colour,
// and only in its last minutes.
func TestTheCacheIsLitOnlyInItsLastMinutes(t *testing.T) {
	th := theme.Default()
	th.Warning = lipgloss.NewStyle().Transform(strings.ToUpper)
	a := revier.AgentView{State: revier.AgentState{Harness: "claude"}}
	now := time.Now()
	for _, c := range []struct {
		until time.Duration
		want  string
	}{
		{40 * time.Minute, "40m"},
		{3 * time.Minute, "3M"},
		{-time.Minute, "--"},
	} {
		it := newAgentItem(a, revier.Project{Name: "revier"}, revier.AgentDetail{Context: 62_074, CacheUntil: now.Add(c.until)}, now)
		row := ansi.Strip(agentLines(th, it, nil, 80, false, false, false, false))
		if !strings.HasSuffix(strings.TrimRight(row, " "), " "+c.want) || strings.Contains(row, "62K") {
			t.Errorf("cache %v: row = %q, want it to end in %q and the tokens unlit", c.until, row, c.want)
		}
	}
}

// A token count fits its column, and a cache says its minutes rounded up,
// "--" once it is cold, and nothing where the harness said nothing.
func TestTheUsageAsTheRowSaysIt(t *testing.T) {
	for n, want := range map[int]string{0: "", 840: "840", 62_074: "62k", 999_999: "999k", 1_250_000: "1.2M", 12_500_000: "12M"} {
		if got := shortTokens(n); got != want || lipgloss.Width(got) > tokensWidth {
			t.Errorf("shortTokens(%d) = %q, want %q in %d cells", n, got, want, tokensWidth)
		}
	}
	now := time.Now()
	for _, c := range []struct {
		until time.Time
		want  string
		state cacheState
	}{
		{time.Time{}, "", cacheWarm},
		{now.Add(time.Hour), "60m", cacheWarm},
		{now.Add(59*time.Minute + 30*time.Second), "60m", cacheWarm},
		{now.Add(5 * time.Minute), "5m", cacheCooling},
		{now.Add(30 * time.Second), "1m", cacheCooling},
		{now, "--", cacheCold},
		{now.Add(-time.Hour), "--", cacheCold},
	} {
		if got, state := cacheLeft(now, c.until); got != c.want || state != c.state || lipgloss.Width(got) > minutesWidth {
			t.Errorf("cacheLeft(%v) = %q, %v; want %q, %v", c.until.Sub(now), got, state, c.want, c.state)
		}
	}
}

// No row is wider than the pane, however long its title, so a click and the
// hover land on the row drawn under the pointer.
func TestAPaneRowFitsThePane(t *testing.T) {
	m := NewWithoutWaits(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	title := strings.Repeat("a very long window title ", 4)
	long := newAgentItem(revier.AgentView{Target: "a-long-target-name", Panel: "1234", State: revier.AgentState{
		Harness: "cursor-agent-with-a-long-name", Name: "a-long-session-name", Activity: title, Dir: "/p/" + title,
	}}, revier.Project{Name: "a-long-project-name"}, revier.AgentDetail{At: time.Now()}, time.Now())
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

	m := NewWithoutWaits(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	m.theme.Match = upper
	it := newAgentItem(revier.AgentView{State: revier.AgentState{Harness: "claude", Activity: "— wide dash, then words"}}, revier.Project{Name: "alpha"}, revier.AgentDetail{}, time.Now())
	matches := ranked(ungrouped)("then", []string{it.filterValue(true)})[0].MatchedIndexes
	if got := ansi.Strip(agentLines(m.spun(), it, matches, 60, false, false, false, true)); !strings.Contains(got, "THEN words") {
		t.Errorf("agent row = %q, want the letters of then lit", got)
	}
}

// A query that matches the second line lights its letters there: the
// worktree, in the list and in the pane, which leaves the project out. The
// harness's name is not on the row, so no query matches it.
func TestTheMatchedLettersAreLitOnTheSecondLine(t *testing.T) {
	m := NewWithoutWaits(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	m.theme.Match = lipgloss.NewStyle().Transform(strings.ToUpper)
	it := newAgentItem(revier.AgentView{State: revier.AgentState{
		Harness: "claude", Name: "revier-c0", Activity: "one", Dir: "/p/revier/.claude/worktrees/delete-key"}}, revier.Project{Name: "revier"}, revier.AgentDetail{}, time.Now())
	if ranks := ranked(ungrouped)("c0", []string{it.filterValue(false)}); len(ranks) != 0 {
		t.Errorf("the query c0 matched %v, want the name not matched", ranks)
	}
	for query, want := range map[string]string{"delete": "DELETE-key"} {
		for _, inPane := range []bool{false, true} {
			matches := ranked(ungrouped)(query, []string{it.filterValue(inPane)})[0].MatchedIndexes
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
