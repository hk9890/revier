package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

	esc := tea.KeyMsg{Type: tea.KeyEsc}
	letter := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}
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
	if res, _ := al.key(sf, tea.KeyMsg{Type: tea.KeyDelete}); res.close == nil || res.close.project.Name != "alpha" {
		t.Errorf("result = %+v, want del to hand back the agent under the cursor", res)
	}
	if res, _ := al.key(sf, esc); !res.leave {
		t.Errorf("result = %+v, want Esc with no query to leave", res)
	}
}
