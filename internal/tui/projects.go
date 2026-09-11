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

// newProjectList is the picker. Filtering is on but its own filter bar is
// hidden, because the surface renders the filter in the header and feeds the
// text in itself (setFilter) - the list never sees a rune key.
func newProjectList(th theme.Theme) list.Model {
	l := list.New(nil, projectDelegate{theme: th}, 0, 0)
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
type projectDelegate struct {
	theme theme.Theme
}

// Height is two: the name line and the path under it. The path is what tells
// two checkouts of the same name apart, and what shows that a project's
// directory is not on this machine (os_list_json.py:476 renders the same two
// lines).
func (d projectDelegate) Height() int                         { return 2 }
func (d projectDelegate) Spacing() int                        { return 0 }
func (d projectDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d projectDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(projectItem)
	if !ok {
		return
	}
	th := d.theme
	sel := index == m.Index()

	style := func(s lipgloss.Style) lipgloss.Style {
		if sel {
			return th.OnSelection(s)
		}
		return s
	}

	v := it.view
	width := m.Width()
	// The mark says whether the project is open, and nothing else: what its
	// agent is doing is the right-hand side's, in an icon and a word.
	mark, markStyle, name := th.Glyphs.Stopped, th.NameDim, th.NameDim
	if v.Running {
		mark, markStyle, name = th.Glyphs.Running, th.Running, th.ProjectName
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
		folder, folderStyle := th.Glyphs.Folder, th.Meta
		if !v.PathExists {
			folder, folderStyle = th.Glyphs.NoFolder, th.PathMissing
		}
		prefix += style(folderStyle).Render(folder) + gap
	}
	indent := lipgloss.Width(prefix)

	// The rows are a grid: the name column is as wide as the widest name on
	// the list, and the state starts right after it, so the states line up
	// and stay beside the names at any width. The activity flows from the
	// state to the edge; it is the one part of the row that can use a wide
	// terminal. Right-aligning the state instead put it a screen away from a
	// short name on a wide list (decisions.md D38).
	nameCol := d.nameColumn(m, width-indent)
	nameText := clipTo(string(v.Project.Name), nameCol)
	first := prefix + highlight(nameText, m.MatchesForItem(index), style(name), style(th.Match))
	if agent := d.agent(v, width-indent-nameCol-gridGap, style); agent != "" {
		first += style(th.Path).Render(strings.Repeat(" ", nameCol-lipgloss.Width(nameText)+gridGap)) + agent
	}

	// The path sits under the name, and what is open, or why nothing can be,
	// follows it on the same line, so the line reads as a sentence about the
	// checkout rather than as two facts a screen apart.
	path := contractHome(v.Project.Path)
	room := width - indent - 1
	tag := d.fitTag(v, lipgloss.Width(path), room, style)
	if tag != "" {
		room -= lipgloss.Width(tag) + gridGap
	}
	// A missing path stays grey: the tag says it, and a third of the rows in
	// maroon from end to end read as a list of errors.
	second := bar + style(th.Path).Render(strings.Repeat(" ", indent-1)) + style(th.Path).Render(elide(path, room))
	if tag != "" {
		second += style(th.Path).Render(strings.Repeat(" ", gridGap)) + tag
	}

	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(first, width, sel, th)+"\n"+fill(second, width, sel, th))
}

// The grid's measures: the gap between two columns, and the most a name
// column takes. A name longer than that is cut, and its row alone pays for
// it, rather than every state on the list moving right for one name.
const (
	gridGap      = 2
	maxNameWidth = 32
)

// nameColumn is the width of the name column: the widest name on the list,
// within its cap, and never so wide that the widest state does not fit after
// it in room.
func (d projectDelegate) nameColumn(m list.Model, room int) int {
	col := 0
	for _, item := range m.VisibleItems() {
		if it, ok := item.(projectItem); ok {
			col = max(col, lipgloss.Width(string(it.view.Project.Name)))
		}
	}
	col = min(col, maxNameWidth)
	if state := lipgloss.Width(statusLabel(d.theme, revier.StatusAttention)); col+gridGap+state > room {
		col = max(room-gridGap-state, 1)
	}
	return col
}

// highlight renders the letters the filter matched in their own style, as fzf
// does: with a fuzzy filter the letters are the only way to see why a row is
// on the list. matches are rune positions, from the list component.
func highlight(text string, matches []int, plain, match lipgloss.Style) string {
	if len(matches) == 0 {
		return plain.Render(text)
	}
	hit := make(map[int]bool, len(matches))
	for _, i := range matches {
		hit[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(text) {
		if hit[i] {
			b.WriteString(match.Render(string(r)))
		} else {
			b.WriteString(plain.Render(string(r)))
		}
	}
	return b.String()
}

// fitTag is the right-hand side of the second line, sized to what the path
// leaves of room.
//
// A directory that is not here is said in words, with what Enter does about
// it (decisions.md D30). That is the one thing on the line the row cannot do
// without - no other column says it - so it shortens, and then cuts the path,
// rather than go. The open targets are the pane's to list as well, so they
// give way to a whole path.
//
// A stopped project with its checkout in place has nothing to say, and
// neither does one with only its home open: that is what the green mark says.
func (d projectDelegate) fitTag(v revier.ProjectView, path, room int, style func(lipgloss.Style) lipgloss.Style) string {
	th := d.theme
	if !v.PathExists {
		long, short := "not on this machine", "not here"
		if v.Project.GitURL != "" {
			long, short = "not cloned · enter clones", "not cloned"
		}
		tag := long
		if path+lipgloss.Width(long)+2 > room {
			tag = short
		}
		return style(th.PathMissing).Render(tag)
	}
	var open []string
	for _, t := range v.Targets {
		if !t.Ref.IsZero() {
			open = append(open, string(t.Name))
		}
	}
	tag := strings.Join(open, " · ")
	if len(open) == 1 && !v.Home.IsZero() || path+lipgloss.Width(tag)+2 > room {
		return ""
	}
	return style(th.NameDim).Render(tag)
}

// agent is the worst agent state in the project and what it is doing: the
// right-hand side of the row, and the part that answers "which of these needs
// me". A project running several agents is why the detail pane lists them all.
//
// It fits room by cutting the activity, never the state: on a narrow screen
// "needs you" is what the row is there to show.
func (d projectDelegate) agent(v revier.ProjectView, room int, style func(lipgloss.Style) lipgloss.Style) string {
	worst, ok := core.Worst(v.Agents)
	if !ok {
		return ""
	}
	th := d.theme
	state := statusLabel(th, worst.Status)
	if lipgloss.Width(state) > room {
		return ""
	}
	out := style(statusStyle(th, worst.Status)).Render(state)
	if rest := room - lipgloss.Width(state) - 1; worst.Activity != "" && rest >= minActivityWidth {
		out += style(th.NameDim).Render(" " + ellipsis(worst.Activity, rest))
	}
	return out
}

// minActivityWidth is the shortest cut of an activity line that still says
// something. Below it the state stands alone.
const minActivityWidth = 8

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
	if m.level == levelTargets {
		m.reloadTargets()
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

// setFilter is every change to the filter text. The list filters
// synchronously through SetFilterText, so the count in the header and the
// selection are right on the same pass as the keystroke.
//
// Clearing the filter keeps the cursor on the project it was on. A search
// ends on the project that was searched for, and dropping the cursor back on
// the first row when the query goes lost the one row the user had just found.
func (m *Model) setFilter(f string) {
	m.filter = f
	if m.input.Value() != f {
		m.input.SetValue(f)
	}
	if f != "" {
		m.plist.SetFilterText(f)
		return
	}
	// ResetFilter keeps the cursor's index in the filtered list, which in the
	// full list is another project; the selection goes back by name.
	was, ok := m.selectedName()
	m.plist.ResetFilter()
	if ok {
		m.selectName(was)
	}
}
