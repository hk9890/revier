package tui

import "github.com/charmbracelet/bubbles/viewport"

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
	l, itemHeight := m.list(), m.itemHeight()

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

// itemHeight is how many lines one row of the level in view takes.
func (m Model) itemHeight() int {
	if m.level == levelProjects {
		return projectDelegate{}.Height()
	}
	return 1 // every other level's rows are one line
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

// listWidth is the room the list has once the detail pane has taken its share.
func (m Model) listWidth() int {
	w, _ := m.inner()
	if pane := m.paneWidth(); pane > 0 {
		w -= pane
	}
	return w
}
