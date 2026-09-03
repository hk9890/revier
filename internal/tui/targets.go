package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
)

// targetItem is one row at the target level: a declared target, or an
// instance attached by hand, which has a ref and no name.
type targetItem struct {
	row targetRow
}

func (i targetItem) FilterValue() string {
	if !i.row.attached.IsZero() {
		return i.row.attached.Title
	}
	return string(i.row.target.Name)
}

// newTargetList has filtering off: a project has a handful of targets, and a
// filter that hides one of them would hide the key that reaches it.
func newTargetList(th theme.Theme) list.Model {
	l := list.New(nil, targetDelegate{theme: th}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return l
}

type targetDelegate struct {
	theme theme.Theme
}

func (d targetDelegate) Height() int                         { return 1 }
func (d targetDelegate) Spacing() int                        { return 0 }
func (d targetDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d targetDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(targetItem)
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
	cursor := " "
	if sel {
		cursor = th.Glyphs.Cursor
	}

	var row string
	if r := it.row.attached; !r.IsZero() {
		row = style(th.Accent).Render(cursor+" ") +
			style(th.NameDim).Render(" "+pad("", keyWidth)) +
			style(th.ProjectName).Render(pad(r.Title, nameWidth)) +
			style(th.Meta).Render("attached · "+r.Host)
	} else {
		t := it.row.target
		mark, state := th.Glyphs.Stopped, th.NameDim.Render("-")
		switch {
		case !t.Available:
			state = th.NameDim.Render("unavailable")
		case !t.Ref.IsZero():
			mark, state = th.Glyphs.Running, th.Running.Render("running")
		}
		row = style(th.Accent).Render(cursor+" ") +
			style(th.NameDim).Render(mark) + style(th.Path).Render(" ") +
			style(th.Accent).Render(pad(t.Key, keyWidth)) +
			style(th.ProjectName).Render(pad(string(t.Name), nameWidth)) +
			style(th.Meta).Render(pad(t.Host, hostWidth)) +
			selStyle(state, sel, th)
	}
	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(row, m.Width(), sel, th))
}

// The target level's columns. They are fixed because every row of a project
// carries the same four fields, and a key legend that moves between projects
// is harder to learn than one that does not.
const (
	keyWidth  = 14
	nameWidth = 24
	hostWidth = 7
)

// reloadTargets puts the drilled-into project's rows into the target list,
// keeping the cursor where it was so a refresh does not move it.
func (m *Model) reloadTargets() {
	rows := m.targetRows()
	items := make([]list.Item, 0, len(rows))
	for _, r := range rows {
		items = append(items, targetItem{row: r})
	}
	at := m.tlist.Index()
	_ = m.tlist.SetItems(items)
	if at >= len(items) {
		at = len(items) - 1
	}
	if at < 0 {
		at = 0
	}
	m.tlist.Select(at)
}
