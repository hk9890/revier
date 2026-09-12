package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// doubleClick is how close two clicks on one row are to count as a double
// click. The desktop's own is between 400 and 500 ms.
const doubleClick = 400 * time.Millisecond

// click is the last press of the left button: which row it landed on and
// when, so the next press can tell a double click from a second choice.
type click struct {
	index int
	at    time.Time
}

// mouse is the wheel, the pointer and the left button. The wheel over the
// detail pane scrolls the pane, the one place with more to read than the
// screen holds; anywhere else it moves the selection, one row a notch, the
// way the arrow keys do.
//
// A row of the list is chosen with one click and opened with a second, as a
// row of a file manager is: choosing it costs nothing and opening it opens a
// window. A target in the pane and a button on the bar are not rows but
// things to do, and one click does them (decisions.md D36).
//
// Whatever the pointer is over is lit, so all three say they can be clicked
// before they are.
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// A delete waiting for its answer holds the screen still: the question
	// names one project, and a wheel moving the highlight to another would
	// leave the user answering about a row they are no longer looking at.
	if m.confirm != "" {
		return m, nil
	}
	m.over = m.hoverAt(msg.X, msg.Y)
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft {
		switch m.over.kind {
		case hoverBar:
			m.err = nil
			return barActions[m.over.index].run(m)
		case hoverTarget:
			m.err = nil
			m.focus, m.tcursor = focusPane, m.over.index
			return m, m.goRow(m.over.index)
		}
	}
	if m.overPane(msg.X) {
		m.detail, _ = m.detail.Update(msg)
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.bodyList().CursorUp()
	case tea.MouseButtonWheelDown:
		m.bodyList().CursorDown()
	case tea.MouseButtonLeft:
		index, ok := m.rowAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		m.err = nil
		if m.dialog == dialogNone {
			m.focus = focusList
		}
		m.bodyList().Select(index)
		last := m.last
		m.last = click{index: index, at: time.Now()}
		if last.index == index && m.last.at.Sub(last.at) < doubleClick {
			m.last = click{}
			if m.dialog != dialogNone {
				return m.dialogEnter()
			}
			return m.enter()
		}
	}
	return m, nil
}

// rowAt is the list row under a terminal cell, if a row is there: the cell is
// inside the list, and not on the header, the footer or the space below the
// last row.
func (m Model) rowAt(x, y int) (int, bool) {
	_, mc := m.margins()
	left := mc + frameWidth/2
	if x < left || x >= left+m.listWidth() {
		return 0, false
	}
	line, ok := m.bodyLine(y)
	if !ok {
		return 0, false
	}
	index := (line + m.body.YOffset) / m.itemHeight()
	if index >= len(m.bodyList().VisibleItems()) {
		return 0, false
	}
	return index, true
}

// bodyLine is the line of the body a terminal row is on: below the border
// row, the action bar, the header, the query line and the rule, and above
// the footer.
func (m Model) bodyLine(y int) (int, bool) {
	mr, _ := m.margins()
	top := mr + frameHeight/2 + chromeHeight - 1
	_, h := m.inner()
	if y < top || y >= top+h {
		return 0, false
	}
	return y - top, true
}

// overPane reports whether a column is inside the detail pane: right of the
// margin, the frame's border and padding, and the list.
func (m Model) overPane(x int) bool {
	if m.paneCols() == 0 {
		return false
	}
	_, mc := m.margins()
	return x >= mc+frameWidth/2+m.listWidth()
}
