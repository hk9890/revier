package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The agent list runs with no root model: its rows in the order of their
// states, the query, and the three presses it hands to the surface - Esc with
// no query, del on a row, and a key that is not its own.
func TestTheAgentListFiltersAndHandsBackWithNoRootModel(t *testing.T) {
	agent := func(id, activity string, status revier.Status) revier.AgentView {
		return revier.AgentView{
			Ref: revier.TargetRef{Host: "rt", ID: id}, Panel: "1",
			State: revier.AgentState{Harness: "claude", Status: status, Activity: activity},
		}
	}
	views := []revier.ProjectView{
		{Project: revier.Project{Name: "alpha"}, Agents: []revier.AgentView{agent("1", "writing tests", revier.StatusRunning)}},
		{Project: revier.Project{Name: "beta"}, Agents: []revier.AgentView{agent("2", "asks which branch", revier.StatusAttention)}},
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), list: 60, pane: 40, page: 5}
	al := newAgentScreen(th)
	al.reload(agentItems(views, nil, time.Now()))
	if it, ok := al.selected(); !ok || it.project.Name != "beta" {
		t.Fatalf("selected = %+v, want the agent that needs the user first", it)
	}
	if got := al.totals(); got[revier.StatusRunning] != 1 || got[revier.StatusAttention] != 1 {
		t.Errorf("totals = %v, want one agent in each state", got)
	}

	esc := pressedKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	letter := pressedKey(tea.KeyPressMsg{Code: 'g', Text: "g"})
	al.query.Focus()
	if res, _ := al.key(sf, letter); !res.rest {
		t.Fatalf("result = %+v, want a letter handed back: the bar is asked before the query", res)
	}
	al.edit(letter)
	if it, ok := al.selected(); al.filter != "g" || !ok || it.project.Name != "alpha" {
		t.Fatalf("filter = %q, selected = %+v, want the query on the agent it matches", al.filter, it)
	}
	if res, _ := al.key(sf, esc); res.leave || al.filter != "" {
		t.Fatalf("result = %+v, filter = %q, want Esc to clear the query first", res, al.filter)
	}
	if res, _ := al.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyDelete})); res.close == nil || res.close.project.Name != "alpha" {
		t.Errorf("result = %+v, want del to hand back the agent under the cursor", res)
	}
	if res, _ := al.key(sf, esc); !res.leave {
		t.Errorf("result = %+v, want Esc with no query to leave", res)
	}
}

// The age column says each moment in its largest whole unit, and moves to the
// next unit exactly at its boundary: a day old reads 1d, not 24h.
func TestTheAgeColumnChangesUnitAtEachBoundary(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{0, "now"},
		{59 * time.Second, "now"},
		{time.Minute, "1m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23*time.Hour + 59*time.Minute, "23h"},
		{24 * time.Hour, "1d"},
		{49 * time.Hour, "2d"},
	} {
		if got := shortAgo(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("shortAgo(%v ago) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := shortAgo(now, time.Time{}); got != "" {
		t.Errorf("shortAgo(zero) = %q, want no age", got)
	}
}
