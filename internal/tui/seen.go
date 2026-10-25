package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/record"
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

// observe is what the surface does with each survey it shows: the survey of
// this machine, or answered, the answers of the linked hosts a round asked.
// It records what the survey saw in the state record (decisions.md D151),
// and keeps it as what the surface last saw (keepSeen). Both are written by
// the command it returns, never in Update.
func (m *Model) observe(answered *core.RemoteAnswers) tea.Cmd {
	return tea.Batch(m.recordSaw(answered), m.keepSeen(answered == nil))
}

// recordSaw is the state record's lines of a survey: what changed since the
// surface's last survey of each scope it saw, which the record works out.
func (m Model) recordSaw(answered *core.RemoteAnswers) tea.Cmd {
	var saw []record.Seen
	if answered == nil {
		saw = []record.Seen{m.core.SeenHere(m.local, m.attached)}
	} else {
		saw = m.core.SeenThere(m.core.Lay(m.local, *answered))
	}
	return func() tea.Msg {
		record.Saw(saw...)
		return nil
	}
}

// keepSeen keeps what the survey saw as what the surface last saw. The first
// survey in which every host here listed checks whether revier ended without
// a shutdown, before anything is written over what was last seen. After that
// it keeps what the survey saw, unless a walk runs, a full shutdown is on
// record, nothing is open, or nothing changed. A survey that holds less than the last one written is
// written only when the next survey of this machine agrees: a terminal
// killed before the surface lists as closed, not as failed, for the moment
// before the surface goes too. surveyed is false for the answers of a linked
// host, which are laid over the same listing and so are no second look at
// it.
func (m *Model) keepSeen(surveyed bool) tea.Cmd {
	if !m.recovered {
		if m.checking || len(m.local.Failed) > 0 {
			return nil
		}
		m.checking = true
		return m.recover()
	}
	if m.walking() || m.onRecord {
		return nil
	}
	s, ok := m.core.Seen(m.local, m.answers, m.seen)
	if !ok || len(s.Projects) == 0 || s.SameAs(m.seen) {
		m.shrink = session.Session{}
		return nil
	}
	if lacks(s, m.seen) && (!surveyed || !s.SameAs(m.shrink)) {
		if surveyed {
			m.shrink = s
		}
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
// the check may have taken the file. A check that failed is asked again at
// the next survey, and nothing is written until one answers: a write would
// replace what the end before left with what is open now.
func (m *Model) takeRecovered(msg recoveredMsg) {
	m.checking = false
	logging.Repeat("unclean end", "unclean end", msg.err)
	if msg.err != nil {
		return
	}
	m.recovered, m.seen = true, session.Session{}
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
