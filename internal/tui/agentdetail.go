package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/pkg/revier"
)

// detailWait bounds one read of what a project's agents said: a host listing
// and the end of a file per agent, which answer in milliseconds.
const detailWait = 2 * time.Second

// maxPaneWidth is the widest the pane's text is set: past it a message's line
// is too long to read, and a wider terminal leaves the pane as it was.
const maxPaneWidth = 100

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
// cursor said last: when another project comes under it, and again on every
// survey, so a turn that ends while it is shown appears. Every agent is read,
// not only the one shown, because which one is shown first depends on when
// each spoke. The read runs off the update loop, so the screen never waits on
// it (decisions.md D106).
func (m *Model) askDetails(msg tea.Msg) tea.Cmd {
	// Nobody reads an answer the pane does not show: the popup hidden, a
	// terminal too narrow for the pane beside the list, or a screen over it.
	// A remote project's agents speak on the other machine (agentSaid).
	v, ok := m.selected()
	if !ok || len(v.Agents) == 0 || m.hidden || m.paneCols() == 0 || m.dialog != dialogNone || v.Project.Remote != nil {
		m.aasked = ""
		return nil
	}
	if _, survey := msg.(surveyMsg); v.Project.Name == m.aasked && !survey {
		return nil
	}
	m.aasked = v.Project.Name
	m.aseq++
	c, name, seq, agents := m.core, v.Project.Name, m.aseq, slices.Clone(v.Agents)
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

// took takes in an answer to askDetails. One for a project the cursor has
// since left is dropped, and so is one older than the answer already taken:
// the asks are not answered in order. An agent the answer has nothing for
// keeps what it said before, so one read that failed does not blank a message
// the pane was showing.
func (m *Model) took(msg detailsMsg) {
	if msg.project != m.aasked || msg.seq <= m.atook {
		return
	}
	m.atook = msg.seq
	said := make(map[agentKey]revier.AgentDetail, len(msg.said))
	for k, d := range msg.said {
		if before := m.adetails[k]; d == (revier.AgentDetail{}) && before != (revier.AgentDetail{}) {
			d = before
		}
		said[k] = d
	}
	m.adetails = said
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

// chooseAgent puts the pane's cursor on the agent to show: the one the user
// last chose while it is still there, and otherwise the one most worth a look
// (firstAgent).
func (m *Model) chooseAgent() {
	rows := m.agentRows()
	if at := slices.IndexFunc(rows, func(r agentRow) bool { return keyOf(r.agent) == m.achosen }); at >= 0 {
		m.acursor = at
		return
	}
	m.acursor = m.firstAgent(rows)
}

// firstAgent is the row most worth a look: an agent that needs the user, then
// one with a message the pane can show, and among those one at rest, then one
// still working, then one whose state is unknown. Among equals, the one that
// spoke last (decisions.md D107). An agent that needs the user comes first
// with nothing to show, because its row says what matters.
func (m Model) firstAgent(rows []agentRow) int {
	rank := map[revier.Status]int{
		revier.StatusAttention: 0,
		revier.StatusIdle:      1,
		revier.StatusRunning:   2,
		revier.StatusUnknown:   3,
	}
	worth := func(a revier.AgentView) [3]int {
		silent := 1
		if m.adetails[keyOf(a)].Message != "" {
			silent = 0
		}
		if a.State.Status == revier.StatusAttention {
			return [3]int{0, 0, 0}
		}
		return [3]int{1, silent, rank[a.State.Status]}
	}
	best := 0
	for i := range rows {
		a, b := rows[i].agent, rows[best].agent
		wa, wb := worth(a), worth(b)
		by := slices.Compare(wa[:], wb[:])
		if by < 0 || by == 0 && m.adetails[keyOf(a)].At.After(m.adetails[keyOf(b)].At) {
			best = i
		}
	}
	return best
}

// agentSaid is the pane's part below the facts: the last thing the agent
// under the pane's cursor said, and how long ago, so the user can tell whether
// it is worth going to. The name and the state are on the row above and are
// not repeated. rows counts the heading.
//
// A message that does not fit keeps its end, under an ellipsis: an agent ends
// on what it did and what it needs, which is what the pane is read for
// (decisions.md D107).
//
// A remote project's agent says it on the other machine, and asking that
// revier for it is not built yet: the pane says so rather than stay blank
// (decisions.md D106).
//
// The heading runs w wide, as the pane's other headings do; the text is set
// tw wide.
func (m *Model) agentSaid(v revier.ProjectView, a revier.AgentView, w, tw, rows int) string {
	th := m.theme
	room := rows - 2 // the heading and the blank line before it
	if room < 1 {
		return ""
	}
	note := func(s string) []string {
		parts := wrap(s, tw)
		for i := range parts {
			parts[i] = th.Meta.Render(parts[i])
		}
		return parts
	}
	var head, body []string
	d, answered := m.adetails[keyOf(a)]
	switch {
	case v.Project.Remote != nil:
		body = note("The last message of an agent on another machine is not implemented yet.")
	case !answered:
		body = note("reading...")
	case d.Message == "":
		body = note("Nothing this agent said can be read.")
	default:
		if !d.At.IsZero() {
			head = []string{th.Meta.Render(ago(m.now(), d.At)), ""}
		}
		body = m.setMessage(d.Message, tw)
	}
	if len(head)+len(body) > room {
		tail := body[len(body)-max(room-len(head)-1, 0):]
		for len(tail) > 0 && tail[0] == "" {
			tail = tail[1:]
		}
		body = append([]string{th.Meta.Render("...")}, tail...)
	}
	lines := append(head, body...)
	lines = lines[max(len(lines)-room, 0):]
	var b strings.Builder
	b.WriteString(m.heading("Last message", w))
	for _, line := range lines {
		b.WriteString(clipTo(line, tw))
		b.WriteString("\n")
	}
	return b.String()
}

// setMessage is a message set as the pane's lines at a width, kept while the
// message and the width stay: the pane is drawn again for every frame of a
// working agent's spinner, and a long message costs a wrap of every line.
func (m *Model) setMessage(text string, w int) []string {
	if m.amessage.lines == nil || m.amessage.text != text || m.amessage.w != w {
		m.amessage = setMessage{text: text, w: w, lines: markdown(text, w, m.theme)}
	}
	return m.amessage.lines
}

// setMessage is the last message set, and what it was set from.
type setMessage struct {
	text  string
	w     int
	lines []string
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
