package tui

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// del closes the row under the cursor: a project, a target, a window attached
// to the project, or an agent. The plan is made from a survey on the press,
// by the core's ShutdownPlan for a project and ClosePlan for a row, and runs
// as a shutdown runs. A close that ends a conversation, or ends more than one
// thing, is confirmed on the shutdown screen first: a project, a target that
// holds an agent, and an agent in a turn. The rest closes at once. alt+del
// (files.go) closes the same way before it deletes.

// closePlannedMsg is the survey and the plan a del asked for.
type closePlannedMsg struct {
	project  revier.ProjectName
	row      core.CloseRow
	label    string
	drop     bool
	projects []core.Project // what the survey covered, for the recheck on confirm
	report   core.Report
	plan     []core.CloseStep
	err      error
}

// atEnd reports a query field with nothing right of its cursor, where del has
// nothing to delete in the query.
func atEnd(in *textinput.Model) bool {
	return in.Position() >= len([]rune(in.Value()))
}

// askClose is del on the row under the cursor.
func (m Model) askClose() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	name := v.Project.Name
	switch m.focus {
	case focusTargets:
		rows := m.targetRows()
		if m.tcursor >= len(rows) {
			return m, nil
		}
		r := rows[m.tcursor]
		if !r.attached.IsZero() {
			return m.closeRow(name, core.CloseRow{Attached: r.attached}, r.attached.Title, false)
		}
		// Whether it is open is not known (decisions.md D89): the host says,
		// not a plan made without it.
		if r.target.Unknown != "" {
			m.err = fmt.Errorf("%s: %s; wait for the host before closing it", r.target.Name, r.target.Unknown)
			return m, nil
		}
		return m.closeRow(name, core.CloseRow{Target: r.target.Name}, string(r.target.Name), false)
	case focusAgents:
		rows := m.agentRows()
		if m.acursor >= len(rows) {
			return m, nil
		}
		a := rows[m.acursor].agent
		return m.closeRow(name, core.CloseRow{Agent: a.Ref, Panel: a.Panel}, harnessOf(a), false)
	}
	return m.closeRow(name, core.CloseRow{}, string(name), false)
}

// closeRow surveys and plans the close of the project, or of one row of it
// when row is set. drop deletes what the configuration holds of it once it
// is closed.
func (m Model) closeRow(project revier.ProjectName, row core.CloseRow, label string, drop bool) (tea.Model, tea.Cmd) {
	m.err = nil
	if m.shut.running {
		m.err = errors.New("a close is still running; press again once it is done")
		return m, nil
	}
	whole := row == core.CloseRow{}
	if whole && !drop {
		if m.err = m.refuseSave(); m.err != nil {
			return m, nil
		}
	}
	// A project's close keeps what another project holds open, so every
	// project is surveyed; a row's close is about its project alone.
	projects := m.projects
	if !whole {
		p, ok := m.project(project)
		if !ok {
			return m, nil
		}
		projects = []core.Project{p}
	}
	c, root := m.core, m.stateRoot
	asked := closePlannedMsg{project: project, row: row, label: label, drop: drop, projects: projects}
	return m, func() tea.Msg {
		asked.report, asked.plan, asked.err = surveyPlan(c, root, projects, func(r core.Report) []core.CloseStep {
			if whole {
				return c.ShutdownPlan(r, project, core.ShutdownAll)
			}
			return c.ClosePlan(r, project, row)
		})
		return asked
	}
}

// closePlanned takes the plan a del asked for. A screen opened since, or a
// close started since, drops it: the press it answers is no longer the last.
func (m Model) closePlanned(msg closePlannedMsg) (tea.Model, tea.Cmd) {
	if m.dialog != dialogNone || m.shut.running || m.confirm != "" {
		return m, nil
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	if len(msg.plan) == 0 && msg.drop {
		// Nothing of it closes here: it closed since the last survey, or
		// another project holds what is open of it too. The delete is asked
		// as for what is not open, and checked again on the answer.
		m.confirm, m.ctarget = msg.project, msg.row.Target
		return m, nil
	}
	if len(msg.plan) == 0 {
		m.err = fmt.Errorf("%s: nothing of it is open here", msg.label)
		return m, nil
	}
	m.shut = shutdown{
		step: shutConfirm, planned: true, projects: msg.projects, report: msg.report, plan: msg.plan,
		one: true, project: msg.project, pick: msg.row, label: msg.label, drop: msg.drop,
	}
	if confirms(msg) {
		m.dialog = dialogShutdown
		return m, nil
	}
	return m.shutRun(false)
}

// confirms reports a close to ask about first: a delete, a project, a target
// that holds an agent, or an agent in a turn.
func confirms(msg closePlannedMsg) bool {
	if msg.drop || msg.row == (core.CloseRow{}) || len(core.Busy(msg.plan)) > 0 {
		return true
	}
	if msg.row.Panel != "" {
		return false
	}
	for _, s := range msg.plan {
		if len(s.Agents) > 0 {
			return true
		}
	}
	return false
}

// closed takes the answer of a close del or alt+del asked for. The surface
// comes back as it was, and the next survey shows what went. A delete runs
// only when everything closed.
func (m Model) closed(msg shutdownMsg) (tea.Model, tea.Cmd) {
	one := m.shut
	m.shut = shutdown{}
	if m.dialog == dialogShutdown {
		m.dialog = dialogNone
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	_, open, failed := msg.closed.Counts()
	var err error
	switch {
	case failed > 0:
		err = fmt.Errorf("%s did not close", core.Count(failed, "step"))
	case open > 0:
		err = fmt.Errorf("%s still open", core.Count(open, "step"))
	case one.drop:
		if one.pick.Target != "" {
			err = m.removeTarget(one.project, one.pick.Target)
		} else {
			err = m.removeProject(one.project)
		}
		// Update logs no error a shutdownMsg brings, because the close logs
		// its own steps; the delete after it is not one of them.
		if err != nil {
			slog.Error("delete after the close", "project", one.project, "target", one.pick.Target, "err", err)
		}
		m.err = err
		return m, nil
	}
	if err != nil && one.drop {
		err = fmt.Errorf("%w; nothing deleted", err)
	}
	m.err = err
	return m, nil
}

// closeTitle is the question the confirm step asks for a close del or
// alt+del asked for.
func (m Model) closeTitle() string {
	s := m.shut
	what := string(s.project)
	if s.pick != (core.CloseRow{}) {
		what = s.label + " of " + what
	}
	return m.closeVerb() + " " + what + "?"
}

// closeVerb is what the confirm step's first row does.
func (m Model) closeVerb() string {
	s := m.shut
	switch {
	case !s.drop:
		return "Close"
	case s.pick.Target != "":
		return "Close and delete"
	}
	return "Close and " + m.deleteVerb(s.project)
}
