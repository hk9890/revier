package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// doubleClick is how close two clicks on one row are to count as a double
// click. The desktop's own is between 400 and 500 ms.
const doubleClick = 400 * time.Millisecond

// zone is where a click landed, so two clicks on the first row of the list
// and the first row of the pane are two choices and not a double click.
type zone int

const (
	zoneList zone = iota
	zonePane
)

// click is the last press of the left button: where it landed, on which row,
// and when, so the next press can tell a double click from a second choice.
type click struct {
	zone  zone
	index int
	at    time.Time
}

// mouse is the wheel, the pointer and the left button. The wheel over the
// detail pane scrolls the pane, the one place with more to read than the
// screen holds; anywhere else it moves the selection, one row a notch, the
// way the arrow keys do. A click selects a row and a second click on it
// opens the row, in the list and in the pane alike (decisions.md D36). The
// action bar is the exception: a button has nothing to select, so one click
// runs it, and the pointer resting on it lights it as the selected row is
// lit.
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// A delete waiting for its answer holds the screen still: the question
	// names one project, and a wheel moving the highlight to another would
	// leave the user answering about a row they are no longer looking at.
	if m.confirm != "" {
		return m, nil
	}
	// The pointer moving is the only message that is not an act: it lights a
	// button and changes nothing else.
	if over := m.barAt(msg.X, msg.Y); over != m.hover {
		m.hover = over
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if m.hover >= 0 {
		if msg.Button == tea.MouseButtonLeft {
			m.err = nil
			return barActions[m.hover].run(m)
		}
		return m, nil
	}
	if m.overPane(msg.X) {
		if msg.Button == tea.MouseButtonLeft && m.dialog == dialogNone {
			return m.clickPane(msg.Y)
		}
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
		if m.second(zoneList, index) {
			if m.dialog != dialogNone {
				return m.dialogEnter()
			}
			return m.enter()
		}
	}
	return m, nil
}

// second records a press and reports whether it completes a double click:
// the same row of the same zone, inside the double-click time.
func (m *Model) second(z zone, index int) bool {
	last := m.last
	m.last = click{zone: z, index: index, at: time.Now()}
	if last.zone != z || last.index != index || m.last.at.Sub(last.at) >= doubleClick {
		return false
	}
	m.last = click{}
	return true
}

// clickPane is a click on the pane. On a target row the first click moves
// the cursor to it and the second runs it, as Enter on it does. Anywhere
// else it does nothing.
func (m Model) clickPane(y int) (tea.Model, tea.Cmd) {
	line, ok := m.bodyLine(y)
	if !ok {
		return m, nil
	}
	for i, at := range m.tlines {
		if at != line+m.detail.YOffset {
			continue
		}
		m.err = nil
		m.focus, m.tcursor = focusPane, i
		if m.second(zonePane, i) {
			return m, m.goRow(i)
		}
		return m, nil
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
// row, the header, the query line, the action bar and the rule, and above
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
