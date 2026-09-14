package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The list's width bounds, the least the pane needs beside it, and the pane
// width at which the pane lays its sections side by side (decisions.md D39).
//
// The list is a table of two columns - the project, and its agent's state -
// and stops at the width that holds both whole. Past that every column goes
// to the pane, which is where a wide terminal has something to show. Below
// the least the two need together, there is no pane.
const (
	minListWidth  = 56
	maxListWidth  = 110
	minPaneWidth  = 44
	widePaneWidth = 130
	maxFactsWidth = 80
)

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
// on it, where it stands in the list's place (decisions.md D73). Zero is a
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
// is kept on a row that exists, and on the screen.
func (m *Model) syncDetail() {
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
	case dialogHosts, dialogNew, dialogConfig:
		m.detail.SetContent("")
		return
	case dialogRemote, dialogLinkName:
		m.detail.SetContent(m.remoteDetail())
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
	m.tcursor = min(max(m.tcursor, 0), max(len(m.targetRows())-1, 0))
	m.acursor = min(max(m.acursor, 0), max(len(m.agentRows())-1, 0))
	m.detail.SetContent(m.detailContent(v))
	if fresh {
		m.detail.GotoTop()
	}
	switch {
	case m.focus == focusTargets && m.tcursor < len(m.tlines):
		m.followPane(m.tlines[m.tcursor])
	case m.focus == focusAgents && m.acursor < len(m.alines):
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

// detailContent is what the shell picker's preview shows, in its order
// (os-fzf.sh:290): what this project is, then what is up, then what the
// agents are doing, then what the directory holds.
//
// A narrow pane stacks the sections, and the snapshot takes the rows the
// others leave. A wide pane puts the snapshot beside the rest, so both fill
// the height and neither waits under the other (decisions.md D39).
func (m *Model) detailContent(v revier.ProjectView) string {
	w := m.paneCols() - paneChrome
	h := m.detail.Height
	if m.paneCols() < widePaneWidth {
		facts := m.facts(v, w)
		return facts + m.snapshot(v, w, h-strings.Count(facts, "\n"))
	}
	// The facts take half, up to what a repository URL and an agent's line
	// need whole; the snapshot takes the rest.
	left := min((w-gridGap)/2, maxFactsWidth)
	right := w - gridGap - left
	// The snapshot starts level with the name: its heading's blank line is a
	// separator from a section above it, and there is none here.
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(left).Render(m.facts(v, left)),
		strings.Repeat(" ", gridGap),
		strings.TrimPrefix(m.snapshot(v, right, h+1), "\n"))
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

	b.WriteString(th.Header.Render(clipTo(string(v.Project.Name), w)))
	b.WriteString("\n")

	status, style := "stopped", th.NameDim
	switch {
	case v.Unreachable != "":
		status, style = "unreachable", th.PathMissing
	case v.Running:
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
	// A host that did not answer is said in full: the row has room for the
	// fact, and this is where the reason is read.
	if v.Unreachable != "" {
		b.WriteString(hang("", v.Unreachable, w, th.PathMissing))
		b.WriteString("\n")
	}
	// Only worth saying when it is the reason nothing can start. A running
	// project whose directory has since gone is a different problem, and the
	// red path already says it. A remote project's host clones it on open
	// (decisions.md D40), so Enter is the same answer there.
	if !v.PathExists && !v.Running && v.Unreachable == "" {
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

	// The rows Tab moves the cursor onto, each section under its own query.
	// Where each lands is recorded, so the cursor can be kept on screen and a
	// click can find its row.
	lineNow := func() int { return strings.Count(b.String(), "\n") }
	m.tinput.Width = w - lipgloss.Width(promptMark) - 1
	m.ainput.Width = m.tinput.Width
	b.WriteString(m.heading("Targets", w))
	m.tfield = lineNow()
	b.WriteString(m.fieldView(m.tinput, focusTargets) + "\n")
	m.tlines = m.tlines[:0]
	for i, row := range m.targetRows() {
		m.tlines = append(m.tlines, lineNow())
		b.WriteString(m.detailRow(row, w, m.focus == focusTargets && i == m.tcursor, m.over.is(hoverTarget, i)))
		b.WriteString("\n")
	}

	// Every agent, not the worst one the row collapses to: a project with two
	// agents is exactly where the row is not enough.
	m.afield, m.alines = -1, m.alines[:0]
	if len(v.Agents) > 0 {
		b.WriteString(m.heading("Agents", w))
		m.afield = lineNow()
		b.WriteString(m.fieldView(m.ainput, focusAgents) + "\n")
		for i, row := range m.agentRows() {
			start := lineNow()
			b.WriteString(m.detailAgent(row, w, m.focus == focusAgents && i == m.acursor, m.over.is(hoverAgent, i)))
			b.WriteString("\n")
			m.alines = append(m.alines, lineSpan{start, lineNow()})
		}
	}
	return b.String()
}

// snapshot is what the directory holds, in the rows it is given: with ninety
// near-identical names this is what says which checkout the cursor is on.
// Tree rows are cut, not wrapped, because a wrapped tree row loses its
// indentation. rows counts the heading; a listing that does not fit ends in
// an ellipsis on its last row.
func (m *Model) snapshot(v revier.ProjectView, w, rows int) string {
	if !v.PathExists || v.Project.Remote != nil {
		return ""
	}
	tree := m.treeFor(v.Project.Path)
	room := rows - 2 // the heading and the blank line before it
	if len(tree) == 0 || room < 1 {
		return ""
	}
	if len(tree) > room {
		tree = append(tree[:room-1:room-1], "...")
	}
	var b strings.Builder
	b.WriteString(m.heading("Project Snapshot", w))
	for _, line := range tree {
		b.WriteString(m.theme.Path.Render(clipTo(line, w)))
		b.WriteString("\n")
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
// hover background, because one click runs it.
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
		title := highlight(ref.Title, row.matches, style(th.ProjectName), style(th.Match))
		return fill(clipTo(head+ellipsis(title, gridRest(w)), w), w, style)
	}
	t := row.target
	mark, state, stateStyle, name := th.Glyphs.Stopped, "stopped", th.Count, th.NameDim
	switch {
	case !t.Available:
		state = "no host here"
	case !t.Ref.IsZero():
		mark, state, stateStyle, name = th.Glyphs.Running, "running", th.Running, th.ProjectName
	}
	head := gridHead(lead,
		highlight(string(t.Name), row.matches, style(name), style(th.Match)),
		style(stateStyle).Render(mark+" "+state), space)
	return fill(clipTo(head+style(th.Accent).Render(ellipsis(keyLabel(t.Key), gridRest(w))), w), w, style)
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
	bar := style(th.Path).Render(" ")
	if sel {
		bar = th.Cursor.Render(th.Glyphs.Cursor)
	}
	lead := bar + style(th.Path).Render(strings.Repeat(" ", detailLeadWidth-1))
	a := row.agent
	harness := harnessOf(a)
	offset := len([]rune(harness)) + 1 // the label is the harness, a space, the activity
	head := clipTo(gridHead(lead,
		highlight(harness, row.matches, style(th.ProjectName), style(th.Match)),
		style(statusStyle(th, a.State.Status)).Render(statusLabel(th, a.State.Status)), style(lipgloss.NewStyle())), w)
	parts := wrap(a.State.Activity, gridRest(w))
	if len(parts) == 0 {
		return fill(head, w, style)
	}
	indent := style(lipgloss.NewStyle()).Render(strings.Repeat(" ", lipgloss.Width(head)))
	lines := make([]string, len(parts))
	for i, at := range runeOffsets(a.State.Activity, parts) {
		matches := within(row.matches, offset+at, len([]rune(parts[i])))
		text := highlight(parts[i], matches, style(th.Path), style(th.Match))
		start := indent
		if i == 0 {
			start = head
		}
		lines[i] = fill(start+text, w, style)
	}
	return strings.Join(lines, "\n")
}

// runeOffsets is where each of the parts wrap cut text into starts in it, in
// runes. wrap drops the spaces it breaks at, so each part is looked for from
// where the one before it ended.
func runeOffsets(text string, parts []string) []int {
	runes := []rune(text)
	out := make([]int, len(parts))
	from := 0
	for i, part := range parts {
		p := []rune(part)
		for at := from; at+len(p) <= len(runes); at++ {
			if string(runes[at:at+len(p)]) == part {
				from = at
				break
			}
		}
		out[i] = from
		from += len(p)
	}
	return out
}

// within is the matches that fall in n runes from start, counted from start.
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
