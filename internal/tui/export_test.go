package tui

import (
	"time"

	bcursor "github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
)

// WithClock replaces the clock the double-click window is measured on, so a
// test decides how far apart two clicks were.
func (m Model) WithClock(now func() time.Time) Model {
	m.now = now
	return m
}

// StaticCursors stops every text field's cursor from blinking. A blink is a
// command that sleeps half a second before it answers, and a test that runs
// the command a focus returns would otherwise wait it out.
func (m Model) StaticCursors() Model {
	for _, in := range []*textinput.Model{&m.input, &m.tinput, &m.ainput, &m.path, &m.lname, &m.rinput, &m.sname, &m.chord, &m.pedit} {
		in.Cursor.SetMode(bcursor.CursorStatic)
	}
	return m
}
