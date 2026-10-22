package tui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/core"
)

// A shutdown and a restore report each step as they walk it, and the surface
// draws the walk as it goes. While one runs, the surface takes no key and no
// click but ctrl+c, which cancels it and leaves the surface up to say what it
// did (decisions.md D140).

// walk is what a running shutdown or restore has reported so far, and how to
// stop it.
type walk struct {
	progress  <-chan core.Progress
	cancel    context.CancelFunc
	cancelled bool
	last      core.Progress // the latest report
	closed    core.Closed   // the close steps done
	restored  core.Restored // the restore steps done
}

// startWalk is a walk with its channel and its cancel, in its first phase
// until it reports. The channel holds every report the walk can make, so the
// core never waits on the surface to read one.
func startWalk(first core.Phase, reports int) (walk, chan core.Progress, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	progress := make(chan core.Progress, reports)
	return walk{progress: progress, cancel: cancel, last: core.Progress{Phase: first}}, progress, ctx
}

// take is one report.
func (w *walk) take(p core.Progress) {
	w.last = p
	if !p.Done {
		return
	}
	switch p.Phase {
	case core.PhaseClose:
		w.closed = append(w.closed, p.Closed)
	case core.PhaseLaunch:
		w.restored = append(w.restored, p.Restored)
	}
}

// line says what the walk does now, and how to stop it.
func (w walk) line() string {
	p := w.last
	now := ""
	switch {
	case w.cancelled:
		return "cancelling once the step that runs is done…"
	case p.Phase == core.PhaseRecheck:
		now = "reading the agents again"
	case p.Phase == core.PhaseSave:
		now = "saving the session when it changed"
	case p.Phase == core.PhaseWait:
		now = "waiting for what closed to go"
	case p.Phase == core.PhaseSurvey:
		now = "surveying what is open now"
	case p.Phase == core.PhaseHome:
		now = "returning to " + p.Label
	case p.Done:
		now = fmt.Sprintf("%d of %d done", p.N, p.Of)
	case p.Phase == core.PhaseClose:
		now = fmt.Sprintf("closing %d of %d: %s", p.N, p.Of, p.Label)
	default:
		now = fmt.Sprintf("opening %d of %d: %s", p.N, p.Of, p.Label)
	}
	return now + "… ctrl+c cancels"
}

// progressMsg is one report of a walk, on the channel it came on, so the next
// wait reads the same walk. ok is false once the walk has ended.
type progressMsg struct {
	progress core.Progress
	from     <-chan core.Progress
	ok       bool
}

// waitProgress delivers the next report of a walk to the update loop.
func waitProgress(from <-chan core.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-from
		return progressMsg{progress: p, from: from, ok: ok}
	}
}

// progressed takes a report of the walk that runs, and waits for the next. A
// report of a walk that has ended is dropped: its answer carries every step.
func (m Model) progressed(msg progressMsg) (tea.Model, tea.Cmd) {
	var w *walk
	switch {
	case !msg.ok:
		return m, nil
	case m.shut.running && m.shut.walk.progress == msg.from:
		w = &m.shut.walk
	case m.sessions.restoring != "" && m.sessions.walk.progress == msg.from:
		w = &m.sessions.walk
	default:
		return m, nil
	}
	w.take(msg.progress)
	return m, waitProgress(msg.from)
}

// walking reports a shutdown or a restore that runs: it holds the input.
func (m Model) walking() bool {
	return m.shut.running || m.sessions.restoring != ""
}

// lockedKey is a press while a walk runs: ctrl+c cancels the walk, and every
// other press does nothing.
func (m Model) lockedKey(msg keyPress) (tea.Model, tea.Cmd) {
	if !key.Matches(msg, m.keys.Quit) {
		return m, nil
	}
	w := &m.sessions.walk
	if m.shut.running {
		w = &m.shut.walk
	}
	w.cancel()
	w.cancelled = true
	return m, nil
}
