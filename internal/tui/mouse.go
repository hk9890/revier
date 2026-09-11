package tui

import tea "github.com/charmbracelet/bubbletea"

// mouse is the wheel, as the picker has it (fzf's default mouse, which
// os-fzf.sh leaves on). Over the detail pane it scrolls the pane, the one
// place with more to read than the screen holds; anywhere else it moves the
// selection, one row a notch, the way the arrow keys do. A click does nothing
// yet.
func (m Model) mouse(msg tea.MouseMsg) Model {
	if msg.Action != tea.MouseActionPress {
		return m
	}
	if m.overPane(msg.X) {
		m.detail, _ = m.detail.Update(msg)
		return m
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.list().CursorUp()
	case tea.MouseButtonWheelDown:
		m.list().CursorDown()
	}
	return m
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
