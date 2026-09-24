package tui

import (
	"cmp"
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

// agentKey names one agent across surveys. A panel id is unique only within
// the instance that holds it, so the instance names it too.
type agentKey struct {
	ref   revier.TargetRef
	panel revier.PanelID
}

func keyOf(a revier.AgentView) agentKey { return agentKey{ref: a.Ref, panel: a.Panel} }

// detailsMsg is the answer to askDetails: what each agent of one project said
// last.
type detailsMsg struct {
	project revier.ProjectName
	said    map[agentKey]revier.AgentDetail
}

// askDetails sends for what every agent of the project under the list's
// cursor said last: when another project comes under it, and again on every
// survey, so a turn that ends while it is shown appears. Every agent is read,
// not only the one shown, because which one is shown first depends on when
// each spoke. The read runs off the update loop, so the screen never waits on
// it (decisions.md D105).
func (m *Model) askDetails(msg tea.Msg) tea.Cmd {
	// Hidden, nobody reads the answer: the popup does no work off the screen.
	// A remote project's agents speak on the other machine (agentSaid).
	v, ok := m.selected()
	if !ok || len(v.Agents) == 0 || m.hidden || v.Project.Remote != nil {
		m.aasked = ""
		return nil
	}
	if _, survey := msg.(surveyMsg); v.Project.Name == m.aasked && !survey {
		return nil
	}
	m.aasked = v.Project.Name
	c, name, agents := m.core, v.Project.Name, slices.Clone(v.Agents)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), detailWait)
		defer cancel()
		said := map[agentKey]revier.AgentDetail{}
		for i, d := range c.Details(ctx, agents) {
			said[keyOf(agents[i])] = d
		}
		return detailsMsg{project: name, said: said}
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
// one at rest with something to report, then one still working, then one
// whose state is unknown. Among equals, the one that spoke last (decisions.md
// D106).
func (m Model) firstAgent(rows []agentRow) int {
	rank := map[revier.Status]int{
		revier.StatusAttention: 0,
		revier.StatusIdle:      1,
		revier.StatusRunning:   2,
		revier.StatusUnknown:   3,
	}
	best := 0
	for i := range rows {
		a, b := rows[i].agent, rows[best].agent
		if by := cmp.Compare(rank[a.State.Status], rank[b.State.Status]); by < 0 ||
			by == 0 && m.adetails[keyOf(a)].At.After(m.adetails[keyOf(b)].At) {
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
// (decisions.md D106).
//
// A remote project's agent says it on the other machine, and asking that
// revier for it is not built yet: the pane says so rather than stay blank
// (decisions.md D105).
func (m *Model) agentSaid(v revier.ProjectView, a revier.AgentView, w, rows int) string {
	th := m.theme
	room := rows - 2 // the heading and the blank line before it
	if room < 1 {
		return ""
	}
	note := func(s string) []string {
		parts := wrap(s, w)
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
		body = markdown(d.Message, w, th)
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
		b.WriteString(clipTo(line, w))
		b.WriteString("\n")
	}
	return b.String()
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
