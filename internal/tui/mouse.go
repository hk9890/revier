package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// doubleClick is how close two clicks on one row are to count as a double
// click. The desktop's own is between 400 and 500 ms.
const doubleClick = 400 * time.Millisecond

// click is the last press of the left button: which row it landed on, and
// when, so the next press can tell a double click from a second choice.
type click struct {
	index int
	at    time.Time
}

// mouse is the wheel and the left button, as the picker has them (fzf's
// default mouse, which os-fzf.sh leaves on). The wheel over the detail pane
// scrolls the pane, the one place with more to read than the screen holds;
// anywhere else it moves the selection, one row a notch, the way the arrow
// keys do. A click on a row selects it, and a second click on the same row
// opens it, as Enter does (decisions.md D36).
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// A delete waiting for its answer holds the screen still: the question
	// names one project, and a wheel moving the highlight to another would
	// leave the user answering about a row they are no longer looking at.
	if msg.Action != tea.MouseActionPress || m.confirm != "" {
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

// clickPane is a click in the pane. On a target row it runs the target, as
// Enter on it does: one click, not two, because a target has no state worth
// selecting other than running it (decisions.md D42). Anywhere else it does
// nothing.
func (m Model) clickPane(y int) (tea.Model, tea.Cmd) {
	line, ok := m.bodyLine(y)
	if !ok {
		return m, nil
	}
	for i, at := range m.tlines {
		if at == line+m.detail.YOffset {
			m.err = nil
			m.focus, m.tcursor = focusPane, i
			return m, m.goRow(i)
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
// row, the header, the query line and the rule, and above the footer.
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
