package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// projectItem is one project in the list. The view is carried whole, so the
// delegate renders from the survey and never looks anything up.
type projectItem struct {
	view revier.ProjectView
}

// FilterValue is what the fuzzy filter matches. Only the name: a path or an
// activity string would make a keystroke select something the user cannot see
// a reason for.
func (i projectItem) FilterValue() string { return string(i.view.Project.Name) }

func (i projectItem) rowView() revier.ProjectView { return i.view }
func (i projectItem) rowPath() string             { return contractHome(i.view.Project.Path) }
func (i projectItem) rowNote() string             { return "" }

func (i projectItem) rowMachine() string {
	if i.view.Project.Remote != nil {
		return i.view.Project.Remote.Host
	}
	return ""
}

// tableRow is an item the project table draws: the projects here, and the
// projects of a host in the link dialog. The two differ in whose home a path
// is written against, in what the row says in place of what is open, and in
// which machine a missing checkout is missing from.
type tableRow interface {
	rowView() revier.ProjectView
	rowPath() string
	rowNote() string
	// rowMachine is the host the checkout belongs on, or "" for this machine.
	rowMachine() string
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

// projectDelegate renders one row: a state mark, the name, and the worst
// agent state in the project with what it is doing.
//
// hover is the row the pointer is on, or -1. It is set on the delegate
// rather than read from the model because the list component renders through
// the delegate and hands it nothing but the row.
type projectDelegate struct {
	theme theme.Theme
	hover int
}

// Height is two: the name line and the path under it. The path is what tells
// two checkouts of the same name apart, and what shows that a project's
// directory is not on this machine (os_list_json.py:476 renders the same two
// lines).
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
	// agent is doing is the right-hand side's, in an icon and a word.
	mark, markStyle, name := th.Glyphs.Stopped, th.NameDim, th.NameDim
	if v.Running {
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
		// host. A host without the checkout is said in words under the state.
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

	// The row is a table of two columns on both its lines (decisions.md
	// D39): the project - its name, and its path under it - and the agent -
	// its state and activity, and under them what is open or why nothing can
	// be. The agent column has a fixed width and sits at the right edge, with
	// its text left-aligned inside it, so the states line up in one column
	// whatever the names and paths beside them do. Nothing from the project
	// column crosses into it: a path is cut in the middle to fit.
	//
	// The project column is what the list is for, so it gives way last: the
	// agent column shrinks first, to its glyph, and then goes, and only then
	// is a name or a path cut.
	projectCol := d.projectColumn(m)
	agentCol := min(maxAgentWidth, width-indent-gridGap-projectCol)
	if agentCol < 1 {
		agentCol, projectCol = 0, width-indent
	} else {
		projectCol = width - indent - gridGap - agentCol
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

	// A missing path stays grey: the note under the state says it, and a
	// third of the rows in maroon from end to end read as a list of errors.
	note := d.note(v, it.rowMachine(), agentCol, style)
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

// The table's measures: the gap between its columns, the most the agent
// column takes, the most the project column asks for, and the most of it a
// name takes. The agent column has what a state and thirty-six of an
// activity need; the project column asks for its widest name or path up to
// a width that holds most paths, and a name longer than its cap is cut so
// its row alone pays for it.
const (
	gridGap         = 2
	maxAgentWidth   = 48
	maxProjectWidth = 48
	maxNameWidth    = 32
)

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

// note is the agent column's second line: what is open, or why nothing can
// be. A directory that is not here is said in words (decisions.md D30), and
// what Enter does about it is the pane's to say. A stopped project with its
// checkout in place has nothing to say, and neither does one with only its
// home open: that is what the green mark says. machine is the host the
// checkout belongs on, or "" for this machine.
func (d projectDelegate) note(v revier.ProjectView, machine string, room int, style func(lipgloss.Style) lipgloss.Style) string {
	th := d.theme
	if v.Unreachable != "" {
		// The failure itself is the pane's: here there is room for the fact.
		note := v.Project.Remote.Host + " unreachable"
		if lipgloss.Width(note) > room {
			note = "unreachable"
		}
		if lipgloss.Width(note) > room {
			return ""
		}
		return style(th.PathMissing).Render(note)
	}
	if !v.PathExists {
		note := "not on this machine"
		if machine != "" {
			note = "not on " + machine
		}
		if v.Project.GitURL != "" {
			note = "not cloned"
		}
		if lipgloss.Width(note) > room {
			note = "not here"
		}
		if lipgloss.Width(note) > room {
			return ""
		}
		return style(th.PathMissing).Render(note)
	}
	var open []string
	for _, t := range v.Targets {
		if !t.Ref.IsZero() {
			open = append(open, string(t.Name))
		}
	}
	if len(open) == 1 && !v.Home.IsZero() {
		return ""
	}
	return style(th.NameDim).Render(ellipsis(strings.Join(open, " · "), room))
}

// agent is the worst agent state in the project and what it is doing: the
// table's second column, and the part that answers "which of these needs
// me". A project running several agents is why the detail pane lists them all.
//
// The column gives way in steps as room goes (decisions.md D39): first the
// activity, which the pane shows whole; then the words, leaving the glyph,
// which is why every set's glyphs are told apart on their own; then the
// glyph. The sort and the header's counts still say who needs you.
func (d projectDelegate) agent(v revier.ProjectView, room int, style func(lipgloss.Style) lipgloss.Style) string {
	worst, ok := core.Worst(v.Agents)
	if !ok {
		return ""
	}
	th := d.theme
	state := statusLabel(th, worst.Status)
	if lipgloss.Width(state) > room {
		state = statusGlyph(th, worst.Status)
	}
	if lipgloss.Width(state) > room {
		return ""
	}
	out := style(statusStyle(th, worst.Status)).Render(state)
	rest := min(room-lipgloss.Width(state)-1, maxActivityWidth)
	if state != statusGlyph(th, worst.Status) && worst.Activity != "" && rest >= minActivityWidth {
		out += style(th.NameDim).Render(" " + ellipsis(worst.Activity, rest))
	}
	return out
}

// The activity's bounds on a row. Below the least, a cut says nothing and
// the state stands alone; above the most, the rest is the pane's, and the
// row would only be pushing the pane away.
const (
	minActivityWidth = 12
	maxActivityWidth = 50
)

// statusGlyph is the state's glyph alone, for a column with no room for its
// words.
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
	was, hadSelection := m.selectedName()

	items := make([]list.Item, 0, len(m.views))
	for _, v := range m.views {
		items = append(items, projectItem{view: v})
	}
	// The command SetItems returns re-runs the filter asynchronously. The
	// filter is re-applied synchronously below instead, so the list is correct
	// before this function returns - which is what makes it testable.
	_ = m.plist.SetItems(items)
	if m.filter != "" {
		m.plist.SetFilterText(m.filter)
	}

	switch {
	case hadSelection:
		m.selectName(was)
	case m.start != "":
		// The first survey: open on the project of the working directory, the
		// way the shell picker preselects it (os_list_json.py:570). After
		// that the user's own selection wins.
		m.selectName(m.start)
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

func (m *Model) selectName(name revier.ProjectName) {
	for i, item := range m.plist.VisibleItems() {
		if it, ok := item.(projectItem); ok && it.view.Project.Name == name {
			m.plist.Select(i)
			return
		}
	}
	m.plist.Select(0)
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
