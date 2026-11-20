package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/pkg/revier"
)

// A full shutdown or a restore that the popup runs runs under a curtain on
// every monitor, which shows the walk's progress alone (decisions.md D192).
// The walk opens the curtains before its first step, sends them each report,
// and takes back what the user pressed on them as the surface's own keys. A
// walk that went well lifts its curtains a moment after its end; any other
// keeps them up with a summary until the user closes them. A curtain that does
// not open never holds the walk: the walk runs without it.

// curtainLift is how long the curtains show a walk that went well before they
// lift.
const curtainLift = 1500 * time.Millisecond

// curtainOpenWait bounds the opening of the curtains: the walk waits for them,
// and never longer than this.
const curtainOpenWait = 3 * time.Second

// curtainArgv is the launch of the curtain on monitor m, which reads the
// walk at socket; main is the curtain that lists the steps.
type curtainArgv func(m revier.Monitor, socket string, main bool) []string

// WithCurtain gives the surface the launch of a curtain, so a full shutdown
// or a restore runs under one on every monitor (decisions.md D192). Without
// it no curtain opens.
func (m Model) WithCurtain(argv func(m revier.Monitor, socket string, main bool) []string) Model {
	m.curtain = argv
	return m
}

// curtainRun is the curtains of one walk: the socket they read and the state
// it sends. The walk's goroutine writes the reports and the update loop the
// rest, so every field after mu is under it.
type curtainRun struct {
	opened   chan struct{}          // closed once the open has answered
	commands <-chan curtain.Command // the curtains' presses, set before opened closes; nil with no curtain
	shade    sync.Mutex             // held over a hide or a show, so that the two never interleave

	mu    sync.Mutex
	srv   *curtain.Server // nil before the open, after one that failed, and once closed
	state curtain.State
	shown walk    // the walk's reports, for the line the curtain says
	after tea.Cmd // what runs once the curtains of a walk that ended are gone
}

// newCurtain is the curtain run of a walk of that mode over those steps, or
// nil on a surface with no curtain to launch.
func newCurtain(argv curtainArgv, mode string, rows []curtain.Row) *curtainRun {
	if argv == nil {
		return nil
	}
	return &curtainRun{opened: make(chan struct{}), state: curtain.State{Mode: mode, Rows: rows, Of: len(rows)}}
}

// open listens on the curtains' socket and opens a curtain on each monitor.
// One that fails is logged and closed, and the walk goes on without it.
func (r *curtainRun) open(c *core.Core, argv curtainArgv) {
	if r == nil {
		return
	}
	defer close(r.opened)
	srv, err := curtain.Listen()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), curtainOpenWait)
		_, err = c.OpenCurtains(ctx, func(m revier.Monitor) []string { return argv(m, srv.Path(), m.Primary) })
		cancel()
		if err != nil {
			srv.Close()
		}
	}
	if err != nil {
		slog.Warn("curtain: the walk runs without it", "mode", r.state.Mode, "err", err)
		return
	}
	r.commands = srv.Commands()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.srv = srv
	srv.Send(r.state)
}

// plan sets the steps of a walk whose plan was made after the curtains
// opened: a restore's comes from the survey that follows the open.
func (r *curtainRun) plan(rows []curtain.Row) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Rows = rows
	r.count()
	r.send()
}

// take is one report of the walk.
func (r *curtainRun) take(p core.Progress) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shown.take(p)
	r.state.Line = r.shown.now()
	switch p.Phase {
	case core.PhaseClose:
		r.mark(string(p.Closed.Project), p.Closed.Name(), p.Done, closeRow(p.Closed))
	case core.PhaseLaunch:
		r.mark(string(p.Restored.Project), string(p.Restored.Target), p.Done, restoreRow(p.Restored))
	}
	r.count()
	r.send()
}

// mark sets the row of a step: running before it, done as it came to after.
// The walk orders its steps itself, so a step is found by what it is, and
// one not in the plan gets a row of its own.
func (r *curtainRun) mark(project, label string, done bool, came curtain.Row) {
	row := curtain.Row{Project: project, Label: label, State: curtain.RowRunning}
	if done {
		row.State, row.Note = came.State, came.Note
	}
	for i, at := range r.state.Rows {
		if at.Project == project && at.Label == label && (at.State == curtain.RowWaiting || at.State == curtain.RowRunning) {
			r.state.Rows[i] = row
			return
		}
	}
	r.state.Rows = append(r.state.Rows, row)
}

// count is the bar: the steps walked of the steps there are.
func (r *curtainRun) count() {
	r.state.N, r.state.Of = 0, len(r.state.Rows)
	for _, row := range r.state.Rows {
		if row.State != curtain.RowWaiting && row.State != curtain.RowRunning {
			r.state.N++
		}
	}
}

// cancelling says the cancel was taken: the walk stops after its step.
func (r *curtainRun) cancelling() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Cancelling = true
	r.send()
}

// refocus gives the keyboard back to the main curtain after a step that took
// it, unless the user hid the curtains.
func (r *curtainRun) refocus(c *core.Core) {
	if !r.up() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
	defer cancel()
	_ = c.FocusCurtain(ctx)
}

// up reports curtains that opened and are not closed.
func (r *curtainRun) up() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.srv != nil
}

// finish sends the walk's end, and then is after, which runs once the
// curtains are gone. A walk that went well lifts them a moment later; any
// other brings a hidden curtain back for its summary, and the curtains stay
// until the user closes them, every curtain has gone, or the surface quits.
// With no curtain up, after runs at once.
func (r *curtainRun) finish(c *core.Core, end curtain.End, lift time.Duration, after tea.Cmd) tea.Cmd {
	if !r.up() {
		return after
	}
	r.mu.Lock()
	// The bar keeps the steps walked: a step not reached is no step walked.
	r.count()
	for i, row := range r.state.Rows {
		if row.State == curtain.RowWaiting || row.State == curtain.RowRunning {
			r.state.Rows[i].State, r.state.Rows[i].Note = curtain.RowSkipped, "not reached"
		}
	}
	r.state.End, r.state.Line, r.after = &end, "", after
	r.send()
	r.mu.Unlock()
	if end.OK {
		return tea.Tick(lift, func(time.Time) tea.Msg {
			if after := r.lift(); after != nil {
				return after()
			}
			return nil
		})
	}
	return r.show(c)
}

// lift ends the curtains, each exits once its socket ends, and hands back
// what runs once they are gone. The lift of a walk that went well, the
// summary's Close and the last curtain gone can each come; only the first
// gets the after.
func (r *curtainRun) lift() tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil {
		r.srv.Close()
		r.srv = nil
	}
	after := r.after
	r.after = nil
	return after
}

// back is the raise of the surface once its curtains are gone: they took the
// focus from the popup, whose overlay rule hid it then. With no curtain up
// the surface was never covered, and back is nil.
func (r *curtainRun) back(c *core.Core, popup bool) tea.Cmd {
	if !r.up() {
		return nil
	}
	return raiseSelf(c, popup)
}

// hide uncovers the desktop while the walk runs. Once the walk ended it does
// nothing: the summary stays up, as the show of the end brings it back.
func (r *curtainRun) hide(c *core.Core) tea.Cmd {
	return func() tea.Msg {
		r.shade.Lock()
		defer r.shade.Unlock()
		if r.ended() {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		defer cancel()
		_ = c.HideCurtains(ctx)
		return nil
	}
}

// show brings a hidden curtain back for the summary.
func (r *curtainRun) show(c *core.Core) tea.Cmd {
	return func() tea.Msg {
		r.shade.Lock()
		defer r.shade.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		defer cancel()
		_ = c.ShowCurtains(ctx)
		return nil
	}
}

// ended reports a walk whose end the curtains were sent.
func (r *curtainRun) ended() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.End != nil
}

// send hands the state to the curtains, when they are up. The caller holds
// mu.
func (r *curtainRun) send() {
	if r.srv != nil {
		r.srv.Send(r.state)
	}
}

// curtainMsg is one press on a curtain of that run; ok is false once its
// socket is closed.
type curtainMsg struct {
	cmd  curtain.Command
	from *curtainRun
	ok   bool
}

// waitCurtain delivers the next press on the run's curtains to the update
// loop, once they opened.
func waitCurtain(r *curtainRun) tea.Cmd {
	if r == nil {
		return nil
	}
	return func() tea.Msg {
		<-r.opened
		if r.commands == nil {
			return nil
		}
		cmd, ok := <-r.commands
		return curtainMsg{cmd: cmd, from: r, ok: ok}
	}
}

// curtained takes a press on a curtain as the surface's own: cancel is the
// first ctrl+c, quit the second (decisions.md D140). Esc on a curtain hides
// the curtains and leaves the walk running. The summary's Close ends them, as
// does the last curtain gone some other way, closed by the window manager or
// crashed: a run with no curtain left keeps no socket open.
func (m Model) curtained(msg curtainMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	next := waitCurtain(msg.from)
	w := m.runningWalk()
	ours := w != nil && w.curtain == msg.from
	switch msg.cmd {
	case curtain.Cancel:
		if ours && !w.cancelled {
			m.cancelWalk()
		}
	case curtain.Quit:
		if ours {
			return m.quitWalk()
		}
	case curtain.Hide:
		return m, tea.Batch(next, msg.from.hide(m.core))
	case curtain.Close, curtain.Gone:
		return m, msg.from.lift()
	}
	return m, next
}

// closeRows are a shutdown's steps, none walked yet.
func closeRows(plan []core.CloseStep) []curtain.Row {
	rows := make([]curtain.Row, 0, len(plan))
	for _, s := range plan {
		rows = append(rows, curtain.Row{Project: string(s.Project), Label: s.Name(), State: curtain.RowWaiting})
	}
	return rows
}

// restoreRows are a restore's steps, none walked yet.
func restoreRows(plan []core.RestoreStep) []curtain.Row {
	rows := make([]curtain.Row, 0, len(plan))
	for _, s := range plan {
		rows = append(rows, curtain.Row{Project: string(s.Project), Label: string(s.Target), State: curtain.RowWaiting})
	}
	return rows
}

// closeRow is what a close step came to: one left open is skipped, with why.
func closeRow(r core.CloseResult) curtain.Row {
	switch {
	case r.Err != nil:
		return curtain.Row{State: curtain.RowFailed, Note: r.Err.Error()}
	case r.Open || r.Action.Leaves():
		return curtain.Row{State: curtain.RowSkipped, Note: r.Note()}
	}
	return curtain.Row{State: curtain.RowDone}
}

// restoreRow is what a restore step came to: one stepped over is skipped,
// with why.
func restoreRow(r core.RestoreResult) curtain.Row {
	switch {
	case r.Err != nil:
		return curtain.Row{State: curtain.RowFailed, Note: r.Err.Error()}
	case r.Action != core.RestoreLaunch:
		return curtain.Row{State: curtain.RowSkipped, Note: r.Note()}
	}
	return curtain.Row{State: curtain.RowDone, Note: r.Note()}
}

// shutdownEnd is the curtain's end of a full shutdown: closed is what it
// closed, unreached what a cancel left.
func shutdownEnd(closed core.Closed, unreached int, cancelled bool) curtain.End {
	n, open, failed := closed.Counts()
	summary := summarize(fmt.Sprintf("%d closed", n), tally{open, "still open"}, tally{failed, "failed"}, tally{unreached, "not reached"})
	switch {
	case cancelled:
		return curtain.End{Title: "Shutdown cancelled", Summary: summary}
	case open > 0 || failed > 0:
		return curtain.End{Title: "Shutdown finished with problems", Summary: summary}
	}
	return curtain.End{OK: true, Title: "Shutdown done", Summary: summary}
}

// restoreEnd is the curtain's end of a restore. back is the return home's
// failure; one the cancel stopped is the cancel's.
func restoreEnd(msg restoredMsg, cancelled bool) curtain.End {
	opened, pending, failed := msg.restored.Counts()
	summary := summarize(fmt.Sprintf("%d opened", opened), tally{pending, "not up yet"}, tally{failed, "failed"}, tally{msg.notReached, "not reached"})
	switch {
	case cancelled:
		return curtain.End{Title: "Restore cancelled", Summary: summary}
	case failed > 0:
		return curtain.End{Title: "Restore finished with problems", Summary: summary}
	case msg.back != nil:
		return curtain.End{Title: "Restore finished with problems", Summary: summary + " · " + msg.back.Error()}
	}
	return curtain.End{OK: true, Title: "Restore done", Summary: summary}
}

// failedEnd is the end of a walk that stopped on an error before its steps.
func failedEnd(mode string, cancelled bool, err error) curtain.End {
	if cancelled {
		return curtain.End{Title: mode + " cancelled", Summary: err.Error()}
	}
	return curtain.End{Title: mode + " failed", Summary: err.Error()}
}

// tally is a count of steps and what came of them.
type tally struct {
	n    int
	word string
}

// summarize is first, then each tally that is not zero.
func summarize(first string, more ...tally) string {
	parts := []string{first}
	for _, t := range more {
		if t.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", t.n, t.word))
		}
	}
	return strings.Join(parts, " · ")
}
