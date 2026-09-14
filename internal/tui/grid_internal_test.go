package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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

// The pane's Targets and Agents share one grid: the name, the state glyph and
// the key or activity each start in the same cell in both sections, whatever
// the harness is called.
func TestThePaneLinesUpTargetsAndAgents(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	g := m.theme.Glyphs
	const w = 60
	target := m.detailRow(targetRow{target: revier.TargetView{
		Name: "home", Key: "ctrl+shift+u", Available: true, Ref: revier.TargetRef{Host: "rt", ID: "1"},
	}}, w, false, false)
	attached := m.detailRow(targetRow{attached: revier.TargetRef{Host: "wm", ID: "2", Title: "Pull requests"}}, w, false, false)

	for _, harness := range []string{"claude", "gemini-cli", "cursor-agent-with-a-long-name"} {
		agent := m.detailAgent(revier.AgentView{State: revier.AgentState{
			Harness: harness, Status: revier.StatusAttention, Activity: "needs a decision",
		}}, w)
		name := harness[:min(len(harness), detailNameWidth-1)]
		if a, b := column(t, target, "home"), column(t, agent, name); a != b {
			t.Errorf("%s: target name at %d, harness at %d", harness, a, b)
		}
		if a, b := column(t, target, g.Running+" running"), column(t, agent, g.NeedsYou+" needs you"); a != b {
			t.Errorf("%s: target state at %d, agent state at %d", harness, a, b)
		}
		if a, b := column(t, target, "ctrl+shift+u"), column(t, agent, "needs a decision"); a != b {
			t.Errorf("%s: target key at %d, activity at %d", harness, a, b)
		}
	}
	if a, b := column(t, target, g.Running+" running"), column(t, attached, g.Running+" running"); a != b {
		t.Errorf("target state at %d, attached state at %d", a, b)
	}
}

// No row is wider than the pane, however long its title, so a click and the
// hover land on the row drawn under the pointer.
func TestAPaneRowFitsThePane(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	title := strings.Repeat("a very long window title ", 4)
	for _, w := range []int{20, 42, 60} {
		rows := []string{
			m.detailRow(targetRow{attached: revier.TargetRef{Host: "wm", ID: "2", Title: title}}, w, false, false),
			m.detailRow(targetRow{target: revier.TargetView{Name: "a-long-target-name", Key: "ctrl+shift+alt+u"}}, w, false, false),
			m.detailAgent(revier.AgentView{State: revier.AgentState{Harness: "cursor-agent-with-a-long-name", Activity: title}}, w),
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
