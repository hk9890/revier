package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
)

// The list component pages: its cursor walks to the bottom of a page and the
// next press replaces every row on the screen. With ninety projects reached by
// a held-down arrow key that is the normal way through the list, so the rows
// flicker through unrelated names on the way to a neighbour.
//
// The list keeps what it is good at - fuzzy ranking, the filter, which item is
// selected - and is given room for every row it holds, so it renders one page.
// A viewport clips that page to the screen and scrolls it by one line at a
// time (decisions.md D23).
//
// The cost is that the delegate renders every visible item on every frame
// rather than one screen of them: ninety projects is a hundred and eighty
// lines, which is a rounding error next to the survey that produced them.
func (m *Model) syncBody() {
	// The new-project and config screens have no list rows: the body is
	// their own text.
	switch m.dialog {
	case dialogNew:
		m.body.SetContent(m.newScreen())
		m.body.SetYOffset(0)
		return
	case dialogLinkName:
		m.body.SetContent(m.linkNameScreen())
		m.body.SetYOffset(0)
		return
	case dialogConfig:
		text, at := m.configScreen()
		m.body.SetContent(text)
		m.follow(at, 1)
		return
	}
	// The help screen scrolls itself: its offset is the reader's, and a
	// survey arriving under it must not move it.
	if m.dialog == dialogHelp {
		// It has no pane beside it, so it is wider than the list it
		// stands over.
		m.body.Width = m.listWidth()
		offset := m.body.YOffset
		m.body.SetContent(m.helpScreen())
		m.body.SetYOffset(offset)
		return
	}
	// The delegate renders the rows and is handed nothing but the row, so
	// the hovered one is set on it before the list draws.
	row := -1
	if m.over.kind == hoverRow {
		row = m.over.index
	}
	m.plist.SetDelegate(projectDelegate{theme: m.theme, hover: row})
	m.rlist.SetDelegate(projectDelegate{theme: m.theme, hover: row})
	l, itemHeight := m.bodyList(), m.itemHeight()

	n := len(l.VisibleItems())
	if n < 1 {
		m.body.SetContent(m.empty())
		m.body.SetYOffset(0)
		return
	}
	l.SetSize(m.listWidth(), n*itemHeight)
	m.body.SetContent(l.View())
	m.follow(l.Index()*itemHeight, itemHeight)
}

// bodyList is the list the body scrolls: the link dialog's rows while it is
// up, the projects otherwise. It is a pointer because the cursor moves on it.
func (m *Model) bodyList() *list.Model {
	switch m.dialog {
	case dialogHosts:
		return &m.hlist
	case dialogRemote:
		return &m.rlist
	}
	return &m.plist
}

// itemHeight is how many lines one row of the list takes.
func (m Model) itemHeight() int {
	if m.dialog == dialogHosts {
		return 1 // a host is its name alone
	}
	return projectDelegate{}.Height()
}

// follow keeps the selected row on the screen, scrolling by the least that
// does it. Centring instead would move the whole list under a cursor that only
// stepped one row.
func (m *Model) follow(top, height int) {
	bottom := top + height
	switch {
	case top < m.body.YOffset:
		m.body.SetYOffset(top)
	case bottom > m.body.YOffset+m.body.Height:
		m.body.SetYOffset(bottom - m.body.Height)
	}
}

func newBody() viewport.Model { return viewport.New(0, 0) }

// listWidth is the room the list has once the detail pane has taken its
// share: none, while the pane stands in the list's place.
func (m Model) listWidth() int {
	w, _ := m.inner()
	return w - m.paneCols()
}
