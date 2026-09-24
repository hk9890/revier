package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// projectItem is one project in the list. The view is carried whole, so the
// delegate renders from the survey and never looks anything up. Before the
// first survey answers the view is the files' alone, and the row says so by
// carrying no mark: a stopped mark on ninety rows for half a second would be
// a claim, and a wrong one for every project that is open.
type projectItem struct {
	view       revier.ProjectView
	unsurveyed bool
}

// FilterValue is what the fuzzy filter matches. Only the name: a path or an
// activity string would make a keystroke select something the user cannot see
// a reason for.
func (i projectItem) FilterValue() string { return string(i.view.Project.Name) }

func (i projectItem) rowView() revier.ProjectView { return i.view }
func (i projectItem) rowPath() string             { return contractHome(i.view.Project.Path) }
func (i projectItem) rowNote() string             { return "" }
func (i projectItem) rowUnsurveyed() bool         { return i.unsurveyed }

// tableRow is an item the project table draws: the projects here, and the
// projects of a host in the link dialog. The two differ in whose home a path
// is written against, and in the note a link dialog row has under the counts.
type tableRow interface {
	rowView() revier.ProjectView
	rowPath() string
	rowNote() string
	rowUnsurveyed() bool
}

// newProjectList is the picker. Filtering is on but its own filter bar is
// hidden, because the surface renders the filter in the header and feeds the
// text in itself (setFilter) - the list never sees a rune key.
func newProjectList(th theme.Theme) list.Model {
	l := list.New(nil, projectDelegate{theme: th, hover: -1}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	return l
}

// projectDelegate renders one row: a state mark, the name, and the
// project's agents counted by state.
//
// hover is the row the pointer is on, or -1. It is set on the delegate
// rather than read from the model because the list component renders through
// the delegate and hands it nothing but the row.
type projectDelegate struct {
	theme theme.Theme
	hover int
}

// Height is two: the name line and the path under it. The path is what tells
// two checkouts of the same name apart (os_list_json.py:476 renders the same
// two lines).
func (d projectDelegate) Height() int                         { return 2 }
func (d projectDelegate) Spacing() int                        { return 0 }
func (d projectDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d projectDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(tableRow)
	if !ok {
		return
	}
	th := d.theme
	sel := index == m.Index()
	over := index == d.hover && !sel

	style := func(s lipgloss.Style) lipgloss.Style {
		switch {
		case sel:
			return th.OnSelection(s)
		case over:
			return th.OnHover(s)
		}
		return s
	}

	v, path, rowNote := it.rowView(), it.rowPath(), it.rowNote()
	width := m.Width()
	// The mark says whether the project is open, and nothing else: what its
	// agents are doing is the right-hand side's, as counts by state.
	mark, markStyle, name := th.Glyphs.Stopped, th.NameDim, th.NameDim
	switch {
	case it.rowUnsurveyed():
		mark = strings.Repeat(" ", lipgloss.Width(mark))
	case v.Held():
		mark, markStyle, name = th.Glyphs.Running, th.Running, th.ProjectName
	}
	// A row with a note of its own is one Enter has nothing to do on.
	if rowNote != "" {
		name = th.NameDim
	}
	if sel {
		name = name.Bold(true)
	}
	// The bar runs down both lines, so the selection is one block and not a
	// name with a path that happens to sit under it.
	bar := style(th.Path).Render(" ")
	if sel {
		bar = th.Cursor.Render(th.Glyphs.Cursor)
	}
	gap := style(th.Path).Render(" ")
	prefix := bar + gap + style(markStyle).Render(mark) + gap
	if th.Glyphs.Folder != "" {
		// The column says where the checkout is: here, not here, or on a
		// host.
		folder, folderStyle := th.Glyphs.Folder, th.Meta
		switch {
		case v.Project.Remote != nil:
			folder, folderStyle = th.Glyphs.Remote, th.Remote
		case !v.PathExists:
			folder, folderStyle = th.Glyphs.NoFolder, th.PathMissing
		}
		prefix += style(folderStyle).Render(folder) + gap
	}
	indent := lipgloss.Width(prefix)

	// The row is a table of two columns (decisions.md D39): the project - its
	// name, and its path under it - and the agents, counted by state on the
	// first line. The agent column says agent state and nothing else: what
	// is open, and a checkout that is missing, are the pane's to say. The
	// agent column has a fixed width and sits at the right edge, with its
	// text left-aligned inside it, so the counts line up in one column
	// whatever the names and paths beside them do. Nothing from the project
	// column crosses into it: a path is cut in the middle to fit.
	//
	// The project column is what the list is for, so it gives way last: the
	// agent column shrinks first, to one glyph, and then goes, and only then
	// is a name or a path cut. Where the agent column starts is agentGeometry's
	// to say, so the rule's totals stand in the same columns as these counts.
	agentAt, agentCol := d.agentGeometry(m)
	projectCol := width - indent
	if agentCol > 0 {
		projectCol = agentAt - gridGap - indent
	}
	cell := func(s string, w int) string {
		return s + style(th.Path).Render(strings.Repeat(" ", max(w-lipgloss.Width(s), 0)))
	}

	nameText := clipTo(v.Project.Label(), min(projectCol, maxNameWidth))
	first := prefix + cell(highlight(nameText, m.MatchesForItem(index), style(name), style(th.Match)), projectCol+gridGap) +
		d.agent(v, agentCol, style)
	if agentCol == 0 {
		first = prefix + highlight(nameText, m.MatchesForItem(index), style(name), style(th.Match))
	}

	// A missing path stays grey: a third of the rows in maroon from end to
	// end read as a list of errors.
	note := ""
	if rowNote != "" {
		note = style(th.Meta).Render(ellipsis(rowNote, agentCol))
	}
	second := bar + style(th.Path).Render(strings.Repeat(" ", indent-1)) +
		cell(style(th.Path).Render(elide(path, projectCol)), projectCol+gridGap) + note
	if agentCol == 0 {
		second = bar + style(th.Path).Render(strings.Repeat(" ", indent-1)) +
			style(th.Path).Render(elide(path, projectCol))
	}

	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(first, width, style)+"\n"+fill(second, width, style))
}

// The table's measures: the gap between its columns, the digits an agent
// count is padded to, the most the agent column takes, the most the project
// column asks for, and the most of it a name takes. The agent column has what
// the counts of all four states need; a link dialog row's "linked as" note is
// cut to it. The project column asks for its widest name or path up to a width
// that holds most paths, and a name longer than its cap is cut so its row alone
// pays for it.
const (
	gridGap         = 2
	countWidth      = 2
	maxAgentWidth   = 24
	maxProjectWidth = 48
	maxNameWidth    = 32
)

// agentGeometry is where a row's agent column starts and how wide it is. It
// is read off the list rather than off one row, so Render and the rule that
// draws its totals over the same column both ask it: the column sits at the
// right edge, so its width decides its start. A start of zero is a list with
// no room for the column at all.
func (d projectDelegate) agentGeometry(m list.Model) (start, room int) {
	indent := rowIndent(d.theme)
	room = min(maxAgentWidth, m.Width()-indent-gridGap-d.projectColumn(m))
	if room < 1 {
		return 0, 0
	}
	return m.Width() - room, room
}

// rowIndent is the width of a row's prefix: the selection bar, the state mark
// and the folder glyph, each followed by a gap. Every glyph that shares a
// column is one width, so the prefix is as wide for one row as for the next.
func rowIndent(th theme.Theme) int {
	indent := 1 + 1 + lipgloss.Width(th.Glyphs.Stopped) + 1
	if th.Glyphs.Folder != "" {
		indent += lipgloss.Width(th.Glyphs.Folder) + 1
	}
	return indent
}

// projectColumn is the width the project column asks for: the widest name
// or path on the list, within its cap. The agent column gets what is left,
// so a list of short paths gives its agents the room and a list of long ones
// keeps the paths whole.
func (d projectDelegate) projectColumn(m list.Model) int {
	col := 0
	for _, item := range m.VisibleItems() {
		if it, ok := item.(tableRow); ok {
			col = max(col,
				min(lipgloss.Width(it.rowView().Project.Label()), maxNameWidth),
				lipgloss.Width(it.rowPath()))
		}
	}
	return min(col, maxProjectWidth)
}

// highlight renders the letters the filter matched in their own style, as fzf
// does: with a fuzzy filter the letters are the only way to see why a row is
// on the list. matches are byte positions: the fuzzy filter under the list
// component counts in bytes, so a rune count lights the wrong letters after
// the first one wider than a byte.
func highlight(text string, matches []int, plain, match lipgloss.Style) string {
	if len(matches) == 0 {
		return plain.Render(text)
	}
	hit := make(map[int]bool, len(matches))
	for _, i := range matches {
		hit[i] = true
	}
	var b strings.Builder
	for i, r := range text {
		if hit[i] {
			b.WriteString(match.Render(string(r)))
		} else {
			b.WriteString(plain.Render(string(r)))
		}
	}
	return b.String()
}

// agent is the project's agents counted by state: the table's second column,
// and the part that answers "which of these needs me". What each agent is
// doing is the pane's. How the counts are laid out is agentPieces'.
func (d projectDelegate) agent(v revier.ProjectView, room int, style func(lipgloss.Style) lipgloss.Style) string {
	counts := map[revier.Status]int{}
	for _, a := range v.Agents {
		counts[a.State.Status]++
	}
	return agentColumn(d.theme, counts, room, style)
}

// agentPiece is one state's count and the column it sits in, measured from
// the agent column's own left edge.
type agentPiece struct {
	at    int
	text  string
	style lipgloss.Style
}

// agentPieces lays the agent column out: each state with agents as its glyph
// and count, in the slot its state owns, so a state reads in the same column
// on every row and in the rule above them. A state with no agent leaves its
// slot empty rather than moving the ones after it.
//
// The column gives way in steps as room goes (decisions.md D39): the slots,
// then the worst state's count alone, then its glyph alone, then nothing.
// Whoever draws the pieces fills what is between them - a row with blanks,
// the rule with its line.
func agentPieces(th theme.Theme, counts map[revier.Status]int, room int) []agentPiece {
	glyphWidth := 0
	for _, s := range statusOrder {
		glyphWidth = max(glyphWidth, lipgloss.Width(statusGlyph(th, s)))
	}
	slot := glyphWidth + 1 + countWidth
	var full []agentPiece
	worst := -1
	for i, s := range statusOrder {
		if counts[s] > 0 {
			if worst < 0 {
				worst = i
			}
			full = append(full, agentPiece{
				at:    i * (slot + gridGap),
				text:  fmt.Sprintf("%s %d", statusGlyph(th, s), counts[s]),
				style: statusStyle(th, s),
			})
		}
	}
	if worst < 0 {
		return nil
	}
	if last := full[len(full)-1]; last.at+lipgloss.Width(last.text) <= room {
		return full
	}
	s := statusOrder[worst]
	for _, text := range []string{
		fmt.Sprintf("%s %d", statusGlyph(th, s), counts[s]),
		statusGlyph(th, s),
	} {
		if lipgloss.Width(text) <= room {
			return []agentPiece{{text: text, style: statusStyle(th, s)}}
		}
	}
	return nil
}

// agentColumn draws the pieces as a row draws them: the space between two
// counts is the row's background, as every other gap in a row is. A count
// with more digits than its slot holds takes the space before the next one
// rather than pushing it out of its column.
func agentColumn(th theme.Theme, counts map[revier.Status]int, room int, style func(lipgloss.Style) lipgloss.Style) string {
	out, at := "", 0
	for _, p := range agentPieces(th, counts, room) {
		out += style(th.Path).Render(strings.Repeat(" ", max(p.at-at, 0))) + style(p.style).Render(p.text)
		at = p.at + lipgloss.Width(p.text)
	}
	return out
}

// statusOrder is every agent state, the one closest to needing you first. A
// row's counts and the rule's totals read in the same order, so a glyph means
// the same thing wherever it is.
var statusOrder = []revier.Status{revier.StatusAttention, revier.StatusRunning, revier.StatusIdle, revier.StatusUnknown}

// statusGlyph is the state's glyph alone, for a row's counts, which have no
// room for its words.
func statusGlyph(th theme.Theme, s revier.Status) string {
	glyph, _, _ := strings.Cut(statusLabel(th, s), " ")
	return glyph
}

// statusLabel is an agent state as the surface says it: its glyph and a word
// for the person reading, not the name the JSON carries. "attention" was a
// field value; "needs you" is what it means, and what the header says.
// "working" and not "running", because running is what a project is when it
// is open, and the two sat on one row.
func statusLabel(th theme.Theme, s revier.Status) string {
	switch s {
	case revier.StatusAttention:
		return th.Glyphs.NeedsYou + " needs you"
	case revier.StatusRunning:
		return th.Glyphs.Working + " working"
	case revier.StatusIdle:
		return th.Glyphs.Idle + " idle"
	default:
		return th.Glyphs.Unknown + " unknown"
	}
}

// statusStyle is the colour of an agent state, the same on the row and in the
// pane.
func statusStyle(th theme.Theme, s revier.Status) lipgloss.Style {
	switch s {
	case revier.StatusAttention:
		return th.Attention
	case revier.StatusRunning:
		return th.Running
	case revier.StatusIdle:
		return th.Idle
	default:
		return th.NameDim
	}
}

// reload puts the current survey into the project list, keeping the filter and
// the selected project across the refresh. Selection is restored by name:
// attention sorting moves rows, so holding the index would move the cursor to
// a different project while the user was reading it.
func (m *Model) reload() {
	was, hadSelection := m.plist.SelectedItem().(projectItem)
	at := m.plist.Index()

	items := make([]list.Item, 0, len(m.views))
	for _, v := range m.views {
		items = append(items, projectItem{view: v, unsurveyed: !m.surveyed})
	}
	// The command SetItems returns re-runs the filter asynchronously. The
	// filter is re-applied synchronously below instead, so the list is correct
	// before this function returns - which is what makes it testable.
	_ = m.plist.SetItems(items)
	if m.filter != "" {
		m.plist.SetFilterText(m.filter)
	}

	// The rows before the first survey are the files' alone, in file order,
	// and the cursor's place on them is nobody's choice unless the user
	// moved it. SetItems keeps the cursor's index, not its project, so the
	// first survey places the cursor itself: on the project the user moved
	// to, else on the project of the working directory, the way the shell
	// picker preselects it (os_list_json.py:570), else on the top row, which
	// is the project that needs the user most. After that the user's own
	// selection wins.
	moved := hadSelection && was.unsurveyed && m.plist.Index() != 0 && was.view.Project.Name != m.start
	switch {
	case hadSelection && (!was.unsurveyed || moved):
		// A project that left the list - deleted here, or its file gone
		// while the popup was hidden - hands the cursor to the row that
		// took its place, and the last row when it was the last
		// (decisions.md D98).
		if !m.selectName(was.view.Project.Name) {
			m.plist.Select(clampRow(at, len(m.plist.VisibleItems())))
		}
	case m.start != "":
		// Only a cursor that landed on the starting project is placed
		// around it. A filter or an empty list leaves it on the first row,
		// and placing that row would scroll the list under the user later,
		// on the frame that finally has rows.
		m.placing = m.selectName(m.start)
	default:
		m.plist.Select(0)
	}
}

// selectedName is the highlighted project, for restoring it after a reload.
func (m Model) selectedName() (revier.ProjectName, bool) {
	it, ok := m.plist.SelectedItem().(projectItem)
	if !ok {
		return "", false
	}
	return it.view.Project.Name, true
}

// selectName puts the cursor on the project, and reports whether the list
// held it: a name it no longer shows falls back to the first row.
func (m *Model) selectName(name revier.ProjectName) bool {
	for i, item := range m.plist.VisibleItems() {
		if it, ok := item.(projectItem); ok && it.view.Project.Name == name {
			m.plist.Select(i)
			return true
		}
	}
	m.plist.Select(0)
	return false
}

// endSearch drops every query once a press has opened what it found, so the
// surface opens again with empty fields. Unlike Esc it is not an undo: each
// cursor stays on the row the search led to.
func (m *Model) endSearch() {
	m.before, _ = m.selectedName()
	m.setFilter("")
	m.endPaneSearch()
}

// setFilter is every change to the filter text. The list filters
// synchronously through SetFilterText, so the count in the header and the
// selection are right on the same pass as the keystroke.
//
// A query is a view over the list, and clearing it, by Esc or by deleting
// the last letter, is its undo: the cursor goes back to the project it was
// on when the query began (decisions.md D44).
func (m *Model) setFilter(f string) {
	if m.filter == "" && f != "" {
		m.before, _ = m.selectedName()
	}
	m.filter = f
	if m.input.Value() != f {
		m.input.SetValue(f)
	}
	if f != "" {
		m.plist.SetFilterText(f)
		return
	}
	m.plist.ResetFilter()
	m.selectName(m.before)
}
