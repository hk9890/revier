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
// click but ctrl+c: the first cancels the walk once the step that runs is
// done, or ends a shutdown's wait after its last step, and leaves the surface
// up to say what it did; a second quits, for a step that hangs (decisions.md
// D140).

// walk is what a running shutdown or restore has reported so far, and how to
// stop it.
type walk struct {
	progress  <-chan core.Progress
	cancel    context.CancelFunc
	cancelled bool
	last      core.Progress // the latest report
	closed    core.Closed   // the close steps done
	restored  core.Restored // the restore steps done
	curtain   *curtainRun   // the curtains the walk runs under, nil for none (decisions.md D192)
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
	switch {
	case w.cancelled && w.last.Phase == core.PhaseWait:
		return "no longer waiting for what was closed… ctrl+c again quits"
	case w.cancelled:
		return "cancelling once the step that runs is done… ctrl+c again quits"
	}
	return w.now() + "… ctrl+c cancels"
}

// now says what the walk does now.
func (w walk) now() string {
	p := w.last
	switch {
	case p.Phase == core.PhaseRecheck:
		return "reading the agents again"
	case p.Phase == core.PhaseSave:
		return "saving the session when it changed"
	case p.Phase == core.PhaseWait:
		return w.waiting()
	case p.Phase == core.PhaseSurvey:
		return "surveying what is open now"
	case p.Phase == core.PhaseHome:
		return "returning to " + p.Label
	case p.Done:
		return fmt.Sprintf("%d of %d done", p.N, p.Of)
	case p.Phase == core.PhaseClose:
		return fmt.Sprintf("closing %d of %d: %s", p.N, p.Of, p.Label)
	}
	return fmt.Sprintf("opening %d of %d: %s", p.N, p.Of, p.Label)
}

// waiting is the wait after the closes, which waits for each step the walk
// asked to close and that did not fail. With no such step, it waits for
// nothing, as `revier shutdown` says no wait.
func (w walk) waiting() string {
	if asked, _, _ := w.closed.Counts(); asked > 0 {
		return fmt.Sprintf("waiting for %d to close", asked)
	}
	return "nothing to wait for"
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

// lockedKey is a press while a walk runs: ctrl+c cancels the walk, a second
// quits, and every other press does nothing.
func (m Model) lockedKey(msg keyPress) (tea.Model, tea.Cmd) {
	if !key.Matches(msg, m.keys.Quit) {
		return m, nil
	}
	if m.runningWalk().cancelled {
		return m.quitWalk()
	}
	m.cancelWalk()
	return m, nil
}

// runningWalk is the walk that runs, nil with none.
func (m *Model) runningWalk() *walk {
	switch {
	case m.shut.running:
		return &m.shut.walk
	case m.sessions.restoring != "":
		return &m.sessions.walk
	}
	return nil
}

// cancelWalk is the first ctrl+c: the walk stops once the step that runs is
// done.
func (m *Model) cancelWalk() {
	w := m.runningWalk()
	w.cancel()
	w.cancelled = true
	w.curtain.cancelling()
}

// quitWalk is the second ctrl+c: the surface quits with the walk still
// running.
//
// The surface quits before the walk returns, so the hung step's command is
// ended here, and the close's record is abandoned here, and the record
// decides whether it is taken back (decisions.md D141). The record of a close
// that is not full returns at once. The command is ended first: the abandon
// of a full shutdown's record can wait on the state's lock.
func (m Model) quitWalk() (tea.Model, tea.Cmd) {
	if m.endHung != nil {
		m.endHung()
	}
	if s := m.shut; s.running && s.record != nil {
		s.record.Abandon()
	}
	return m, m.quit()
}
