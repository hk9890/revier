package tui

import (
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// What the surface last saw: while it runs, it keeps what its surveys find
// open as one session file outside the saved ones, so an end with no
// shutdown - a killed terminal, a power loss - still leaves a restore point
// (decisions.md D150).

// recoveredMsg is the check for an end with no shutdown, answered: the
// session to offer when ok.
type recoveredMsg struct {
	offer session.Session
	ok    bool
	err   error
}

// observe is what the surface does with each survey it shows. The first one
// in which every host here listed checks whether revier ended without a
// shutdown, before anything is written over what was last seen. After that it
// keeps what the survey saw, unless a walk runs, nothing is open, or nothing
// changed. A survey that holds less than the last one written is written
// only when the next survey agrees: a terminal killed before the surface
// lists as closed, not as failed, for the moment before the surface goes too.
//
// The file is written by the command it returns, never in Update.
func (m *Model) observe() tea.Cmd {
	if !m.recovered {
		if m.checking || len(m.local.Failed) > 0 {
			return nil
		}
		m.checking = true
		return m.recover()
	}
	if m.walking() {
		return nil
	}
	s, ok := m.core.Seen(m.local, m.answers, m.seen)
	if !ok || len(s.Projects) == 0 || s.SameAs(m.seen) {
		m.shrink = session.Session{}
		return nil
	}
	if lacks(s, m.seen) && !s.SameAs(m.shrink) {
		m.shrink = s
		return nil
	}
	m.shrink = session.Session{}
	s.At = m.now()
	m.seen = s
	root := m.stateRoot
	return func() tea.Msg {
		err := app.SaveSeen(root, s)
		logging.Repeat("session seen", "session seen", err, "path", session.SeenPath(root))
		return nil
	}
}

// recover asks whether revier ended without a shutdown, on the survey the
// surface has.
func (m Model) recover() tea.Cmd {
	c, root, r := m.core, m.stateRoot, m.local
	return func() tea.Msg {
		offer, ok, err := app.Recover(c, root, r)
		return recoveredMsg{offer: offer, ok: ok, err: err}
	}
}

// takeRecovered takes the check's answer: what was last seen is written from
// now on, and an end that lost something is offered on the start dialog
// over the plain surface. What was last seen is written again whole, since
// the check may have taken the file.
func (m *Model) takeRecovered(msg recoveredMsg) {
	m.checking, m.recovered, m.seen = false, true, session.Session{}
	if msg.err != nil {
		slog.Warn("unclean end", "err", msg.err)
	}
	if msg.ok && m.plain() {
		m.openUnclean(msg.offer)
	}
}

// lacks reports a target that was holds and s does not.
func lacks(s, was session.Session) bool {
	type at struct {
		project revier.ProjectName
		target  revier.TargetName
	}
	has := map[at]bool{}
	for _, p := range s.Projects {
		for _, t := range p.Targets {
			has[at{p.Name, t.Name}] = true
		}
	}
	for _, p := range was.Projects {
		for _, t := range p.Targets {
			if !has[at{p.Name, t.Name}] {
				return true
			}
		}
	}
	return false
}
