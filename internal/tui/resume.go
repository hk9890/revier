package tui

import (
	"log/slog"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
)

// The start dialog: the first screen after a full shutdown, while the state
// records one (decisions.md D141). It offers the session the shutdown saved,
// the sessions screen, or the surface as it is. It is a screen like the
// others (D8), not a second surface.

// resumeScreen is the start dialog: when revier was shut down, and the
// session to offer, which is zero when none is saved.
type resumeScreen struct {
	at      time.Time
	session session.Session
	row     int
}

// resumeRow is one choice of the dialog.
type resumeRow struct {
	label string
	run   func(Model) (tea.Model, tea.Cmd)
}

// openResume puts the dialog up over the surface for the shutdown on record.
// The session it names comes first; one deleted since gives way to the
// newest. A shutdown that saved none offers none.
func (m *Model) openResume(sd state.ShutDown) {
	var s session.Session
	if sd.Session != "" {
		var err error
		if s, err = session.Load(m.stateRoot, sd.Session); err != nil {
			s, _ = session.Load(m.stateRoot, "")
		}
	}
	m.resume = resumeScreen{at: sd.At, session: s}
	m.dialog = dialogResume
}

// resumeRows are the dialog's choices. With no session saved there is
// nothing to restore, and the row is left out.
func (m Model) resumeRows() []resumeRow {
	var rows []resumeRow
	if m.resume.session.ID != "" {
		rows = append(rows, resumeRow{"Restore this session", Model.resumeRestore})
	}
	return append(rows,
		resumeRow{"Pick another session", Model.resumePick},
		resumeRow{"Start fresh", Model.startFresh})
}

// resumeKey is every press on the dialog. Esc is Start fresh.
func (m Model) resumeKey(msg keyPress) (tea.Model, tea.Cmd) {
	m.err = nil
	r := &m.resume
	rows := m.resumeRows()
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		return m.startFresh()
	case key.Matches(msg, m.keys.Up):
		r.row = max(r.row-1, 0)
	case key.Matches(msg, m.keys.Down):
		r.row = min(r.row+1, len(rows)-1)
	case key.Matches(msg, m.keys.Enter):
		return rows[r.row].run(m)
	}
	return m, nil
}

// resumeRestore restores the session the dialog offers, on the sessions
// screen, which shows the walk as it goes. The restore ends the shutdown on
// record as it starts.
func (m Model) resumeRestore() (tea.Model, tea.Cmd) {
	slog.Info("start dialog: restore", "id", m.resume.session.ID)
	m.dialog = dialogSessions
	if m.err = m.sessions.open(m.stateRoot, *m.theme, m.resume.session.ID); m.err != nil {
		return m, nil
	}
	cmd, err := m.sessions.restore(m.surface())
	m.err = err
	return m, cmd
}

// resumePick is the sessions screen, with the cursor on the session the
// dialog offered. The shutdown stays on record until something opens.
func (m Model) resumePick() (tea.Model, tea.Cmd) {
	slog.Info("start dialog: pick another session")
	m.dialog = dialogSessions
	m.err = m.sessions.open(m.stateRoot, *m.theme, m.resume.session.ID)
	return m, nil
}

// startFresh is the surface as it is. It ends the shutdown on record and
// deletes no session.
func (m Model) startFresh() (tea.Model, tea.Cmd) {
	slog.Info("start dialog: start fresh")
	app.ClearShutDown(m.core)
	m.dialog = dialogNone
	return m, nil
}

// resumeTitle is the line over the rule: when revier was shut down. What the
// session holds is the pane's, which wraps; this line is one line in the
// list's column.
func (m Model) resumeTitle() string {
	r := m.resume
	at := r.at.Local()
	when := at.Format("15:04")
	if now := m.now().Local(); at.YearDay() != now.YearDay() || at.Year() != now.Year() {
		when = at.Format("2006-01-02 15:04")
	}
	return "revier was shut down at " + when + "."
}

// resumeScreenRows is the dialog's choices, in the list's place.
func (m Model) resumeScreenRows() string {
	var labels []string
	for _, r := range m.resumeRows() {
		labels = append(labels, r.label)
	}
	return choiceRows(*m.theme, labels, m.resume.row, m.listWidth())
}

// resumeDetail is the pane: the session the dialog offers, and what each
// choice does.
func (m Model) resumeDetail() string {
	th, s := *m.theme, m.resume.session
	w := m.paneCols() - paneChrome
	var b strings.Builder
	say := func(style lipgloss.Style, text string) {
		b.WriteString(hang("", text, w, style) + "\n")
	}
	b.WriteString(th.Header.Render("Shut down") + "\n")
	if s.ID != "" {
		say(th.Path, "session "+s.ID+": "+sessionCounts(s))
		say(th.Meta, "\nRestore opens what it recorded, and shows each step as it goes.")
	} else {
		say(th.Path, "No session was saved.")
	}
	say(th.Meta, "Pick another session lists every saved session.")
	say(th.Meta, "Start fresh opens nothing and deletes no session. Esc does the same.")
	return b.String()
}
