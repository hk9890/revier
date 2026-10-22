package tui

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/core"
)

// LookupStart exposes the command Init runs for the starting project, so a
// test delivers its answer at a moment of its choosing.
func (m Model) LookupStart() tea.Cmd { return m.lookupStart() }

// Spin is one tick of the working spinner.
func Spin() tea.Msg { return spinMsg{} }

// Interval is how long the surface waits now for its next refresh.
func (m Model) Interval() time.Duration { return m.interval() }

// Ticked is the refresh tick that is out, as it arrives: a test delivers it
// without waiting it out.
func (m Model) Ticked() tea.Msg { return tickMsg{chain: m.chain} }

// ShutQuit is the end of the wait after a full shutdown, as it arrives: a
// test delivers it without waiting it out.
func ShutQuit() tea.Msg { return shutQuitMsg{} }

// WithClock replaces the clock the double-click window and the gap between
// two rounds to a linked host are measured on, so a test decides how far
// apart two clicks or two surveys were.
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

// Mirrored is the model once the mirror's read of the agent the pane mirrors
// has answered, as the command its tick sends would answer it: at once, on
// this goroutine.
func (m Model) Mirrored() Model {
	next, _ := m.Update(mirrorTickMsg{gen: m.mirror.gen})
	m = next.(Model)
	_, a, ok := m.mirroring()
	if !ok {
		return m
	}
	next, _ = m.Update(m.mirror.readScreen(m.core, a)())
	return next.(Model)
}

// MirrorTicked is the model after one tick of the mirror's timer.
func (m Model) MirrorTicked() Model {
	next, _ := m.Update(mirrorTickMsg{gen: m.mirror.gen})
	return next.(Model)
}

// MirrorAsked is how many reads of a panel the mirror has sent for.
func (m Model) MirrorAsked() int { return m.mirror.seq }

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
		st := in.Styles()
		st.Cursor.Blink = false
		in.SetStyles(st)
	}
	return m
}

// AssistArgs is `revier assist` as the surface runs it, after the binary's
// own path.
func (m Model) AssistArgs() []string { return m.assistArgs() }

// Assisted is the model once the assistant the surface handed the terminal to
// has exited with err, and the command that follows.
func (m Model) Assisted(err error) (Model, tea.Cmd) {
	next, cmd := m.Update(assistedMsg{err: err})
	return next.(Model), cmd
}

// Acted is the model once an action the surface handed the terminal to has
// exited with err, and the command that follows.
func (m Model) Acted(err error) (Model, tea.Cmd) {
	next, cmd := m.Update(actedMsg{err: err})
	return next.(Model), cmd
}

// Cloned is the model once the clone of p the surface handed the terminal to
// has exited with err, and the command that follows.
func (m Model) Cloned(p core.Project, err error) (Model, tea.Cmd) {
	home, _ := p.Home()
	next, cmd := m.Update(clonedMsg{project: p, home: home.Name, err: err})
	return next.(Model), cmd
}
