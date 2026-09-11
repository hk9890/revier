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
		m.detail, _ = m.detail.Update(msg)
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.list().CursorUp()
	case tea.MouseButtonWheelDown:
		m.list().CursorDown()
	case tea.MouseButtonLeft:
		index, ok := m.rowAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		m.err = nil
		m.list().Select(index)
		last := m.last
		m.last = click{index: index, at: time.Now()}
		if last.index == index && m.last.at.Sub(last.at) < doubleClick {
			m.last = click{}
			return m.enter()
		}
	}
	return m, nil
}

// rowAt is the row of the level in view under a terminal cell, if a row is
// there: the cell is inside the list, and not on the header, the footer or the
// space below the last row.
func (m Model) rowAt(x, y int) (int, bool) {
	mr, mc := m.margins()
	left := mc + frameWidth/2
	if x < left || x >= left+m.listWidth() {
		return 0, false
	}
	// The border row, then the header, the query line and the rule.
	top := mr + frameHeight/2 + chromeHeight - 1
	_, h := m.inner()
	if y < top || y >= top+h {
		return 0, false
	}
	index := (y - top + m.body.YOffset) / m.itemHeight()
	if index >= len(m.list().VisibleItems()) {
		return 0, false
	}
	return index, true
}

// overPane reports whether a column is inside the detail pane: right of the
// margin, the frame's border and padding, and the list.
func (m Model) overPane(x int) bool {
	if m.paneWidth() == 0 {
		return false
	}
	_, mc := m.margins()
	return x >= mc+frameWidth/2+m.listWidth()
}
