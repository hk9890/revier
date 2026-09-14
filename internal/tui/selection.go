package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
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

// dragFrom is how far, in cells either way, the pointer moves from the press
// before a drag begins. A hand that shifts one cell during a click is still
// clicking, and a selection there would overwrite the clipboard with two
// characters and run nothing.
const dragFrom = 2

// press is where the left button went down and what it went down on.
type press struct {
	at   pointerCell
	over hovered
}

// held is a mouse event while the left button is down. Only the left button
// moving or coming up moves or ends a drag; a wheel or another button during
// a selection would change the screen the selection stands over, so it is
// dropped. Motion with no button held says the release went to another
// window, so the press is forgotten and nothing is copied. done is false for
// an event that is not the drag's, which the caller handles as usual.
func (m Model) held(msg tea.MouseMsg) (Model, bool) {
	switch {
	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft:
		return m.drag(msg), true
	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone:
		m.press, m.sel = nil, selection{}
	case msg.Action == tea.MouseActionRelease && msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonNone:
		return m, true
	case msg.Action == tea.MouseActionPress && m.sel.active:
		return m, true
	}
	return m, false
}

// drag follows the left button. A selection begins once the pointer is
// dragFrom cells from the press.
func (m Model) drag(msg tea.MouseMsg) Model {
	at := pointerCell{x: msg.X, y: msg.Y}
	if !m.sel.active {
		from := m.press.at
		if max(abs(at.x-from.x), abs(at.y-from.y)) < dragFrom {
			return m
		}
		m.sel = selection{active: true, from: from, screen: m.View()}
	}
	m.sel.to = at
	return m
}

func abs(n int) int {
	return max(n, -n)
}

// release ends a press. After a drag it copies the box, unless the box holds
// only blank cells: an empty copy clears the clipboard. After a press with no
// drag, it runs the button or target it landed on, so a drag that begins on
// one runs nothing.
func (m Model) release() (tea.Model, tea.Cmd) {
	p := m.press
	m.press = nil
	if m.sel.active {
		text := m.sel.text()
		m.sel = selection{}
		if strings.TrimSpace(text) == "" {
			return m, nil
		}
		m.copied = len([]rune(strings.ReplaceAll(text, "\n", "")))
		return m, copyText(text)
	}
	if p == nil {
		return m, nil
	}
	return m.clickAction(*p)
}

// selectingKey is a key pressed during a drag. Back drops the selection and
// copies nothing, and quit still quits. Every other key is dropped, because it
// would change a screen the selection stands over.
func (m Model) selectingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.sel, m.press = selection{}, nil
	case key.Matches(msg, m.keys.Quit):
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
