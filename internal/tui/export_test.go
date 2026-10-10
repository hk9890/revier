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

// Mirrored is the model once the mirror's read of the agent under the agent
// list's cursor has answered, as the command its tick sends would answer it:
// at once, on this goroutine.
func (m Model) Mirrored() Model {
	next, _ := m.Update(mirrorTickMsg{})
	m = next.(Model)
	it, ok := m.agents.selected()
	if !ok {
		return m
	}
	next, _ = m.Update(m.agents.readScreen(m.core, it.agent)())
	return next.(Model)
}

// MirrorTicked is the model after one tick of the mirror's timer.
func (m Model) MirrorTicked() Model {
	next, _ := m.Update(mirrorTickMsg{})
	return next.(Model)
}

// MirrorAsked is how many reads of a panel the mirror has sent for.
func (m Model) MirrorAsked() int { return m.agents.mirror.seq }

// ScreenLines exposes the mirror's setting of a panel's screen.
func ScreenLines(text string, w int) []string { return screenLines(text, w) }

// Raised is the popup hidden and raised again, without the window host.
func (m Model) Raised() Model {
	m.popup = true
	next, _ := m.Update(hiddenMsg{})
	next, _ = next.Update(tea.FocusMsg{})
	return next.(Model)
}

// StaticCursors stops every text field's cursor from blinking. A blink is a
// command that sleeps half a second before it answers, and a test that runs
// the command a focus returns would otherwise wait it out.
func (m Model) StaticCursors() Model {
	for _, in := range []*textinput.Model{&m.input, &m.ainput, &m.agents.query, &m.create.path, &m.link.name, &m.link.query, &m.sessions.name, &m.config.chord, &m.proj.edit} {
		in.Cursor.SetMode(bcursor.CursorStatic)
	}
	return m
}

// Assisted is the model once the assistant the surface handed the terminal to
// has exited with err, and the command that follows.
func (m Model) Assisted(err error) (Model, tea.Cmd) {
	next, cmd := m.Update(assistedMsg{err: err})
	return next.(Model), cmd
}
