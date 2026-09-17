package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// The shutdown wizard: a full shutdown or one project's, for a project what of
// it, then the plan to confirm. The run saves the session when it changed and
// closes what the plan names, through the core paths `revier shutdown` uses
// (decisions.md D78). The surface stays up and shows what came of it.

// shutdownBarKey opens the wizard.
const shutdownBarKey = "alt+q"

// shutdownTimeout bounds the survey a plan is made from, and the save before
// the close.
const shutdownTimeout = 30 * time.Second

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
	step    shutStep
	row     int
	whole   bool
	project revier.ProjectName
	scope   core.ShutdownScope
	// planned is whether the plan's survey has answered; report and plan are
	// what it found.
	planned bool
	report  core.Report
	plan    []core.CloseStep
	running bool
	saved   string // what the save before the close came to
	closed  core.Closed
}

// plannedMsg is the survey and plan the confirm step shows, for the choice
// it was asked for.
type plannedMsg struct {
	whole   bool
	project revier.ProjectName
	scope   core.ShutdownScope
	report  core.Report
	plan    []core.CloseStep
	err     error
}

// shutdownMsg is a shutdown's answer.
type shutdownMsg struct {
	saved  string
	closed core.Closed
	err    error
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

// openShutdown is the "shutdown" button and alt+q.
func (m Model) openShutdown() (tea.Model, tea.Cmd) {
	m.err = nil
	if m.saving {
		m.err = errors.New("a save is still running; shut down once it is done")
		return m, nil
	}
	// The save before the close would record a desktop half restored, and the
	// restore would open again what the shutdown closes.
	if m.restoring != "" {
		m.err = fmt.Errorf("the restore of %s is still running; shut down once it is done", m.restoring)
		return m, nil
	}
	m.toList()
	m.dialog = dialogShutdown
	m.shut = shutdown{}
	return m, nil
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

// shutdownKey is every press in the wizard. A shutdown that runs takes no
// press but quit: its answer is what the wizard shows next.
func (m Model) shutdownKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	s := &m.shut
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case s.running:
		return m, nil
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
// to the surface.
func (m *Model) shutBack() {
	s := &m.shut
	switch s.step {
	case shutKind, shutDone:
		m.dialog = dialogNone
	case shutProject:
		*s = shutdown{step: shutKind, row: 1}
	case shutScope:
		s.step, s.row = shutProject, indexOf(m.openProjects(), s.project)
	case shutConfirm:
		if s.whole {
			*s = shutdown{step: shutKind}
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
		return m.shutRun()
	}
	return m, nil
}

// shutPlan is the confirm step: a survey of what is open now, attachments
// included, and the plan made from it.
func (m Model) shutPlan() (tea.Model, tea.Cmd) {
	s := &m.shut
	s.step, s.row, s.planned, s.plan = shutConfirm, 0, false, nil
	c, projects, root := m.core, m.projects, m.stateRoot
	asked := plannedMsg{whole: s.whole, project: s.project, scope: s.scope}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		st := loadedState(root)
		report, err := c.Survey(ctx, projects, st.Bound, st.Attached)
		if err != nil {
			asked.err = err
			return asked
		}
		asked.report, asked.plan = report, c.ShutdownPlan(report, asked.project, asked.scope)
		return asked
	}
}

// planned takes the plan's survey. An answer for a wizard that has left the
// confirm step, or for a choice made before the one it shows, is dropped.
func (m Model) planned(msg plannedMsg) (tea.Model, tea.Cmd) {
	s := m.shut
	if m.dialog != dialogShutdown || s.step != shutConfirm || s.planned ||
		msg.whole != s.whole || msg.project != s.project || msg.scope != s.scope {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		m.shutBack()
		return m, nil
	}
	m.shut.planned, m.shut.report, m.shut.plan = true, msg.report, msg.plan
	return m, nil
}

// shutRun saves the session when it changed and closes what the plan names.
// Confirming a plan with a busy agent is the TUI's --force.
func (m Model) shutRun() (tea.Model, tea.Cmd) {
	s := &m.shut
	s.running = true
	c, root, report, plan := m.core, m.stateRoot, s.report, s.plan
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		st := loadedState(root)
		stored, saved, _, err := c.SaveChanged(ctx, root, report, st.Current, time.Now())
		if err != nil {
			return shutdownMsg{err: fmt.Errorf("%w; nothing closed", err)}
		}
		note := "nothing open to save"
		switch {
		case saved:
			note = "saved as session " + stored.ID
		case stored.ID != "":
			note = "session " + stored.ID + " already holds what was open"
		}
		plan = core.CloseLast(plan, report.Instances, core.RunsUnder())
		closing, stop := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stop()
		return shutdownMsg{saved: note, closed: c.Shutdown(closing, plan, core.CloseWait)}
	}
}

// shutDown takes a shutdown's answer: the result is the pane's, a step that
// did not close is the footer's too.
func (m Model) shutDown(msg shutdownMsg) (tea.Model, tea.Cmd) {
	s := &m.shut
	s.running = false
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	s.step, s.row, s.saved, s.closed = shutDone, 0, msg.saved, msg.closed
	if _, _, failed := msg.closed.Counts(); failed > 0 {
		m.err = fmt.Errorf("%s did not close", core.Count(failed, "step"))
	}
	return m, m.Survey()
}

// shutdownTitle is the question the step asks, on the line over the rule.
func (m Model) shutdownTitle() string {
	s := m.shut
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
	th, s, w := m.theme, m.shut, m.listWidth()
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
			return say(th.Meta, "shutting down…")
		case !s.planned:
			return say(th.Meta, "surveying")
		case len(s.plan) == 0:
			return say(th.NameDim, "Nothing to close.")
		}
		run := "Shut down"
		if busy := core.Busy(s.plan); len(busy) > 0 {
			run = "Shut down anyway: " + core.Count(busyAgents(busy), "busy agent")
		}
		rows = []string{run, "Cancel"}
	case shutDone:
		n, open, _ := s.closed.Counts()
		return say(th.Running, fmt.Sprintf("closed %d, %d still open", n, open)) + "\n" +
			say(th.Meta, s.saved)
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
	switch {
	case s.step == shutDone:
		b.WriteString(th.Header.Render("Closed") + "\n")
		var project revier.ProjectName
		for _, r := range s.closed {
			project = m.shutProjectLine(&b, project, r.Project, w)
			op, rest := startOp(th, "closed"), ""
			if r.Err != nil || r.Open || r.Action == core.CloseUnsupported {
				op, rest = skipOp(th, "open"), r.Note()
			}
			b.WriteString(planRow(th, op, th.ProjectName.Render(r.Name()), "", rest, th.PathMissing, w) + "\n")
		}
	case s.step == shutConfirm && s.planned:
		b.WriteString(th.Header.Render("Shutdown plan") + "\n")
		if len(s.plan) == 0 {
			b.WriteString(hang("", "nothing is open", w, th.Meta) + "\n")
		}
		var project revier.ProjectName
		for _, step := range s.plan {
			project = m.shutProjectLine(&b, project, step.Project, w)
			op, rest := skipOp(th, "close"), ""
			if step.Action == core.CloseUnsupported {
				op, rest = keepOp(th, "keep"), "its host cannot close it"
			}
			b.WriteString(planRow(th, op, th.ProjectName.Render(step.Name()), "", rest, th.PathMissing, w) + "\n")
			for _, a := range step.Agents {
				state := statusStyle(th, a.State.Status).Render(statusLabel(th, a.State.Status))
				b.WriteString(planRow(th, "", th.ProjectName.Render(a.State.Harness), state, a.State.Activity, th.Path, w) + "\n")
			}
		}
		b.WriteString(hang("", "\nthe session is saved first when it changed; "+contractHome(session.Dir(m.stateRoot)), w, th.Meta) + "\n")
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
