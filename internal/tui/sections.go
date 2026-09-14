package tui

import (
	"context"
	"slices"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/pkg/revier"
)

// The cursor stands in one of three sections: the project list, and the
// pane's Targets and Agents. Each has its own query field over its own rows,
// so what is typed filters the rows under it, and Enter acts on a row of its
// own kind: a project opens, a target runs, an agent's tab comes to the front.
// Tab walks the sections in that order and shift+tab walks back; a section
// with no rows is passed over (decisions.md D73).

// sections are the sections Tab walks, in order, for the project under the
// cursor.
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

// step moves the cursor by sections, forward for 1 and back for -1.
func (m Model) step(by int) (tea.Model, tea.Cmd) {
	s := m.sections()
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
	m.tinput.Blur()
	m.ainput.Blur()
	_ = m.field().Focus()
	return m
}

// toList brings the cursor back to the project list, as a screen standing
// over the surface does before it opens.
func (m *Model) toList() {
	*m = m.focusOn(focusList)
}

// field is the query field of the section the cursor is in.
func (m *Model) field() *textinput.Model {
	switch m.focus {
	case focusTargets:
		return &m.tinput
	case focusAgents:
		return &m.ainput
	}
	return &m.input
}

// query applies a section's query to its rows. The first match is selected,
// as the list selects it on a project query.
func (m *Model) query(f focus, q string) {
	switch f {
	case focusTargets:
		m.tfilter, m.tcursor = q, 0
		m.tinput.SetValue(q)
	case focusAgents:
		m.afilter, m.acursor = q, 0
		m.ainput.SetValue(q)
	default:
		m.setFilter(q)
	}
}

// forgetPane drops the pane's queries and cursors. They are about one
// project's rows, and mean nothing over another's.
func (m *Model) forgetPane() {
	m.query(focusTargets, "")
	m.query(focusAgents, "")
}

// agentRow is one row of the pane's Agents section. matches are the rune
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

// goAgentRow brings the agent of one row of the Agents section to the front.
// For a link the pane onto its host is raised as well, and bound where it
// landed, as a Go binds it.
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
	c, bound, panel := m.core, m.bound[p.Name], rows[i].agent.Panel
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), bindWait)
		defer cancel()
		res, err := c.GoAgent(ctx, p, panel, bound)
		if err != nil || res.Ref.IsZero() {
			return actedMsg{err: err}
		}
		return actedMsg{bind: &binding{project: p.Name, target: res.Target, ref: res.Ref}}
	}
}

// lineSpan is the pane lines a row takes, start inclusive and end exclusive:
// an agent's row wraps its activity onto as many lines as it needs.
type lineSpan struct{ start, end int }
