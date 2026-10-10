package tui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// The key that opens the surface switches it to the agents and back, and the
// bar's first button names where it goes.
func TestTheSwitchKeyShowsTheAgentListAndTheProjectsAgain(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)
	if bar := barLine(m); !strings.Contains(bar, "agents alt+space") {
		t.Fatalf("bar = %q, want the switch to the agents on it", bar)
	}
	m = switched(m)
	if bar := barLine(m); !strings.Contains(bar, "projects alt+space") {
		t.Errorf("bar = %q after the switch, want the switch back to the projects", bar)
	}
	if q := query(m); !strings.Contains(q, "filter agents") {
		t.Errorf("query line = %q, want the agent list's query", q)
	}
	if rows := listedRows(m); len(rows) != 1 || !strings.Contains(rows[0], "needs a decision") {
		t.Errorf("rows = %q, want the one agent of the open project", rows)
	}
	m = switched(m)
	if q := query(m); !strings.Contains(q, "filter projects") {
		t.Errorf("query line = %q after the second switch, want the projects", q)
	}
}

// The popup's key pressed while a screen with a field is up reaches the
// surface as the switch key. The screen has nothing to switch, and its field
// does not take the press as a space.
func TestTheSwitchKeyOverAScreenTypesNothing(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 140, 20)
	m, _ = press(m, "alt+n")
	m, _ = press(m, "x")
	before := query(m)
	m = switched(m)
	if after := query(m); after != before || !strings.Contains(barLine(m), "Add a project") {
		t.Errorf("field = %q after the switch key, was %q; want the screen as it was", after, before)
	}
}

// A row is the agent's state and what it is on, without its harness, over
// its project; the age of what it said last stands at the row's right end.
func TestAnAgentRowLeadsWithItsStateAndNamesItsProject(t *testing.T) {
	now := time.Now()
	m, _, _ := listedWorld(t, 140, 20,
		listed{project: "alpha", status: revier.StatusIdle, on: "Taskmgr tickets", said: "done", at: now.Add(-4 * time.Minute)})
	all := lines(m)
	first, _, _ := strings.Cut(all[4], "│")
	second, _, _ := strings.Cut(all[5], "│")
	if !strings.Contains(first, "idle") || !strings.Contains(first, "Taskmgr tickets") || strings.Contains(first, "claude") {
		t.Errorf("row = %q, want the state and the summary and no harness", first)
	}
	if strings.Index(first, "idle") > strings.Index(first, "Taskmgr tickets") {
		t.Errorf("row = %q, want the state before the summary", first)
	}
	if !strings.HasSuffix(strings.TrimSpace(first), "4m") {
		t.Errorf("row = %q, want the age at its right end", first)
	}
	if !strings.HasSuffix(strings.TrimSpace(second), " alpha") || column(second, "alpha") != column(first, "Taskmgr tickets") {
		t.Errorf("second line = %q, want the project under the summary of %q", second, first)
	}
}

// An agent that works in another directory than its project's shows it
// beside the project: that is how a worktree reads.
func TestAnAgentInAWorktreeNamesItBesideItsProject(t *testing.T) {
	m, _, _ := listedWorld(t, 140, 20,
		listed{project: "alpha", status: revier.StatusIdle, on: "one", dir: "/p/alpha"},
		listed{project: "alpha", status: revier.StatusIdle, on: "two", dir: "/p/alpha/.claude/worktrees/delete-key"})
	body := strings.Join(lines(m), "\n")
	if !strings.Contains(body, "alpha · delete-key") {
		t.Errorf("no row names the worktree:\n%s", body)
	}
	if strings.Count(body, "alpha ·") != 1 {
		t.Errorf("an agent in the project's own directory names a directory:\n%s", body)
	}
}

// An agent with no title yet shows the start of what it said last.
func TestAnAgentWithNoTitleShowsWhatItSaidLast(t *testing.T) {
	m, _, _ := listedWorld(t, 140, 20,
		listed{project: "alpha", status: revier.StatusIdle, said: "\n## Housekeeping done\nAll merged."})
	if rows := listedRows(m); len(rows) != 1 || !strings.Contains(rows[0], "## Housekeeping done") {
		t.Errorf("rows = %q, want the first line of the last message", rows)
	}
}

// The agents that need the user come first, then the working ones, then the
// ones at rest, then the unknown. Those that need the user and those at rest
// stand by when they spoke, the latest first; the working ones by project.
func TestTheAgentListIsOrderedByStateAndThenByWhenEachSpoke(t *testing.T) {
	now := time.Now()
	m, _, _ := listedWorld(t, 140, 40,
		listed{project: "beta", status: revier.StatusRunning, on: "work-b", at: now.Add(-time.Minute)},
		listed{project: "alpha", status: revier.StatusIdle, on: "idle-old", at: now.Add(-3 * time.Hour)},
		listed{project: "alpha", status: revier.StatusUnknown, on: "unknown-a"},
		listed{project: "alpha", status: revier.StatusAttention, on: "asks-old", at: now.Add(-2 * time.Hour)},
		listed{project: "beta", status: revier.StatusIdle, on: "idle-new", at: now.Add(-2 * time.Minute)},
		listed{project: "alpha", status: revier.StatusRunning, on: "work-a", at: now.Add(-time.Hour)},
		listed{project: "beta", status: revier.StatusAttention, on: "asks-new", at: now.Add(-5 * time.Minute)},
		listed{project: "beta", status: revier.StatusIdle, on: "idle-untimed"})
	var got []string
	for _, row := range listedRows(m) {
		for _, name := range []string{"asks-new", "asks-old", "work-a", "work-b", "idle-new", "idle-old", "idle-untimed", "unknown-a"} {
			if strings.Contains(row, name) {
				got = append(got, name)
			}
		}
	}
	want := []string{"asks-new", "asks-old", "work-a", "work-b", "idle-new", "idle-old", "idle-untimed", "unknown-a"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// The cursor stays on its agent when a survey reorders the rows, so the
// mirror beside it does not change under the reader.
func TestTheCursorFollowsItsAgentThroughAReordering(t *testing.T) {
	now := time.Now()
	m, _, fakes := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "the worker", screen: "worker screen"},
		listed{project: "alpha", status: revier.StatusIdle, on: "the rester", at: now.Add(-time.Hour), screen: "rester screen"})
	if row := selectedRow(t, m); !strings.Contains(row, "the worker") {
		t.Fatalf("selected = %q, want the first row", row)
	}
	// The worker ends its turn after the other spoke: it goes under the
	// rester's group head, to the top of the agents at rest, and the rester
	// stays above nothing. It moves from row 0 to row 0 of another group, so
	// the rester is made to need the user to move the worker down a row.
	fakes[0].State.Status = revier.StatusIdle
	fakes[1].State.Status = revier.StatusAttention
	m = survey(m).Said()
	rows := listedRows(m)
	if len(rows) != 2 || !strings.Contains(rows[0], "the rester") || !strings.Contains(rows[1], "the worker") {
		t.Fatalf("rows = %q, want the agent that needs the user first", rows)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "the worker") {
		t.Errorf("selected = %q after the reordering, want the cursor still on its agent", row)
	}
}

// The list opens before it knows when each agent spoke, and the answer
// reorders it. The cursor is on the first row of the order that knows: the
// agent that asked for the user last. A cursor the user moved before the
// answer stays on its agent.
func TestTheAgentListOpensOnTheAgentThatSpokeLast(t *testing.T) {
	now := time.Now()
	agents := []listed{
		{project: "alpha", status: revier.StatusAttention, on: "asks-old", at: now.Add(-2 * time.Hour)},
		{project: "beta", status: revier.StatusAttention, on: "asks-new", at: now.Add(-5 * time.Minute)},
		{project: "gamma", status: revier.StatusAttention, on: "asks-between", at: now.Add(-time.Hour)},
	}
	m, _, _ := listedSurface(t, 140, 30, agents...)
	m = switched(m)
	if row := selectedRow(t, m); !strings.Contains(row, "asks-old") {
		t.Fatalf("selected = %q before the answer, want the first row by project", row)
	}
	m = m.Said()
	if rows := listedRows(m); len(rows) != 3 || !strings.Contains(rows[0], "asks-new") {
		t.Fatalf("rows = %q, want the latest to speak first", rows)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "asks-new") {
		t.Errorf("selected = %q, want the first row, the agent that spoke last", row)
	}

	m, _, _ = listedSurface(t, 140, 30, agents...)
	m = switched(m)
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	m = m.Said()
	if row := selectedRow(t, m); !strings.Contains(row, "asks-between") {
		t.Errorf("selected = %q, want the cursor still on the agent the user moved it to", row)
	}
}

// The agent list opens on its first row every time, the project list keeps
// its cursor and its query across the switch, and a raise of the popup opens
// on the projects.
func TestTheSwitchKeepsTheProjectListAndTheRaiseOpensOnIt(t *testing.T) {
	now := time.Now()
	m, _, _ := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusAttention, on: "first", at: now},
		listed{project: "beta", status: revier.StatusIdle, on: "second", at: now})
	m, _ = press(m, "down")
	if row := selectedRow(t, m); !strings.Contains(row, "second") {
		t.Fatalf("selected = %q after down, want the second agent", row)
	}
	m = switched(m)
	m, _ = press(m, "b")
	if q := query(m); !strings.Contains(q, "b") || !strings.Contains(selectedRow(t, m), "beta") {
		t.Fatalf("query = %q, selected = %q; want the project query to select beta", q, selectedRow(t, m))
	}
	m = switched(m)
	if row := selectedRow(t, m); !strings.Contains(row, "first") {
		t.Errorf("selected = %q after switching back, want the agent list's first row", row)
	}
	m = switched(m)
	if q := query(m); !strings.Contains(q, "b") || !strings.Contains(selectedRow(t, m), "beta") {
		t.Errorf("query = %q, selected = %q after the round trip; want the project list as it was left", q, selectedRow(t, m))
	}

	m = switched(m).Raised()
	if q := query(m); !strings.Contains(q, "b") || strings.Contains(q, "filter agents") {
		t.Errorf("query line = %q after a raise, want the projects", q)
	}
}

// Typing filters the agents by what they are on and by where, and Esc clears
// the query before it leaves.
func TestTypingFiltersTheAgentList(t *testing.T) {
	m, _, _ := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "review the parser"},
		listed{project: "beta", status: revier.StatusIdle, on: "fix the build"})
	for _, r := range "build" {
		m, _ = press(m, string(r))
	}
	if rows := listedRows(m); len(rows) != 1 || !strings.Contains(rows[0], "fix the build") {
		t.Errorf("rows = %q, want the agent the query matches", rows)
	}
	if rule := ruleLine(m); !strings.Contains(rule, "1/2") {
		t.Errorf("rule = %q, want the count of the rows the query left", rule)
	}
	m, cmd := press(m, "esc")
	if cmd != nil || len(listedRows(m)) != 2 {
		t.Errorf("after esc: %d rows, command %v; want the query cleared and nothing else", len(listedRows(m)), cmd)
	}
	for _, r := range "beta" {
		m, _ = press(m, string(r))
	}
	if rows := listedRows(m); len(rows) != 1 || !strings.Contains(rows[0], "fix the build") {
		t.Errorf("rows = %q, want the agent of the project the query names", rows)
	}
}

// Enter on a row goes to the agent, as from the pane's Agents.
func TestEnterOnAnAgentRowGoesToTheAgent(t *testing.T) {
	m, rt, _ := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "one"},
		listed{project: "beta", status: revier.StatusIdle, on: "two"})
	m, _ = press(m, "down")
	_, cmd := press(m, "enter")
	runAll(cmd)
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{"2"}) {
		t.Errorf("panel focuses = %v, want the second agent's panel", rt.PanelFocuses)
	}
}

// del on a row closes the agent's tab, as del on its row in the pane does.
func TestDelOnAnAgentRowClosesTheAgent(t *testing.T) {
	m, rt, _ := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "one"},
		listed{project: "alpha", status: revier.StatusIdle, on: "two"})
	m, _ = press(m, "down")
	m, cmd := press(m, "delete")
	m, cmd = deliver(m, cmd)
	runAll(cmd)
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("closed panels = %v, want the second agent's", rt.ClosedPanels)
	}
	if q := query(m); !strings.Contains(q, "filter agents") {
		t.Errorf("query line = %q after the close, want the agent list still", q)
	}
}

// The rule totals the agents under it by state.
func TestTheAgentListsRuleTotalsItsRowsByState(t *testing.T) {
	m, _, _ := listedWorld(t, 140, 30,
		listed{project: "alpha", status: revier.StatusAttention, on: "one"},
		listed{project: "alpha", status: revier.StatusIdle, on: "two"},
		listed{project: "beta", status: revier.StatusIdle, on: "three"})
	rule, _, _ := strings.Cut(ruleLine(m), "│")
	if !strings.Contains(rule, "3/3") {
		t.Errorf("rule = %q, want the count of the agents", rule)
	}
	for _, want := range []string{" 1", " 2"} {
		if !strings.Contains(rule, want) {
			t.Errorf("rule = %q, want the total %q on it", rule, want)
		}
	}
}

// The pane names the agent as the project pane names a project: the agent in
// the title, and the project among the lines under it.
func TestTheAgentPaneNamesTheAgentAndItsProject(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusAttention, on: "Taskmgr tickets", said: "which one?",
			at: time.Now().Add(-4 * time.Minute), dir: "/p/alpha/.claude/worktrees/delete-key"})
	body := pane(m)
	for _, want := range []string{
		"Agent    Taskmgr tickets", "needs you", "Project  alpha",
		"Path     /p/alpha/.claude/worktrees/delete-key", "Git URL  https://example.com/alpha.git",
		"Harness  claude", "4 minutes ago",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pane has no %q:\n%s", want, body)
		}
	}
	if at, project := strings.Index(body, "Agent "), strings.Index(body, "Project "); at < 0 || at > project {
		t.Errorf("the agent is not the pane's title:\n%s", body)
	}
}

// Under its head the pane shows what the agent's panel shows, and follows it.
func TestThePaneMirrorsTheAgentsPanel(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen\nsecond line\n\n\n"},
		listed{project: "alpha", status: revier.StatusRunning, on: "two", screen: "the other panel"})
	body := pane(m)
	if !strings.Contains(body, "first screen") || !strings.Contains(body, "second line") || strings.Contains(body, "the other panel") {
		t.Fatalf("pane does not mirror the panel of the agent under the cursor:\n%s", body)
	}
	rt.Screens["1"] = "the screen moved on"
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "the screen moved on") || strings.Contains(body, "first screen") {
		t.Errorf("pane does not follow the panel:\n%s", body)
	}
	m, _ = press(m, "down")
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "the other panel") {
		t.Errorf("pane does not mirror the agent the cursor moved to:\n%s", body)
	}
}

// An agent that comes under the cursor is drawn with its own screen pending,
// and not over the screen of the agent the cursor left.
func TestTheScreenOfTheAgentTheCursorLeftIsNotShownUnderTheNext(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen"},
		listed{project: "alpha", status: revier.StatusRunning, on: "two", screen: "the other panel"})
	m, _ = press(m, "down")
	body := pane(m)
	if !strings.Contains(body, "Agent    two") || strings.Contains(body, "first screen") || !strings.Contains(body, "reading...") {
		t.Errorf("pane shows another agent's screen under this one's head:\n%s", body)
	}
}

// A mirror that comes back into view holds a screen read before it left: it
// is read again at once, and not a tick later.
func TestTheMirrorIsReadAgainWhenItComesBackIntoView(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen"})
	m, _ = press(m, "alt+h")
	m = m.MirrorTicked()
	asked := m.MirrorAsked()
	m, _ = press(m, "esc")
	if got := m.MirrorAsked(); got != asked+1 {
		t.Errorf("reads asked for = %d after the screen over the mirror left, want %d", got, asked+1)
	}
}

// A wheel up over a screen with nothing above it has no line to hold: the
// mirror follows the end again, and reads the scrollback no more.
func TestAWheelWithNothingAboveTheScreenLeavesTheMirrorFollowing(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "only line"})
	border := paneBorder(t, m)
	next, cmd := m.Update(tea.MouseMsg{X: border + 4, Y: 12, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("the wheel over the mirror asked for nothing, want a read of the scrollback")
	}
	next, _ = next.Update(cmd())
	m = next.(tui.Model)

	var screen []string
	for i := range 80 {
		screen = append(screen, fmt.Sprintf("row %02d", i))
	}
	rt.Screens["1"] = strings.Join(screen, "\n")
	m = m.Mirrored()
	if last := rt.ScreenReads[len(rt.ScreenReads)-1]; last.Scrollback {
		t.Errorf("last read = %+v, want the screen alone once nothing is above it", last)
	}
	if body := pane(m); !strings.Contains(body, "row 79") {
		t.Errorf("pane does not follow the end of the screen:\n%s", body)
	}
}

// A screen longer than the pane shows its end; the wheel scrolls back into
// the panel's scrollback, which is read only then.
func TestTheWheelScrollsTheMirrorIntoTheScrollback(t *testing.T) {
	var screen []string
	for i := range 80 {
		screen = append(screen, fmt.Sprintf("row %02d", i))
	}
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: strings.Join(screen[60:], "\n")})
	if body := pane(m); !strings.Contains(body, "row 79") || strings.Contains(body, "row 59") {
		t.Fatalf("pane does not show the end of the screen:\n%s", body)
	}
	for _, r := range rt.ScreenReads {
		if r.Scrollback {
			t.Fatalf("reads = %+v, want none of the scrollback before the user scrolls", rt.ScreenReads)
		}
	}
	rt.Screens["1"] = strings.Join(screen, "\n")
	border := paneBorder(t, m)
	next, cmd := m.Update(tea.MouseMsg{X: border + 4, Y: 12, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("the wheel over the mirror asked for nothing, want a read of the scrollback")
	}
	next, _ = next.Update(cmd())
	m = next.(tui.Model)
	if last := rt.ScreenReads[len(rt.ScreenReads)-1]; !last.Scrollback {
		t.Errorf("last read = %+v, want the scrollback", last)
	}
	if body := pane(m); strings.Contains(body, "row 79") || !strings.Contains(body, "row 76") {
		t.Errorf("pane is not scrolled up by a notch:\n%s", body)
	}
}

// A runtime that cannot read a panel leaves the mirror a note, and the head
// stands.
func TestAPanelThatCannotBeReadLeavesANote(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "one", screen: "never shown"})
	rt.ScreenErr = core.ErrNoScreen
	m, _ = press(m, "down")
	m = switched(switched(m)).Mirrored()
	body := pane(m)
	if !strings.Contains(body, "cannot be read") || !strings.Contains(body, "Agent    one") {
		t.Errorf("pane = %q, want the head and a note in the mirror's place", body)
	}
}

// The mirror keeps colour and drops everything else a screen carries: a line
// too long continues on the next in the style it was cut in, and every line
// is closed.
func TestScreenLinesKeepColourAndNothingElse(t *testing.T) {
	red := "\x1b[31m"
	got := tui.ScreenLines(red+"abcdef\x1b[39m\n"+
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\ \x1b[2Jcleared\x1b[>4;2m\r\n"+
		"\x1b[1;38;5;208mbold\x1b[0m plain\n\n\n", 4)
	want := []string{
		red + "abcd\x1b[m",
		red + "ef\x1b[39m\x1b[m",
		"link",
		" cle",
		"ared",
		"\x1b[1;38;5;208mbold\x1b[0m\x1b[m",
		" pla",
		"in",
	}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q\nwant    %q", got, want)
	}
}

// A character is as wide as the terminal draws it with what joins it: an
// emoji and its variation selector take two cells together, and the line is
// cut for two.
func TestScreenLinesMeasureACharacterWithWhatJoinsIt(t *testing.T) {
	got := tui.ScreenLines("⚠️ab", 3)
	want := []string{"⚠️a", "b"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}
