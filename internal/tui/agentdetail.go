package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/pkg/revier"
)

// detailWait bounds one read of what a project's agents said: a host listing
// and the end of a file per agent, which answer in milliseconds.
const detailWait = 2 * time.Second

// agentKey names one agent across surveys: the instance that holds it, by
// host and id as the core names one, and its panel, whose id is unique only
// within that instance. A ref's title is not part of it: a window host can
// retitle an instance every survey.
type agentKey struct {
	host, id string
	panel    revier.PanelID
}

func keyOf(a revier.AgentView) agentKey {
	return agentKey{host: a.Ref.Host, id: a.Ref.ID, panel: a.Panel}
}

// detailsMsg is the answer to askDetails: what each agent of one project said
// last, and which ask it answers.
type detailsMsg struct {
	project revier.ProjectName
	seq     int
	said    map[agentKey]revier.AgentDetail
}

// askDetails sends for what every agent of the project under the list's
// cursor said last, and when: when another project comes under it, and again
// on every survey. The pane draws none of it (decisions.md D125): which agent
// it mirrors first depends on when each spoke. The read runs off the update
// loop, so the screen never waits on it (D106).
//
// The agent list asks for every agent on the surface instead: its rows stand
// in the order the agents spoke in (D110).
func (m *Model) askDetails(msg tea.Msg) tea.Cmd {
	name, agents := m.detailsWanted()
	if len(agents) == 0 {
		m.aasked = ""
		return nil
	}
	if _, survey := msg.(surveyMsg); name == m.aasked && !survey {
		return nil
	}
	m.aasked = name
	m.aseq++
	c, seq := m.core, m.aseq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), detailWait)
		defer cancel()
		said := map[agentKey]revier.AgentDetail{}
		for i, d := range c.Details(ctx, agents) {
			said[keyOf(agents[i])] = d
		}
		return detailsMsg{project: name, seq: seq, said: said}
	}
}

// everyAgent stands for the agent list where an ask for details names a
// project. No project has the name: a project's name is a file's.
const everyAgent revier.ProjectName = "\x00agents"

// detailsWanted is the agents whose last word the surface reads now, and the
// project they are asked for under: one project's beside the project list,
// every project's on the agent list. None is nothing to ask.
func (m Model) detailsWanted() (revier.ProjectName, []revier.AgentView) {
	// Nobody reads an answer the surface does not show: the popup hidden, or
	// a screen over the list.
	if m.hidden || m.dialog != dialogNone {
		return "", nil
	}
	if m.agents.shown {
		var all []revier.AgentView
		for _, v := range m.views {
			all = append(all, v.Agents...)
		}
		return everyAgent, all
	}
	// Beside the project list the pane alone reads it: not on a terminal too
	// narrow for the pane, and not for a remote project, whose agents speak
	// on the other machine.
	v, ok := m.selected()
	if !ok || m.paneCols() == 0 || v.Project.Remote != nil {
		return "", nil
	}
	return v.Project.Name, slices.Clone(v.Agents)
}

// took takes in an answer to askDetails. One for a project the cursor has
// since left is dropped, and so is one older than the answer already taken:
// the asks are not answered in order. An agent the answer has nothing for
// keeps what it said before, so one read that failed does not blank the row
// of the agent list that shows it.
func (m *Model) took(msg detailsMsg) {
	if msg.project != m.aasked || msg.seq <= m.atook {
		return
	}
	m.atook, m.aanswered = msg.seq, msg.project
	said := make(map[agentKey]revier.AgentDetail, len(msg.said))
	for k, d := range msg.said {
		// A turn with no message is one whose last message is further back
		// than the probe reads, and the one shown is still the last.
		switch before := m.adetails[k]; {
		case d.IsZero():
			d = before
		case d.Message == "" && before.Message != "":
			d.Message, d.At = before.Message, before.At
		}
		said[k] = d
	}
	m.adetails = said
	m.reloadAgents()
	// The agent list opened before this answer, on the first row of an order
	// that did not know when each agent spoke. The first row of the order
	// that does is the one it opens on (switchList).
	if m.agents.shown && m.agents.top {
		m.agents.top = false
		m.agents.list.Select(0)
	}
}

// pickAgent puts the pane's cursor on an agent row the user moved it to, and
// keeps it there: the pane stops choosing for this project. Moving into the
// Agents is not a choice, so Tab leaves the pane's own choice standing.
func (m *Model) pickAgent(i int) {
	rows := m.agentRows()
	m.acursor = clampRow(i, len(rows))
	if m.acursor < len(rows) {
		m.achosen = keyOf(rows[m.acursor].agent)
	}
}

// chooseAgent puts the pane's cursor on the agent to mirror: the one the user
// last chose while it is still there, and otherwise the one most worth a look
// (firstAgent).
//
// The pane keeps the agent it chose while no other is more worth a look: an
// agent that speaks does not take the mirror, and the scroll in it, from its
// equal. And it does not choose among equals before it knows when each spoke,
// so the mirror is not read for one agent and then for another.
func (m *Model) chooseAgent() {
	rows := m.agentRows()
	at := func(k agentKey) int {
		return slices.IndexFunc(rows, func(r agentRow) bool { return keyOf(r.agent) == k })
	}
	m.apending = false
	if i := at(m.achosen); i >= 0 {
		m.acursor = i
		return
	}
	m.acursor = m.firstAgent(rows)
	if len(rows) == 0 {
		return
	}
	rank := agentRank[rows[m.acursor].agent.State.Status]
	if i := at(m.akept); i >= 0 && agentRank[rows[i].agent.State.Status] == rank {
		m.acursor = i
		return
	}
	equals := 0
	for _, r := range rows {
		if agentRank[r.agent.State.Status] == rank {
			equals++
		}
	}
	if name, _ := m.detailsWanted(); equals > 1 && name != "" && name != m.aanswered {
		m.apending = true
		return
	}
	m.akept = keyOf(rows[m.acursor].agent)
}

// agentRank orders the agents by how much each is worth a look: one that
// needs the user, then one at rest, then one still working, then one whose
// state is unknown.
var agentRank = map[revier.Status]int{
	revier.StatusAttention: 0,
	revier.StatusIdle:      1,
	revier.StatusRunning:   2,
	revier.StatusUnknown:   3,
}

// firstAgent is the row most worth a look, by agentRank. Among equals, the
// one that spoke last (decisions.md D125).
func (m Model) firstAgent(rows []agentRow) int {
	best := 0
	for i := range rows {
		a, b := rows[i].agent, rows[best].agent
		by := agentRank[a.State.Status] - agentRank[b.State.Status]
		if by < 0 || by == 0 && m.adetails[keyOf(a)].At.After(m.adetails[keyOf(b)].At) {
			best = i
		}
	}
	return best
}

// ago is how long before now a moment was, in the largest whole unit.
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return count(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return count(int(d/time.Hour), "hour") + " ago"
	}
	return count(int(d/(24*time.Hour)), "day") + " ago"
}

func count(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
