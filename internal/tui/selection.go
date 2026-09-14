package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A drag of the left button selects a box of the screen and copies its text.
// The terminal's own selection cannot be used: the surface asks for every
// mouse event, so the terminal selects only with shift held and then tells
// the surface nothing, and a survey rewriting a line under the selection
// clears it.
//
// The box is drawn over the screen as it was when the drag began, and the
// text is taken from that screen, so a survey landing mid-drag moves nothing
// under the pointer. The model keeps updating underneath and is shown again on
// release.
type selection struct {
	active   bool
	from, to pointerCell
	screen   string // the surface as it was when the drag began
}

// box is the selection's corners, top left first, inclusive.
func (s selection) box() (left, top, right, bottom int) {
	return min(s.from.x, s.to.x), min(s.from.y, s.to.y), max(s.from.x, s.to.x), max(s.from.y, s.to.y)
}

// text is what the box holds, one line per row, without colour and without
// the spaces a row is padded with.
func (s selection) text() string {
	left, top, right, bottom := s.box()
	lines := strings.Split(s.screen, "\n")
	var out []string
	for y := top; y <= bottom && y < len(lines); y++ {
		cut := ansi.Cut(ansi.Strip(lines[y]), left, right+1)
		out = append(out, strings.TrimRight(cut, " "))
	}
	return strings.Join(out, "\n")
}

// view is the frozen screen with the box in reverse video. The styles of a
// line resume after the box, because the part after it keeps the escape
// codes of the part cut away.
func (s selection) view() string {
	left, top, right, bottom := s.box()
	lines := strings.Split(s.screen, "\n")
	width := right - left + 1
	for y := top; y <= bottom && y < len(lines); y++ {
		line := lines[y]
		before := ansi.Truncate(line, left, "")
		inside := ansi.Cut(ansi.Strip(line), left, right+1)
		lines[y] = pad(before, left) +
			"\x1b[0;7m" + pad(inside, width) + "\x1b[0m" +
			ansi.TruncateLeft(line, right+1, "")
	}
	return strings.Join(lines, "\n")
}

// drag follows the left button past the cell it was pressed on. The first
// move off that cell begins the selection.
func (m Model) drag(msg tea.MouseMsg) Model {
	at := pointerCell{x: msg.X, y: msg.Y}
	if !m.sel.active {
		if m.press == nil || *m.press == at {
			return m
		}
		m.sel = selection{active: true, from: *m.press, screen: m.View()}
	}
	m.sel.to = at
	return m
}

// release ends a press. After a drag it copies the box; after a press that
// stayed on its cell, it runs the button or target it landed on, so a drag
// that begins on one runs nothing.
func (m Model) release(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	press := m.press
	m.press = nil
	if m.sel.active {
		text := m.sel.text()
		m.sel = selection{}
		m.copied = len([]rune(text))
		return m, copyText(text)
	}
	if press == nil || *press != (pointerCell{x: msg.X, y: msg.Y}) {
		return m, nil
	}
	return m.clickAction()
}

// selectingKey is a key pressed during a drag. Esc drops the selection and
// copies nothing; ctrl+c still quits. Every other key waits, because it would
// change a screen the selection stands over.
func (m Model) selectingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.sel, m.press = selection{}, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// copyText puts text on the system clipboard with OSC 52, which reaches the
// clipboard of the machine the terminal runs on, over ssh too. It is one
// write, so it cannot land inside a frame the renderer is writing.
func copyText(text string) tea.Cmd {
	return func() tea.Msg {
		_, _ = os.Stdout.WriteString(ansi.SetSystemClipboard(text))
		return nil
	}
}
