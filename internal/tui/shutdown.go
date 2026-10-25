package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// The shutdown wizard: a full shutdown or one project's, for a project what of
// it, then the plan to confirm. The run saves the session when it changed and
// closes what the plan names, through the core paths `revier shutdown` uses
// (decisions.md D78). It belongs to the sessions screen, whose bar has its
// button; its key opens it from the surface too. A project's shutdown leaves
// the surface up to say what came of it. A full one says it briefly and
// quits, the popup included, so the next start offers the restore (D141).

// shutdownBarKey opens the wizard.
const shutdownBarKey = "alt+q"

// quitAfterShutdown is how long the result of a full shutdown stays on the
// screen before the surface quits. Any key quits sooner.
const quitAfterShutdown = 1500 * time.Millisecond

// shutQuitMsg is the end of that wait.
type shutQuitMsg struct{}

// shutdownWait bounds the survey a plan is made from. The close keeps none
// of it: the recheck, the save and the closes each run on a budget of their
// own, core.RecheckBudget, core.SaveBudget and core.CloseBudget, counted from
// where each starts.
const shutdownWait = 30 * time.Second

type shutStep int

const (
	shutKind shutStep = iota
	shutProject
	shutScope
	shutConfirm
	shutDone
)

// shutdown is the wizard while it is up.
type shutdown struct {
	step shutStep
	row  int
	// whole is a shutdown of every project, all of each: the one the state
	// records (decisions.md D141). A del never sets it.
	whole   bool
	project revier.ProjectName
	scope   core.ShutdownScope
	// planned is whether the plan's survey has answered, and plan is what it
	// found, over the projects it covered - the ones the recheck inside the
	// close surveys again.
	planned  bool
	projects []core.Project
	plan     []core.CloseStep
	running  bool
	walk     walk                // what the close that runs has reported, and its cancel
	record   *app.ShutdownRecord // the record of a whole shutdown that runs
	saved    string              // what the save before the close came to
	closed   core.Closed
	quitting bool   // a full shutdown is done: the surface quits on the next key or the wait
	back     dialog // the screen the wizard was opened from, which Esc returns to

	// one is a close del or alt+del asked for (close.go): of the project,
	// or of its row when pick is set. It starts at the confirm step, or runs
	// with no step at all, and leaves the surface as it was. drop deletes
	// what the configuration holds of it once it is closed.
	one   bool
	pick  core.CloseRow
	label string
	drop  bool
}

// saves reports a close that saves the session first: the wizard's, and del
// on a project. One row closed is no change of the session worth recording,
// and a project about to be deleted is no project a session can restore.
func (s shutdown) saves() bool {
	return !s.one || (s.pick == core.CloseRow{} && !s.drop)
}

// plannedMsg is the plan the confirm step shows, for the choice it was asked
// for.
type plannedMsg struct {
	whole   bool
	project revier.ProjectName
	scope   core.ShutdownScope
	plan    []core.CloseStep
}

// shutdownMsg is a shutdown's answer. recheck is set instead when an agent
// of the plan turned busy after the plan was shown: the plan with its agents
// as they are now, and nothing closed. recorded is a full shutdown the state
// now records, which the surface quits after.
type shutdownMsg struct {
	saved    string
	closed   core.Closed
	recheck  []core.CloseStep
	err      error
	recorded bool
}

var kindRows = []string{"Full shutdown: every project", "Project shutdown: one project"}

var scopeRows = []struct {
	label string
	scope core.ShutdownScope
}{
	{"All: its targets and agents", core.ShutdownAll},
	{"Only agents", core.ShutdownAgents},
	{"Only targets: what holds no agent", core.ShutdownTargets},
}

// openShutdown is the sessions screen's "shutdown" button, and alt+q there
// and on the surface. Esc from its first step goes back to where it was
// opened.
func (m Model) openShutdown() (tea.Model, tea.Cmd) {
	if m.err = m.refuseSave(); m.err != nil {
		return m, nil
	}
	m.toList()
	m.shut = shutdown{back: m.dialog}
	m.dialog = dialogShutdown
	return m, nil
}

// refuseSave refuses a close that saves the session first while a save runs.
// A restore or another close that runs holds every press (lockedKey), so
// none of them gets here.
func (m Model) refuseSave() error {
	if m.sessions.saving {
		return errors.New("a save is still running; shut down once it is done")
	}
	return nil
}

// openProjects are the projects with something open, in the surface's order:
// what a project shutdown can close.
func (m Model) openProjects() []revier.ProjectName {
	var out []revier.ProjectName
	for _, v := range m.views {
		if anyOpen(v.Targets) {
			out = append(out, v.Project.Name)
		}
	}
	return out
}

// anyOpen reports a target among them that is open.
func anyOpen(targets []revier.TargetView) bool {
	for _, tv := range targets {
		if !tv.Ref.IsZero() {
			return true
		}
	}
	return false
}

// shutRows is how many rows the step offers.
func (m Model) shutRows() int {
	switch m.shut.step {
	case shutKind:
		return len(kindRows)
	case shutProject:
		return len(m.openProjects())
	case shutScope:
		return len(scopeRows)
	case shutConfirm:
		if m.shut.planned && len(m.shut.plan) > 0 {
			return 2
		}
	}
	return 0
}

// shutdownKey is every press in the wizard. A shutdown that runs never gets
// here: the surface holds every press but its cancel (lockedKey).
func (m Model) shutdownKey(msg keyPress) (tea.Model, tea.Cmd) {
	s := &m.shut
	if s.quitting {
		return m, tea.Quit
	}
	m.err = nil
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.shutBack()
	case key.Matches(msg, m.keys.Up):
		s.row = max(s.row-1, 0)
	case key.Matches(msg, m.keys.Down):
		s.row = min(s.row+1, max(m.shutRows()-1, 0))
	case key.Matches(msg, m.keys.Enter):
		return m.shutEnter()
	}
	return m, nil
}

// shutBack is Esc: one step back, and from the first step or the result back
// to the screen the wizard was opened from. The sessions screen is read
// again: the shutdown may have saved one.
func (m *Model) shutBack() {
	s := &m.shut
	if s.one {
		m.dialog, *s = dialogNone, shutdown{}
		return
	}
	switch s.step {
	case shutKind, shutDone:
		m.dialog = s.back
		if m.dialog == dialogSessions {
			m.err = m.sessions.open(m.stateRoot, *m.theme, "")
		}
	case shutProject:
		*s = shutdown{step: shutKind, row: 1, back: s.back}
	case shutScope:
		s.step, s.row = shutProject, indexOf(m.openProjects(), s.project)
	case shutConfirm:
		if s.whole {
			*s = shutdown{step: shutKind, back: s.back}
			return
		}
		s.step, s.row, s.planned, s.plan = shutScope, int(s.scope), false, nil
	}
}

func indexOf(names []revier.ProjectName, name revier.ProjectName) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return 0
}

// shutEnter takes the row under the cursor.
func (m Model) shutEnter() (tea.Model, tea.Cmd) {
	s := &m.shut
	switch s.step {
	case shutKind:
		if s.row == 0 {
			s.whole, s.scope = true, core.ShutdownAll
			return m.shutPlan()
		}
		s.step, s.row = shutProject, 0
	case shutProject:
		// A survey since the last press may have closed a project and shortened
		// the rows under the cursor.
		names := m.openProjects()
		if s.row >= len(names) {
			s.row = max(len(names)-1, 0)
			return m, nil
		}
		s.project, s.step, s.row = names[s.row], shutScope, 0
	case shutScope:
		s.scope = scopeRows[s.row].scope
		return m.shutPlan()
	case shutConfirm:
		if m.shutRows() == 0 {
			return m, nil
		}
		if s.row == 1 {
			m.shutBack()
			return m, nil
		}
		return m.shutRun(true)
	}
	return m, nil
}

// shutPlan is the confirm step: a survey of what is open now, attachments
// included, and the plan made from it.
func (m Model) shutPlan() (tea.Model, tea.Cmd) {
	s := &m.shut
	s.step, s.row, s.planned, s.plan = shutConfirm, 0, false, nil
	c, projects := m.core, m.projects
	s.projects = projects
	asked := plannedMsg{whole: s.whole, project: s.project, scope: s.scope}
	return m, func() tea.Msg {
		asked.plan = surveyPlan(c, projects, asked.project, func(r core.Report) []core.CloseStep {
			return c.ShutdownPlan(r, asked.project, asked.scope)
		})
		return asked
	}
}

// surveyPlan surveys what is open now, attachments included, and makes the
// plan from what it found. Of the linked hosts it waits only for the ones the
// close is about (core.SurveyToClose). It runs off the update loop.
func surveyPlan(c *core.Core, projects []core.Project, only revier.ProjectName, plan func(core.Report) []core.CloseStep) []core.CloseStep {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	return plan(c.SurveyToClose(ctx, projects, only))
}

// planned takes the plan's survey. An answer for a wizard that has left the
// confirm step, or for a choice made before the one it shows, is dropped.
func (m Model) planned(msg plannedMsg) (tea.Model, tea.Cmd) {
	s := m.shut
	if m.dialog != dialogShutdown || s.step != shutConfirm || s.planned ||
		msg.whole != s.whole || msg.project != s.project || msg.scope != s.scope {
		return m, nil
	}
	m.shut.planned, m.shut.plan = true, msg.plan
	return m, nil
}

// shutRun saves the session when it changed and closes what the plan names.
// Confirming a plan that already names a busy agent is the TUI's --force
// (decisions.md D78); every other close is refused by core.Shutdown when one
// of the plan's agents turned busy since the plan was drawn, and the wizard
// shows the plan again with the agents that refused it.
//
// Only the plan the wizard drew goes in. Every close, the forced one too,
// reads the agents again inside core.Shutdown, and the order and the session
// it saves come off that reading rather than off the listing the plan was
// drawn from.
func (m Model) shutRun(confirmed bool) (tea.Model, tea.Cmd) {
	s := &m.shut
	s.running = true
	c, root, projects, plan, saves, full := m.core, m.stateRoot, s.projects, s.plan, s.saves(), s.whole
	reason := session.ReasonProjectClosed
	if s.whole {
		reason = session.ReasonShutdown
	}
	force := confirmed && len(core.Busy(plan)) > 0
	// The recheck, the save, a report before and after each step, and the
	// wait.
	w, progress, cancelled := startWalk(core.PhaseRecheck, 3+2*len(plan))
	s.walk = w
	s.record = nil
	if full {
		s.record = app.NewShutdownRecord(c)
	}
	record := s.record
	closing := func() tea.Msg {
		defer close(progress)
		defer w.cancel()
		opts := core.ShutdownOpts{Force: force, Projects: projects, Progress: func(p core.Progress) { progress <- p }}
		note := ""
		var save func(context.Context, core.Report) (session.Session, error)
		if saves {
			// The save is skipped when the recheck left nothing to close, so
			// the result says why rather than showing a blank line.
			note = "nothing closed, so nothing was saved"
			// The save runs after the busy guard, so a close it refuses
			// leaves no session file behind either, and it records the survey
			// the close works from: a window closed by hand while the confirm
			// was on screen is not saved and reopened by a later restore.
			save = func(saving context.Context, now core.Report) (session.Session, error) {
				stored, saved, _, err := app.SaveChanged(saving, c, root, now, reason, time.Now())
				if err != nil {
					return session.Session{}, err
				}
				note = "nothing open to save"
				switch {
				case saved:
					note = "saved as session " + stored.ID
				case stored.ID != "":
					note = "session " + stored.ID + " already holds what was open"
				}
				return stored, nil
			}
		}
		// No deadline over the close: each phase bounds its own, and once a
		// deadline laid over the cancel passed, a ctrl+c would no longer
		// reach the walk.
		answer := closeAnswer(app.Shutdown(cancelled, c, plan, core.CloseWait, opts, save, record))
		if answer.err == nil && answer.recheck == nil {
			answer.saved, answer.recorded = note, record != nil && record.Recorded()
		}
		return answer
	}
	// The close goes first in the batch: run in order, as the tests run a
	// batch, the wait would otherwise block on a channel nothing writes yet.
	return m, tea.Batch(closing, waitProgress(progress))
}

// closeAnswer is what a close came to, as the wizard reads it: the refusal
// carries the rechecked plan, so the surface shows it rather than an error
// alone.
func closeAnswer(closed core.Closed, err error) shutdownMsg {
	var refused *core.BusyRefusal
	switch {
	case errors.As(err, &refused):
		return shutdownMsg{recheck: refused.Plan}
	case err != nil:
		return shutdownMsg{err: fmt.Errorf("%w; nothing closed", err)}
	}
	return shutdownMsg{closed: closed}
}

// shutDown takes a shutdown's answer: the result is the pane's and the
// result screen's, a step that did not close included. The timer's next survey shows what
// closed; starting one here would add a second survey-tick chain that never
// ends, as it would for actedMsg.
func (m Model) shutDown(msg shutdownMsg) (tea.Model, tea.Cmd) {
	s := &m.shut
	s.running = false
	// A cancel before the first close fails the save on the context it
	// ended; the user's word for that is the cancel. Any other failure is
	// said as it came.
	if s.walk.cancelled && errors.Is(msg.err, context.Canceled) {
		msg.err = errors.New("cancelled; nothing closed")
	}
	if msg.recheck != nil {
		m.err = errors.New("an agent turned busy since the plan was shown; nothing closed")
		// The cursor lands on Cancel: the close it refused is one keypress
		// away, and that press is the force, not a second Enter nobody aimed.
		// A del that closed at once asks here instead, so its second press
		// is the force too. That force surveys again like any other close,
		// so it carries no listing from here.
		s.plan, s.row, s.step = msg.recheck, 1, shutConfirm
		m.dialog = dialogShutdown
		return m, nil
	}
	if s.one {
		return m.closed(msg)
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	s.step, s.row, s.saved, s.closed = shutDone, 0, msg.saved, msg.closed
	// A cancel that came after the last step was walked stopped nothing: the
	// result is that of a shutdown nobody cancelled, and a full one quits.
	if len(s.unreached()) == 0 {
		s.walk.cancelled = false
	}
	// A full shutdown leaves nothing to come back to: the surface says what
	// it did and quits, the popup too, so the next start is a cold one that
	// offers the restore (decisions.md D141).
	if msg.recorded && !s.walk.cancelled {
		s.quitting = true
		return m, tea.Tick(quitAfterShutdown, func(time.Time) tea.Msg { return shutQuitMsg{} })
	}
	return m, nil
}

// shutQuit is the end of the wait after a full shutdown.
func (m Model) shutQuit() (tea.Model, tea.Cmd) {
	if !m.shut.quitting {
		return m, nil
	}
	return m, tea.Quit
}

// notReached says how much of a cancelled close was left open.
func (s shutdown) notReached() string {
	return "cancelled: " + core.Count(len(s.unreached()), "step") + " not reached"
}

// unreached are the steps of the plan a close never walked, which a cancel
// leaves open. The close walks the steps in an order of its own, so a step
// is found by what it closes, not by its place.
func (s shutdown) unreached() []core.CloseStep {
	type closes struct {
		project revier.ProjectName
		target  revier.TargetName
		host    string
		id      string
		panel   revier.PanelID
	}
	what := func(c core.CloseStep) closes {
		return closes{c.Project, c.Target, c.Ref.Host, c.Ref.ID, c.Panel}
	}
	walked := make(map[closes]bool, len(s.closed))
	for _, r := range s.closed {
		walked[what(r.CloseStep)] = true
	}
	var out []core.CloseStep
	for _, step := range s.plan {
		if !walked[what(step)] {
			out = append(out, step)
		}
	}
	return out
}

// shutdownTitle is the question the step asks, on the line over the rule.
func (m Model) shutdownTitle() string {
	s := m.shut
	switch {
	case s.running:
		return "Shutting down: wait until it ends"
	case s.one:
		return m.closeTitle()
	}
	switch s.step {
	case shutKind:
		return "What to shut down?"
	case shutProject:
		return "Which project?"
	case shutScope:
		return "What of " + string(s.project) + "?"
	case shutDone:
		return "Shut down"
	}
	if s.whole {
		return "Shut down every project?"
	}
	return "Shut down " + scopeRows[s.scope].label + " of " + string(s.project) + "?"
}

// shutdownScreen is the wizard's rows, in the list's place.
func (m Model) shutdownScreen() string {
	th, s, w := *m.theme, m.shut, m.listWidth()
	say := func(st lipgloss.Style, text string) string {
		return st.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	var rows []string
	switch s.step {
	case shutKind:
		rows = kindRows
	case shutProject:
		for _, n := range m.openProjects() {
			rows = append(rows, string(n))
		}
		if len(rows) == 0 {
			return say(th.NameDim, "No project has anything open.")
		}
	case shutScope:
		for _, r := range scopeRows {
			rows = append(rows, r.label)
		}
	case shutConfirm:
		switch {
		case s.running:
			return say(th.Meta, s.walk.line())
		case !s.planned:
			return say(th.Meta, "surveying")
		case len(s.plan) == 0:
			return say(th.NameDim, "Nothing to close.")
		}
		run := "Shut down"
		if s.one {
			run = m.closeVerb()
		}
		if busy := core.Busy(s.plan); len(busy) > 0 {
			run += " anyway: " + core.Count(busyAgents(busy), "busy agent")
		}
		rows = []string{run, "Cancel"}
	case shutDone:
		n, open, failed := s.closed.Counts()
		if s.walk.cancelled {
			open += len(s.unreached())
		}
		result := fmt.Sprintf("closed %d, %d still open", n, open)
		if open == 0 && failed == 0 {
			result = fmt.Sprintf("Shutdown complete: closed %d. It is safe to leave.", n)
		}
		done := say(th.Running, result) + "\n" + say(th.Meta, s.saved)
		// What did not close, and what a cancel left, are said on the
		// result's own rows, so the footer keeps the result's keys.
		if failed > 0 {
			done += "\n" + say(th.Attention, core.Count(failed, "step")+" did not close")
		}
		if s.walk.cancelled {
			done += "\n" + say(th.Attention, s.notReached())
		}
		if s.quitting {
			done += "\n\n" + say(th.NameDim, "revier quits; the next start offers the restore")
		}
		return done
	}
	return choiceRows(th, rows, s.row, w)
}

func busyAgents(steps []core.CloseStep) int {
	n := 0
	for _, s := range steps {
		n += len(s.BusyAgents())
	}
	return n
}

// shutdownDetail is the pane: the plan to confirm, or what the shutdown came
// to, one row a step, grouped by project.
func (m Model) shutdownDetail() string {
	th, s := m.spun(), m.shut
	w := m.paneCols() - paneChrome
	var b strings.Builder
	// Until the wait after the closes has confirmed them, a step the walk
	// asked to close is waited for, not closed.
	closedRows := func(closed core.Closed, confirmed bool) revier.ProjectName {
		var project revier.ProjectName
		for _, r := range closed {
			project = m.shutProjectLine(&b, project, r.Project, w)
			op, rest := startOp(th, "closed"), ""
			switch {
			case r.Err != nil || r.Open || r.Action.Leaves():
				op, rest = skipOp(th, "open"), r.Note()
			case !confirmed:
				op = keepOp(th, "waiting")
			}
			b.WriteString(planRow(th, op, th.ProjectName.Render(r.Name()), "", rest, th.PathMissing, w) + "\n")
		}
		return project
	}
	switch {
	case s.running:
		// The steps done, then the one that runs: a host that hangs on a
		// close is the step left on the screen.
		b.WriteString(th.Header.Render("Closing") + "\n")
		project := closedRows(s.walk.closed, false)
		left := len(s.plan) - len(s.walk.closed)
		p := s.walk.last
		if p.Phase == core.PhaseClose && !p.Done {
			m.shutProjectLine(&b, project, p.Closed.Project, w)
			b.WriteString(planRow(th, keepOp(th, "closing"), th.ProjectName.Render(p.Label), "", "", th.Path, w) + "\n")
			left--
		}
		switch {
		case p.Phase == core.PhaseWait:
			b.WriteString(hang("", "\n"+s.walk.waiting(), w, th.Meta) + "\n")
		case left > 0:
			b.WriteString(hang("", "\n"+core.Count(left, "step")+" to go", w, th.Meta) + "\n")
		}
	case s.step == shutDone:
		b.WriteString(th.Header.Render("Closed") + "\n")
		project := closedRows(s.closed, true)
		if !s.walk.cancelled {
			break
		}
		for _, step := range s.unreached() {
			project = m.shutProjectLine(&b, project, step.Project, w)
			b.WriteString(planRow(th, skipOp(th, "open"), th.ProjectName.Render(step.Name()), "", "not reached", th.PathMissing, w) + "\n")
		}
	case s.step == shutConfirm && s.planned:
		header := "Shutdown plan"
		if s.one {
			header = "Close plan"
		}
		b.WriteString(th.Header.Render(header) + "\n")
		if len(s.plan) == 0 {
			b.WriteString(hang("", "nothing is open", w, th.Meta) + "\n")
		}
		var project revier.ProjectName
		for _, step := range s.plan {
			project = m.shutProjectLine(&b, project, step.Project, w)
			op, rest := skipOp(th, "close"), ""
			// A plan the busy guard handed back carries the steps its survey
			// could not read, and those close nothing: the row says so rather
			// than promising a close (decisions.md D99).
			switch {
			case step.Unread != "":
				op, rest = keepOp(th, "keep"), step.Unread
			case step.Action == core.CloseUnsupported:
				op, rest = keepOp(th, "keep"), "its host cannot close it"
			}
			b.WriteString(planRow(th, op, th.ProjectName.Render(step.Name()), "", rest, th.PathMissing, w) + "\n")
			for _, a := range step.Agents {
				state := statusStyle(th, a.State.Status).Render(statusLabel(th, a.State.Status))
				b.WriteString(planRow(th, "", th.ProjectName.Render(a.State.Harness), state, a.State.Activity, th.Path, w) + "\n")
			}
		}
		switch {
		case s.saves():
			b.WriteString(hang("", "\nthe session is saved first when it changed; "+config.ContractHome(session.Dir(m.stateRoot)), w, th.Meta) + "\n")
		case s.drop:
			b.WriteString(hang("", "\nthen "+m.dropNote(s.project, s.pick.Target), w, th.Meta) + "\n")
		}
	default:
		b.WriteString(th.Header.Render("Shutdown") + "\n")
		b.WriteString(hang("", "Saves the session when it changed, then closes what you choose. Esc goes back a step.", w, th.Meta) + "\n")
	}
	return b.String()
}

// shutProjectLine writes a project's name over its first step.
func (m Model) shutProjectLine(b *strings.Builder, last, project revier.ProjectName, w int) revier.ProjectName {
	if project != last {
		if last != "" {
			b.WriteString("\n")
		}
		b.WriteString(m.theme.ProjectName.Bold(true).Render(clipTo(string(project), w)) + "\n")
	}
	return project
}
