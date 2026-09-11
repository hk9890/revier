package tui

import (
	"fmt"
	"io"

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
	mark, markStyle, name := th.Glyphs.Stopped, th.NameDim, th.NameDim
	if v.Running {
		mark, markStyle, name = th.Glyphs.Running, th.Running, th.ProjectName
	}
	if v.Attention() {
		mark, markStyle = th.Glyphs.Attention, th.Attention
	}
	cursor := " "
	if sel {
		cursor = th.Glyphs.Cursor
	}

	// Name on the left, agent state on the right. Padding every name to the
	// longest one on screen put the state column half a row away from a short
	// name; the right edge does not move.
	left := style(th.Accent).Render(cursor+" ") + style(markStyle).Render(mark) + style(th.Path).Render(" ") +
		style(name).Render(string(v.Project.Name))
	first := spread(left, d.agent(v, style, m.Width()-lipgloss.Width(left)-1), m.Width())

	pathStyle := th.Path
	if !v.PathExists {
		pathStyle = th.PathMissing
	}
	second := style(th.Path).Render("    ") +
		style(pathStyle).Render(contractHome(v.Project.Path))

	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(first, m.Width(), sel, th)+"\n"+fill(second, m.Width(), sel, th))
}

// agent is the worst agent state in the project and what it is doing: the
// right-hand side of the row, and the part that answers "which of these needs
// me". A project running several agents is why the detail pane lists them all.
//
// It fits in room by cutting the activity and never the state: the state is
// the answer, and the activity is what the detail pane shows whole.
func (d projectDelegate) agent(v revier.ProjectView, style func(lipgloss.Style) lipgloss.Style, room int) string {
	worst, ok := core.Worst(v.Agents)
	if !ok {
		return ""
	}
	th := d.theme
	var s lipgloss.Style
	switch worst.Status {
	case revier.StatusAttention:
		s = th.Attention
	case revier.StatusRunning:
		s = th.Running
	case revier.StatusIdle:
		s = th.Idle
	default:
		s = th.NameDim
	}
	status := worst.Status.String()
	out := style(s).Render(status)
	if activity := ellipsize(worst.Activity, room-lipgloss.Width(status)-1); activity != "" {
		out += style(th.NameDim).Render(" " + activity)
	}
	return out
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
func (m *Model) setFilter(f string) {
	was, hadSelection := m.selectedName()
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
	m.plist.ResetFilter()
	if hadSelection {
		m.selectName(was)
	}
}
