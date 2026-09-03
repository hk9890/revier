package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	// nameWidth is the column the agent state starts at, computed per render
	// pass from the visible rows so the columns line up on this screen and
	// not on the widest name in the store.
	nameWidth int
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
	state := th.NameDim.Render(pad("-", stateWidth))
	if v.Running {
		mark, markStyle, name = th.Glyphs.Running, th.Running, th.ProjectName
		state = th.Running.Render(pad("running", stateWidth))
	}
	if v.Attention() {
		mark, markStyle = th.Glyphs.Attention, th.Attention
	}
	cursor := " "
	if sel {
		cursor = th.Glyphs.Cursor
	}

	first := style(th.Accent).Render(cursor+" ") +
		style(markStyle).Render(mark) + style(th.Path).Render(" ") +
		style(name).Render(pad(string(v.Project.Name), d.nameWidth)) +
		style(th.Path).Render("  ") + selStyle(state, sel, th) +
		style(th.Path).Render("  ") +
		d.agent(v, style)

	pathStyle := th.Path
	if !v.PathExists {
		pathStyle = th.PathMissing
	}
	second := style(th.Path).Render("    ") +
		style(pathStyle).Render(truncate(contractHome(v.Project.Path), m.Width()-5))

	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(first, m.Width(), sel, th)+"\n"+fill(second, m.Width(), sel, th))
}

// agent is the worst agent state in the project and its activity: the part of
// the row that answers "which one needs me".
func (d projectDelegate) agent(v revier.ProjectView, style func(lipgloss.Style) lipgloss.Style) string {
	if len(v.Agents) == 0 {
		return ""
	}
	worst := revier.AgentState{}
	for _, a := range v.Agents {
		if a.State.Status >= worst.Status {
			worst = a.State
		}
	}
	th := d.theme
	label := worst.Status.String()
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
	out := style(s).Render(label)
	if worst.Activity != "" {
		out += style(th.Path).Render(" " + worst.Activity)
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
	m.setNameWidth()

	if hadSelection {
		m.selectName(was)
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
	m.filter = f
	if f == "" {
		m.plist.ResetFilter()
	} else {
		m.plist.SetFilterText(f)
	}
	m.setNameWidth()
}

// setNameWidth measures the visible rows. Padding to the widest name in the
// store would leave a filtered list of three short names indented across half
// the screen.
func (m *Model) setNameWidth() {
	w := 0
	for _, item := range m.plist.VisibleItems() {
		it, ok := item.(projectItem)
		if !ok {
			continue
		}
		if n := lipgloss.Width(string(it.view.Project.Name)); n > w {
			w = n
		}
	}
	if w > maxNameWidth {
		w = maxNameWidth
	}
	m.plist.SetDelegate(projectDelegate{theme: m.theme, nameWidth: w})
}

// stateWidth holds the workspace column, so the agent state after it starts
// at the same place whether the workspace is up or not.
const stateWidth = 7

// maxNameWidth caps the name column. A name longer than this is not truncated,
// it just stops holding the column, which keeps one outlier from indenting
// every other row.
const maxNameWidth = 32
