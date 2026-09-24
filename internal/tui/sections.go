package tui

import (
	"context"
	"slices"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The cursor stands in the project list or the pane's Agents, and Tab moves it
// between the two; shift+tab walks back. Each has its own query field over its
// own rows, so what is typed filters the rows under it, and Enter acts on a
// row of its own kind: a project opens, an agent's tab comes to the front. A
// project with no agents has only the list.
//
// The pane's targets take the cursor too, but not by Tab: a click on one, or
// alt+t, puts it there, and Tab takes it back to the list. They have no
// query, so typing there filters nothing (decisions.md D104).

// sections are the sections the cursor can be in, for the project under it.
func (m Model) sections() []focus {
	v, ok := m.selected()
	if !ok {
		return []focus{focusList}
	}
	out := []focus{focusList, focusTargets}
	if len(v.Agents) > 0 {
		out = append(out, focusAgents)
	}
	return out
}

// step moves the cursor by sections, forward for 1 and back for -1: between
// the list and the agents, and from the targets back to the list.
func (m Model) step(by int) (tea.Model, tea.Cmd) {
	if m.focus == focusTargets {
		return m.focusOn(focusList), nil
	}
	s := slices.DeleteFunc(m.sections(), func(f focus) bool { return f == focusTargets })
	i := max(slices.Index(s, m.focus), 0)
	return m.focusOn(s[(i+by+len(s))%len(s)]), nil
}

// focusOn puts the cursor in a section. Only its field shows a cursor, so the
// field a key lands in is the one that looks typed into. The cursor does not
// blink after the first move: a blink is a timer the field would have to
// start on every Tab.
func (m Model) focusOn(f focus) Model {
	m.focus = f
	m.input.Blur()
	m.ainput.Blur()
	if in := m.field(); in != nil {
		_ = in.Focus()
	}
	return m
}

// toList brings the cursor back to the project list, as a screen standing
// over the surface does before it opens.
func (m *Model) toList() {
	*m = m.focusOn(focusList)
}

// field is the query field of the section the cursor is in, or nil in the
// targets, which have none.
func (m *Model) field() *textinput.Model {
	switch m.focus {
	case focusTargets:
		return nil
	case focusAgents:
		return &m.ainput
	}
	return &m.input
}

// query applies a section's query to its rows. The first match is selected,
// as the list selects it on a project query: typing an agent's name is a
// choice of that agent. An empty agent query hands the choice back to the
// pane (chooseAgent).
func (m *Model) query(f focus, q string) {
	switch f {
	case focusAgents:
		m.afilter, m.achosen = q, agentKey{}
		m.ainput.SetValue(q)
		if q != "" {
			m.pickAgent(0)
		}
	default:
		m.setFilter(q)
	}
}

// forgetPane drops the pane's query and cursors, and the agent the user
// chose. They are about one project's rows, and mean nothing over another's.
func (m *Model) forgetPane() {
	m.tcursor = 0
	m.pclick = paneClick{} // a click on the last project's row is not half of a double click on this one's
	m.query(focusAgents, "")
}

// endPaneSearch drops the agent query over the same project, so the cursor
// stays on the agent it reached rather than going back to the pane's choice.
func (m *Model) endPaneSearch() {
	if m.afilter != "" {
		rows, i := m.agentRows(), m.acursor
		m.query(focusAgents, "")
		if i < len(rows) {
			m.pickAgent(slices.IndexFunc(m.agentRows(), rows[i].same))
		}
	}
}

// agentRow is one row of the pane's Agents section. matches are the byte
// positions of its label the query matched, for the highlight.
type agentRow struct {
	agent   revier.AgentView
	matches []int
}

// label is what the agent query matches: the harness, and what the agent is
// doing, which is where a conversation's name shows.
func (r agentRow) label() string {
	return harnessOf(r.agent) + " " + r.agent.State.Activity
}

// same reports whether two rows are one agent, whatever the query matched.
func (r agentRow) same(o agentRow) bool {
	return keyOf(r.agent) == keyOf(o.agent)
}

func harnessOf(a revier.AgentView) string {
	if a.State.Harness == "" {
		return "agent"
	}
	return a.State.Harness
}

// agentRows is the pane's Agents section: every agent of the project under
// the cursor, or, while an agent query is typed, the ones it matches, ranked
// as the list ranks projects.
func (m Model) agentRows() []agentRow {
	v, ok := m.selected()
	if !ok {
		return nil
	}
	all := make([]agentRow, len(v.Agents))
	labels := make([]string, len(v.Agents))
	for i, a := range v.Agents {
		all[i] = agentRow{agent: a}
		labels[i] = all[i].label()
	}
	if m.afilter == "" {
		return all
	}
	var out []agentRow
	for _, rank := range list.DefaultFilter(m.afilter, labels) {
		r := all[rank.Index]
		r.matches = rank.MatchedIndexes
		out = append(out, r)
	}
	return out
}

// goAgentRow brings the agent of one row of the Agents section to the front:
// the panel here that shows it, for a link's agent too, unless the link's
// workspace is still coming up.
func (m Model) goAgentRow(i int) tea.Cmd {
	rows := m.agentRows()
	v, ok := m.selected()
	if i < 0 || i >= len(rows) || !ok {
		return nil
	}
	p, ok := m.project(v.Project.Name)
	if !ok {
		return nil
	}
	c, root, agent := m.core, m.stateRoot, rows[i].agent
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), core.BindWait)
		defer cancel()
		_, err := c.ActivateAgentWaiting(ctx, p, agent, core.StateLedger{Root: root})
		return actedMsg{err: err}
	}
}

// lineSpan is the pane lines a row takes, start inclusive and end exclusive:
// an agent's row wraps its activity onto as many lines as it needs.
type lineSpan struct{ start, end int }
