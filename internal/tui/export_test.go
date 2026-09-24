package tui

import (
	"time"

	bcursor "github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// LookupStart exposes the command Init runs for the starting project, so a
// test delivers its answer at a moment of its choosing.
func (m Model) LookupStart() tea.Cmd { return m.lookupStart() }

// Spin is one tick of the working spinner.
func Spin() tea.Msg { return spinMsg{} }

// WithClock replaces the clock the double-click window is measured on, so a
// test decides how far apart two clicks were.
func (m Model) WithClock(now func() time.Time) Model {
	m.now = now
	return m
}

// Said is the model once the pane's ask for what the agents of the project
// under the cursor said has been answered, as the command it sends would
// answer it: at once, on this goroutine.
func (m Model) Said() Model {
	m.aasked = ""
	cmd := m.askDetails(nil)
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// StaticCursors stops every text field's cursor from blinking. A blink is a
// command that sleeps half a second before it answers, and a test that runs
// the command a focus returns would otherwise wait it out.
func (m Model) StaticCursors() Model {
	for _, in := range []*textinput.Model{&m.input, &m.ainput, &m.path, &m.lname, &m.rinput, &m.sname, &m.chord, &m.pedit} {
		in.Cursor.SetMode(bcursor.CursorStatic)
	}
	return m
}
