package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The list's width bounds, the least the pane needs beside it, and the widest
// the pane sets its facts when it lays the message beside them.
//
// The list is a table of two columns - the project, and its agent's state -
// and stops at the width that holds both whole. Past that every column goes
// to the pane. Below the least the two need together, there is no pane.
const (
	minListWidth  = 56
	maxListWidth  = 80
	minPaneWidth  = 44
	maxFactsWidth = 80
)

// widePaneWidth is the pane width at which the pane lays the message beside
// the facts: the least that holds the facts and a message maxPaneWidth wide.
const widePaneWidth = paneChrome + maxFactsWidth + gridGap + maxPaneWidth

// paneWidth is what the detail pane gets, or zero when the terminal is too
// narrow to give both the list and the pane their least. The list takes half
// up to its cap, as the picker split its preview (os-fzf.sh:782), and the
// pane takes the rest.
func (m Model) paneWidth() int {
	inner, _ := m.inner()
	if !m.dialog.hasPane() || inner < minListWidth+minPaneWidth {
		return 0
	}
	list := min(max(inner/2, minListWidth), maxListWidth)
	return inner - list
}

// paneCols is the columns the pane renders in: its share beside the list,
// or, on a terminal too narrow to split, the whole width while the cursor is
// on it, where it stands in the list's place (decisions.md D105). Zero is a
// pane not on screen.
func (m Model) paneCols() int {
	if pane := m.paneWidth(); pane > 0 {
		return pane
	}
	if m.focus != focusList {
		w, _ := m.inner()
		return w
	}
	return 0
}

// paneChrome is the border column and the padding column the pane's frame
// takes from what it holds.
const paneChrome = 2

// paneRise is the rows the pane beside the list starts above the list's rows:
// the query line and the rule, which stand in the list's column.
const paneRise = 2

func newDetail(th theme.Theme) viewport.Model {
	v := viewport.New(0, 0)
	// A border takes its own colour and not the style's foreground: without
	// one it is drawn in the terminal's text colour, brighter than the rules
	// it meets.
	v.Style = th.Border.
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(th.Border.GetForeground()).
		PaddingLeft(1)
	return v
}

// syncDetail rebuilds the pane for whatever the cursor is on. It runs after
// every message, because the cursor moves on a keypress and the content
// changes on a survey. The wheel scrolls the pane; a survey keeps that
// scroll, and a different project starts at its top. The pane's own cursor
// is kept on a row that exists, and on the screen, and in a section that
// exists: an agent that exits takes its section with it, and the cursor goes
// back to the list, where Tab would have taken it.
func (m *Model) syncDetail() {
	if !slices.Contains(m.sections(), m.focus) {
		*m = m.focusOn(focusList)
	}
	cols := m.paneCols()
	if cols == 0 {
		return
	}
	// A viewport's width is its outside, border and padding included. Beside
	// the list the pane also takes the query line and the rule.
	_, h := m.inner()
	if m.paneWidth() > 0 {
		h += paneRise
	}
	m.detail.Width, m.detail.Height = cols, h
	switch m.dialog {
	case dialogHosts, dialogNew, dialogConfig, dialogProject:
		m.detail.SetContent("")
		return
	case dialogRemote, dialogLinkName:
		m.detail.SetContent(m.remoteDetail())
		return
	case dialogSessions, dialogSessionName:
		m.detail.SetContent(m.sessionDetail())
		return
	case dialogShutdown:
		m.detail.SetContent(m.shutdownDetail())
		return
	}
	v, ok := m.selected()
	if !ok {
		m.detail.SetContent("")
		return
	}
	fresh := v.Project.Name != m.shown
	if fresh {
		m.shown = v.Project.Name
		m.forgetPane()
	}
	m.tcursor = clampRow(m.tcursor, len(m.targetRows()))
	m.chooseAgent()
	m.detail.SetContent(m.detailContent(v))
	if fresh {
		m.detail.GotoTop()
	}
	switch {
	case m.focus == focusTargets && m.tcursor < len(m.tlines):
		m.followPane(m.tlines[m.tcursor])
	case m.focus == focusAgents && m.acursor < len(m.alines):
		// The last line first, so a row taller than the pane shows its start.
		m.followPane(m.alines[m.acursor].end - 1)
		m.followPane(m.alines[m.acursor].start)
	}
}

// followPane keeps a line of the pane on the screen, scrolling by the least
// that does it, as the list does for its cursor.
func (m *Model) followPane(line int) {
	switch {
	case line < m.detail.YOffset:
		m.detail.SetYOffset(line)
	case line >= m.detail.YOffset+m.detail.Height:
		m.detail.SetYOffset(line - m.detail.Height + 1)
	}
}

// detailContent is what this project is, then what is up, then what its agents
// are doing, then what the agent under the pane's cursor said last.
//
// A narrow pane stacks them, and the message takes the rows the others leave.
// A wide pane puts the message beside the rest, so it has the whole height
// (decisions.md D107). The message's text is set maxPaneWidth wide on both
// sides of the switch, so a resize across it moves the message and does not
// re-wrap it; widePaneWidth is the least pane that holds it that wide beside
// the facts. Only the text is held to that width: the facts, the rows and the
// rules run to the pane's edge.
func (m *Model) detailContent(v revier.ProjectView) string {
	w := m.paneCols() - paneChrome
	rows := m.agentRows()
	if m.paneCols() < widePaneWidth {
		facts := m.facts(v, w)
		if m.acursor >= len(rows) {
			return facts
		}
		return facts + m.agentSaid(v, rows[m.acursor].agent, w, min(w, maxPaneWidth), m.detail.Height-strings.Count(facts, "\n"))
	}
	facts := m.facts(v, maxFactsWidth)
	if m.acursor >= len(rows) {
		return facts
	}
	// The message starts level with the name: its heading's blank line is a
	// separator from a section above it, and there is none here.
	right := w - gridGap - maxFactsWidth
	said := m.agentSaid(v, rows[m.acursor].agent, right, min(right, maxPaneWidth), m.detail.Height+1)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(maxFactsWidth).Render(facts),
		strings.Repeat(" ", gridGap),
		strings.TrimPrefix(said, "\n"))
}

// facts is the pane's first part: what this project is, then what is up,
// then what the agents are doing.
//
// A path and an activity line wrap, as the preview does (os-fzf.sh:788,
// --preview-window=...,wrap): they are the fields worth reading whole, and a
// cut takes exactly the end that says which checkout or which step.
func (m *Model) facts(v revier.ProjectView, w int) string {
	th := m.theme
	var b strings.Builder

	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style))
		b.WriteString("\n")
	}

	// The name stands level with the list's query, and the rule under it
	// level with the list's rule, so the two columns read as one head.
	b.WriteString(m.nameButton(v.Project.Name, w))
	b.WriteString("\n")
	b.WriteString(th.Border.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	// Before the first survey the view is the files' alone, and says nothing
	// about what runs: the pane says so, as the rows do by carrying no mark.
	status, style := "stopped", th.NameDim
	if !m.ready() {
		status, style = "surveying", th.Meta
	}
	switch {
	case v.Invalid != "":
		status, style = "invalid", th.PathMissing
	case v.Unreachable != "":
		status, style = "unreachable", th.PathMissing
	case m.heldHere(v):
		status, style = "running", th.Running
	case !v.PathExists:
		status, style = "not available", th.PathMissing
	}
	line("Status", status, style)
	if v.Project.Remote != nil {
		line("Host", v.Project.Remote.Host, th.Path)
	}

	// A host that did not answer has said nothing about the checkout.
	pathStyle := th.Path
	if !v.PathExists && v.Unreachable == "" {
		pathStyle = th.PathMissing
	}
	line("Path", contractHome(v.Project.Path), pathStyle)
	if v.Project.GitURL != "" {
		line("Git URL", v.Project.GitURL, th.Path)
	}
	// A host that did not answer is said here, in full: the row says nothing
	// about it, and this is where the reason is read.
	if v.Unreachable != "" {
		b.WriteString(hang("", v.Unreachable, w, th.PathMissing))
		b.WriteString("\n")
	}
	// A file that did not load is read here for the same reason. The pane is
	// the only place with room for it, and the project is listed precisely so
	// that there is somewhere to read it.
	if v.Invalid != "" {
		b.WriteString(hang("", v.Invalid, w, th.PathMissing))
		b.WriteString("\n")
	}
	// Only worth saying when it is the reason nothing can start. A running
	// project whose directory has since gone is a different problem, and the
	// red path already says it. A remote project's host clones it on open
	// (decisions.md D40), so Enter is the same answer there.
	// A file that did not load has no path to be missing and nothing to
	// clone into: what is wrong with it is the reason printed above.
	if !v.PathExists && !m.heldHere(v) && v.Unreachable == "" && v.Invalid == "" {
		where, clone := "this machine", "Enter: clone and open"
		if v.Project.Remote != nil {
			where, clone = v.Project.Remote.Host, "Enter: clone there and open"
		}
		b.WriteString(th.PathMissing.Render(clipTo("Directory is not on "+where, w)))
		b.WriteString("\n")
		// What Enter does about it, as the picker's preview says
		// (os-fzf.sh:326, :338).
		if v.Project.GitURL != "" {
			b.WriteString(th.Meta.Render(clipTo(clone, w)))
		} else {
			b.WriteString(th.PathMissing.Render(clipTo("No git_url recorded to clone it from", w)))
		}
		b.WriteString("\n")
	}

	// The rows the pane's cursor stands on: the targets, and the agents under
	// their query. Where each lands is recorded, so the cursor can be kept on
	// screen and a click can find its row.
	lineNow := func() int { return strings.Count(b.String(), "\n") }
	m.ainput.Width = w - lipgloss.Width(promptMark) - 1
	b.WriteString(m.heading("Targets", w))
	m.tlines = m.tlines[:0]
	for i, row := range m.targetRows() {
		m.tlines = append(m.tlines, lineNow())
		b.WriteString(m.detailRow(row, w, m.focus == focusTargets && i == m.tcursor, m.over.is(hoverTarget, i)))
		b.WriteString("\n")
	}

	// Every agent, and what it is doing: the row only counts them by state.
	m.afield, m.alines = -1, m.alines[:0]
	if len(v.Agents) > 0 {
		b.WriteString(m.heading("Agents", w))
		m.afield = lineNow()
		b.WriteString(m.fieldView(m.ainput, focusAgents) + "\n")
		for i, row := range m.agentRows() {
			start := lineNow()
			b.WriteString(m.detailAgent(row, w, i == m.acursor, m.over.is(hoverAgent, i)))
			b.WriteString("\n")
			m.alines = append(m.alines, lineSpan{start, lineNow()})
		}
	}
	return b.String()
}

// heading opens a section of the pane: a blank line, then the title with a
// rule to the pane's edge, so the sections read as blocks rather than as a
// list of lines that happens to change colour.
func (m Model) heading(title string, w int) string {
	th := m.theme
	rule := w - lipgloss.Width(title) - 1
	if rule < 0 {
		rule = 0
	}
	return "\n" + th.Heading.Render(title) + " " + th.Border.Render(strings.Repeat("─", rule)) + "\n"
}

// detailRow is one row of the Targets section: a target - its name, whether
// it is up, and its key in the spelling the footer uses - or an attached
// instance, which has a title and no key. A stopped target says "stopped",
// where it said "-", which read as a value that failed to load. The row under
// the pane's cursor carries the list's bar and selection background across its
// width, so the two cursors read as one; the row under the pointer carries the
// hover background, because a click reaches it.
func (m Model) detailRow(row targetRow, w int, sel, over bool) string {
	th := m.theme
	style := func(s lipgloss.Style) lipgloss.Style {
		switch {
		case sel:
			return th.OnSelection(s)
		case over:
			return th.OnHover(s)
		}
		return s
	}
	bar := style(th.Path).Render(" ")
	if sel {
		bar = th.Cursor.Render(th.Glyphs.Cursor)
	}
	lead := bar + style(th.Path).Render(strings.Repeat(" ", detailLeadWidth-1))
	space := style(lipgloss.NewStyle())
	if ref := row.attached; !ref.IsZero() {
		head := gridHead(lead, style(th.NameDim).Render("attached"), style(th.Running).Render(th.Glyphs.Running+" running"), space)
		return fill(clipTo(head+style(th.ProjectName).Render(ellipsis(ref.Title, gridRest(w))), w), w, style)
	}
	t := row.target
	mark, state, stateStyle, name := th.Glyphs.Stopped, "stopped", th.Count, th.NameDim
	switch {
	// A target its own config refused is a mistake to go and fix; one no host
	// here can realize is the expected result on a machine without that host.
	// The two must not read alike.
	case !t.Available && t.Reason != "":
		state, stateStyle = "invalid", th.PathMissing
	case !t.Available:
		state = "no host here"
	case t.Unknown != "":
		// The host could not list this survey: up or not is not known, and
		// the footer says why.
		mark, state, stateStyle = strings.Repeat(" ", lipgloss.Width(mark)), "unknown", th.Meta
	case !m.ready():
		// The files' view, before any host has answered: whether the target
		// is up is not known yet, and the row says so rather than "stopped".
		mark, state, stateStyle = strings.Repeat(" ", lipgloss.Width(mark)), "surveying", th.Meta
	case !t.Ref.IsZero():
		mark, state, stateStyle, name = th.Glyphs.Running, "running", th.Running, th.ProjectName
	}
	head := gridHead(lead, style(name).Render(string(t.Name)), style(stateStyle).Render(mark+" "+state), space)
	return fill(clipTo(head+style(th.Help).Render(ellipsis(keyLabel(t.Key), gridRest(w))), w), w, style)
}

// detailAgent is one row of the Agents section, on the Targets section's grid:
// the harness under the target names, the state glyph and words under theirs,
// and the activity where their keys are, wrapped under itself. A selected or
// pointed-at row is lit as a target row is, on every line it takes.
func (m Model) detailAgent(row agentRow, w int, sel, over bool) string {
	th := m.spun()
	style := func(s lipgloss.Style) lipgloss.Style {
		switch {
		case sel:
			return th.OnSelection(s)
		case over:
			return th.OnHover(s)
		}
		return s
	}
	// The agent shown below is marked while the cursor is in the list, so the
	// message has a row it belongs to; the cursor's bar is the Agents' own.
	bar := style(th.Path).Render(" ")
	if sel && m.focus == focusAgents {
		bar = th.Cursor.Render(th.Glyphs.Cursor)
	}
	lead := bar + style(th.Path).Render(strings.Repeat(" ", detailLeadWidth-1))
	a := row.agent
	harness := harnessOf(a)
	offset := len(harness) + 1 // the label is the harness, a space, the activity
	head := clipTo(gridHead(lead,
		highlight(harness, row.matches, style(th.ProjectName), style(th.Match)),
		style(statusStyle(th, a.State.Status)).Render(statusLabel(th, a.State.Status)), style(lipgloss.NewStyle())), w)
	parts := wrap(a.State.Activity, gridRest(w))
	if len(parts) == 0 {
		return fill(head, w, style)
	}
	indent := style(lipgloss.NewStyle()).Render(strings.Repeat(" ", lipgloss.Width(head)))
	lines := make([]string, len(parts))
	for i, at := range partOffsets(a.State.Activity, parts) {
		matches := within(row.matches, offset+at, len(parts[i]))
		text := highlight(parts[i], matches, style(th.Path), style(th.Match))
		start := indent
		if i == 0 {
			start = head
		}
		lines[i] = fill(start+text, w, style)
	}
	return strings.Join(lines, "\n")
}

// partOffsets is where each of the parts wrap cut text into starts in it, in
// bytes, as the filter counts its matches. wrap drops the spaces it breaks
// at, so each part is looked for from where the one before it ended.
func partOffsets(text string, parts []string) []int {
	out := make([]int, len(parts))
	from := 0
	for i, part := range parts {
		if at := strings.Index(text[from:], part); at >= 0 {
			from += at
		}
		out[i] = from
		from = min(from+len(part), len(text))
	}
	return out
}

// within is the matches that fall in n bytes from start, counted from start.
func within(matches []int, start, n int) []int {
	var out []int
	for _, i := range matches {
		if i >= start && i < start+n {
			out = append(out, i-start)
		}
	}
	return out
}

// gridHead is the start of a row of the pane's grid, the part both sections
// share: the lead, then the name and the state, each cut to leave a gap before
// the next column, so no name or state can push the columns after it. space
// styles the padding, so a selected row's background runs unbroken.
func gridHead(lead, name, state string, space lipgloss.Style) string {
	return lead + gridCell(name, detailNameWidth, space) + gridCell(state, detailStateWidth, space)
}

func gridCell(s string, width int, space lipgloss.Style) string {
	s = clipTo(s, width-1)
	return s + space.Render(strings.Repeat(" ", width-lipgloss.Width(s)))
}

// gridRest is the width left after gridHead: a target's key, or the first line
// of an agent's activity.
func gridRest(w int) int {
	return w - detailLeadWidth - detailNameWidth - detailStateWidth
}

// The pane's columns. Narrower than the list's, because the pane is. The lead
// is the cursor bar and a space. The state column fits the widest state, a
// glyph and "no host here", and a gap, so the key or activity after it has the
// room.
const (
	detailLabelWidth = 9
	detailLeadWidth  = 2
	detailNameWidth  = 10
	detailStateWidth = 15
)
