package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The surface holds prepared projects for its lifetime (decisions.md D17). A
// project the surface itself changed - written on the project screen, or
// removed - is the exception: that change is the one it knows about, so it
// applies it without a restart.

// clonedMsg follows the clone Enter started for a project whose directory is
// missing; the home target opens once it succeeds.
type clonedMsg struct {
	project core.Project
	home    revier.TargetName
	err     error
}

// highlighted is the prepared project under the cursor.
func (m Model) highlighted() (core.Project, bool) {
	v, ok := m.selected()
	if !ok {
		return core.Project{}, false
	}
	return m.project(v.Project.Name)
}

// replaceProject puts p where the project called name was. A rename changes
// the name, and the cursor stays on the project.
func (m *Model) replaceProject(name revier.ProjectName, p core.Project) {
	// New lists, not writes into the old ones: a survey still running reads
	// the old list on another goroutine.
	projects := slices.Clone(m.projects)
	for i := range projects {
		if projects[i].Name == name {
			projects[i] = p
		}
	}
	views := slices.Clone(m.views)
	for i := range views {
		if views[i].Project.Name == name {
			views[i] = provisional(views[i], p)
		}
	}
	m.projects, m.views = projects, views
	m.setKeys()
	m.reload()
	m.selectName(p.Name)
}

// provisional is the view of a project as its file was just written, until
// the next survey answers: the row the survey found for a target the file
// still declares, a bare row for one it now declares, the attached windows,
// and the file's own reason. A target the file no longer declares has no row
// to run.
func provisional(v revier.ProjectView, p core.Project) revier.ProjectView {
	found := map[revier.TargetName]revier.TargetView{}
	var attached []revier.TargetView
	for _, tv := range v.Targets {
		if tv.Attached {
			attached = append(attached, tv)
			continue
		}
		found[tv.Name] = tv
	}
	targets := make([]revier.TargetView, 0, len(p.Targets)+len(attached))
	for _, t := range p.Targets {
		tv, ok := found[t.Name]
		if !ok {
			tv = revier.TargetView{Name: t.Name}
		}
		tv.Key = t.Key
		targets = append(targets, tv)
	}
	v.Project, v.Targets, v.Invalid = p.Project, append(targets, attached...), ""
	if p.Invalid != nil {
		v.Invalid = p.Invalid.Error()
	}
	return v
}

// askDelete is alt+del: it deletes what the configuration holds of the row
// under the cursor. On the list that is the project's file, which for a link
// is the link alone: the project on its host is not touched. In the pane it
// is a target's entry in the project file; a target of config.toml is every
// project's, and an agent or an attached window is in no file. What is open
// of it closes first, as del closes it, so no window outlives the only entry
// that can reach it; what is not open is deleted after a y.
func (m Model) askDelete() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.err = nil
	switch m.focus {
	case focusTargets:
		return m.askDropTarget(v)
	case focusAgents:
		m.err = errors.New("an agent is in no file; del closes it")
		return m, nil
	}
	// Before the first survey the view is the files' alone, and says nothing
	// about what is open.
	if m.surveyed && m.openHere(v) {
		if err := m.refuseUnsettled(v, "deleting"); err != nil {
			m.err = err
			return m, nil
		}
		return m.closeRow(v.Project.Name, core.CloseRow{}, string(v.Project.Name), true)
	}
	if err := m.refuseRunning(v, "deleting"); err != nil {
		m.err = err
		return m, nil
	}
	m.confirm = v.Project.Name
	return m, nil
}

// askDropTarget is alt+del on a target row: its entry in the project file
// goes, once the target is closed.
func (m Model) askDropTarget(v revier.ProjectView) (tea.Model, tea.Cmd) {
	rows := m.targetRows()
	if m.tcursor >= len(rows) {
		return m, nil
	}
	row := rows[m.tcursor]
	if !row.attached.IsZero() {
		m.err = errors.New("an attached window is in no file; del closes it")
		return m, nil
	}
	name := row.target.Name
	p, ok := m.project(v.Project.Name)
	if !ok || p.File == "" {
		return m, nil
	}
	text, err := config.ReadProject(p.File, m.usable)
	if err != nil {
		m.err = err
		return m, nil
	}
	i := slices.IndexFunc(text.Targets, func(pt config.ProjectTarget) bool { return pt.Target.Name == name })
	switch {
	case i < 0:
		err = fmt.Errorf("target %q is in no file", name)
	case text.Targets[i].Source == config.FromShared:
		err = fmt.Errorf("target %q is config.toml's, shared by every project; the config screen changes it", name)
	case text.Targets[i].Source == config.Overridden:
		err = fmt.Errorf("target %q is config.toml's, shared by every project; the project screen drops this project's changes to it", name)
	case text.Targets[i].Source == config.Derived:
		err = fmt.Errorf("target %q comes with the link and is in no file", name)
	default:
		// Asked before anything closes: a delete the file refuses must not
		// cost the windows and agents the close ends first.
		err = config.CheckRemoveProjectTarget(p.File, m.usable, name)
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	open, err := m.targetOpen(v.Project.Name, row.target)
	switch {
	case err != nil:
		m.err = err
	case open:
		return m.closeRow(v.Project.Name, core.CloseRow{Target: name}, string(name), true)
	default:
		m.confirm, m.ctarget = v.Project.Name, name
	}
	return m, nil
}

// targetOpen reports a target running here, by the last survey or by where
// state says it landed. It refuses while that is not known: before the first
// survey, while its host cannot list, or while its window is still to
// appear.
func (m Model) targetOpen(project revier.ProjectName, tv revier.TargetView) (bool, error) {
	switch {
	case !m.surveyed:
		return false, fmt.Errorf("no survey has answered yet; wait for it before deleting %s", tv.Name)
	case tv.Unknown != "":
		return false, fmt.Errorf("%s: %s; wait for the host before deleting it", tv.Name, tv.Unknown)
	case m.pending != nil && m.pending.Project == project && m.pending.Target == tv.Name && time.Since(m.pending.At) <= core.BindWindow:
		return false, fmt.Errorf("%s is coming up; wait for its window before deleting it", tv.Name)
	}
	ref, bound := m.bound[project][tv.Name]
	return !tv.Ref.IsZero() || (bound && m.hostHere(ref.Host)), nil
}

// confirmDelete takes the key that answers the question. Only "y" deletes;
// every other key keeps the project and is not acted on, so a stray Enter
// cannot fall through and open what the user just declined to delete.
func (m Model) confirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name, target := m.confirm, m.ctarget
	m.confirm, m.ctarget = "", ""
	if msg.String() != "y" {
		return m, nil
	}
	// Asked again: a survey may have arrived between the question and the
	// answer, and the target or the project may have started since.
	if target != "" {
		if err := m.refuseStartedTarget(name, target); err != nil {
			m.err = err
			return m, nil
		}
		m.err = m.removeTarget(name, target)
		return m, nil
	}
	for _, v := range m.views {
		if v.Project.Name == name {
			if err := m.refuseRunning(v, "deleting"); err != nil {
				m.err = err
				return m, nil
			}
		}
	}
	m.err = m.removeProject(name)
	return m, nil
}

// removeProject deletes the project's file, and the project from the surface.
func (m *Model) removeProject(name revier.ProjectName) error {
	p, ok := m.project(name)
	if !ok || p.File == "" {
		return nil
	}
	if err := os.Remove(p.File); err != nil {
		return err
	}
	slog.Info("project deleted", "project", name, "path", p.File)
	m.projects = without(m.projects, name)
	m.views = m.known(m.views)
	m.setKeys()
	m.reload()
	return nil
}

// removeTarget deletes the target's entry in the project's file.
func (m *Model) removeTarget(name revier.ProjectName, target revier.TargetName) error {
	p, ok := m.project(name)
	if !ok {
		return nil
	}
	written, err := config.RemoveProjectTarget(p.File, m.usable, target)
	if err != nil {
		return err
	}
	slog.Info("target deleted", "project", name, "target", target, "path", p.File)
	m.replaceProject(name, written)
	m.tcursor = clampRow(m.tcursor, len(m.targetRows()))
	return nil
}

// refuseRunning refuses deleting or renaming a project with a target running
// or a window attached to it. The last survey says what runs; a target that
// landed since, or a launch whose window is still to appear, is read from
// state, else the change would leave that window reachable by no name. An
// attachment or a binding on a host here is alive: every refresh prunes the
// ones that host no longer lists. One on another host - a surface started
// where that host does not probe - cannot be reached from here, so it does
// not hold the change up.
func (m Model) refuseRunning(v revier.ProjectView, doing string) error {
	name := v.Project.Name
	// Before the first survey the view is the files' alone, and says nothing
	// about what runs.
	if !m.surveyed {
		return fmt.Errorf("no survey has answered yet; wait for it before %s %s", doing, name)
	}
	if m.running(v) {
		return fmt.Errorf("%s is running; close its targets before %s it", name, doing)
	}
	if err := m.refuseUnsettled(v, doing); err != nil {
		return err
	}
	if m.attachedHere(name) {
		return fmt.Errorf("%s has an attached window open; close it before %s the project", name, doing)
	}
	return nil
}

// refuseUnsettled refuses the change while what of the project runs is not
// known: a target whose host could not list, or a launch whose window is
// still to appear. A close cannot reach either, so a delete that closes first
// is refused for them too.
func (m Model) refuseUnsettled(v revier.ProjectView, doing string) error {
	name := v.Project.Name
	// A target whose host could not list may be running (decisions.md D89):
	// the change waits for the host, not for a guess.
	for _, t := range v.Targets {
		if t.Unknown != "" {
			return fmt.Errorf("%s: %s; wait for the host before %s it", name, t.Unknown, doing)
		}
	}
	if m.pending != nil && m.pending.Project == name && time.Since(m.pending.At) <= core.BindWindow {
		return fmt.Errorf("%s is coming up; wait for its window before %s it", name, doing)
	}
	return nil
}

// refuseStartedTarget refuses deleting the entry of a target that runs, or
// may, by the survey that answered since the question.
func (m Model) refuseStartedTarget(project revier.ProjectName, target revier.TargetName) error {
	for _, v := range m.views {
		if v.Project.Name != project {
			continue
		}
		for _, tv := range v.Targets {
			if tv.Attached || tv.Name != target {
				continue
			}
			open, err := m.targetOpen(project, tv)
			if err == nil && open {
				err = fmt.Errorf("%s is running; alt+del closes it before deleting it", target)
			}
			return err
		}
	}
	return nil
}

// running reports a project with a target running, by the last survey or by
// where state says a target landed.
func (m Model) running(v revier.ProjectView) bool {
	running := v.Held()
	for _, ref := range m.bound[v.Project.Name] {
		running = running || m.hostHere(ref.Host)
	}
	return running
}

// attachedHere reports a window attached to the project on the window host
// here.
func (m Model) attachedHere(name revier.ProjectName) bool {
	for _, ref := range m.attached[name] {
		if m.core.Window != nil && ref.Host == m.core.Window.Name() {
			return true
		}
	}
	return false
}

// openHere reports a project with anything open that a close here reaches.
func (m Model) openHere(v revier.ProjectView) bool {
	return m.running(v) || m.attachedHere(v.Project.Name)
}

// hostHere reports a host this surface lists, whose refs a refresh prunes.
func (m Model) hostHere(host string) bool {
	return (m.core.Runtime != nil && host == m.core.Runtime.Name()) || (m.core.Window != nil && host == m.core.Window.Name())
}

func without(projects []core.Project, name revier.ProjectName) []core.Project {
	out := make([]core.Project, 0, len(projects))
	for _, p := range projects {
		if p.Name != name {
			out = append(out, p)
		}
	}
	return out
}

// uncovered carries the rows of projects the survey did not cover. A survey
// started before a link was written answers after it, from the list it began
// with, and would otherwise take the new row off the screen - and the cursor
// with it - until the next refresh answered.
func (m Model) uncovered(views []revier.ProjectView) []revier.ProjectView {
	covered := make(map[revier.ProjectName]bool, len(views))
	for _, v := range views {
		covered[v.Project.Name] = true
	}
	out := views
	for _, v := range m.views {
		if !covered[v.Project.Name] {
			out = append(out, v)
		}
	}
	return out
}

// known drops the views of projects the surface no longer holds. A survey
// started before a delete answers after it, and would otherwise put the
// deleted row back for a refresh.
func (m Model) known(views []revier.ProjectView) []revier.ProjectView {
	out := make([]revier.ProjectView, 0, len(views))
	for _, v := range views {
		if _, ok := m.project(v.Project.Name); ok {
			out = append(out, v)
		}
	}
	return out
}

// deletePrompt is the footer while a delete waits for its answer. It names
// the file, because the file is what goes or changes.
func (m Model) deletePrompt() string {
	verb := m.deleteVerb(m.confirm)
	question := verb + " " + string(m.confirm)
	if m.ctarget != "" {
		verb, question = "delete", fmt.Sprintf("delete target %q of %s", m.ctarget, m.confirm)
	}
	return m.theme.Attention.Render(fmt.Sprintf(" %s? %s  ", question, m.dropNote(m.confirm, m.ctarget))) +
		m.theme.Help.Render("y: "+verb+" · any other key: keep")
}

// deleteVerb is what deleting the project is: for a link, unlinking.
func (m Model) deleteVerb(name revier.ProjectName) string {
	if p, ok := m.project(name); ok && p.Remote != nil {
		return "unlink"
	}
	return "delete"
}

// dropNote says what deleting the project, or its target when one is named,
// changes on disk. Unlinking says what it leaves as well: the project on the
// host is not touched.
func (m Model) dropNote(name revier.ProjectName, target revier.TargetName) string {
	p, ok := m.project(name)
	if !ok {
		return ""
	}
	file := contractHome(p.File)
	switch {
	case target != "":
		return "removes it from " + file
	case p.Remote != nil:
		return "removes " + file + "; nothing on " + p.Remote.Host + " changes"
	}
	return "removes " + file
}

// clone runs the clone of a project whose directory is missing, with the
// terminal handed over so git's progress is on screen, and opens the home
// target once it is done.
func (m Model) clone(p core.Project, home revier.TargetName) tea.Cmd {
	return tea.Exec(&cloneCmd{project: p.Project}, func(err error) tea.Msg {
		return clonedMsg{project: p, home: home, err: err}
	})
}

// cloneCmd is checkout.Ensure as something bubbletea can hand the terminal
// to. The output is kept as well as shown: the screen returns to the list the
// moment git exits, so the reason a clone failed has to reach the footer.
type cloneCmd struct {
	project revier.Project
	out     io.Writer
}

func (c *cloneCmd) SetStdin(io.Reader)    {}
func (c *cloneCmd) SetStdout(w io.Writer) { c.out = w }
func (c *cloneCmd) SetStderr(io.Writer)   {}
func (c *cloneCmd) Run() error {
	var kept bytes.Buffer
	out := io.Writer(&kept)
	if c.out != nil {
		out = io.MultiWriter(c.out, &kept)
	}
	if _, err := checkout.Ensure(c.project, out); err != nil {
		if last := lastLine(kept.String()); last != "" {
			return fmt.Errorf("%w: %s", err, last)
		}
		return err
	}
	return nil
}

// lastLine is the last non-empty line of git's output. Progress redraws a
// line with carriage returns, so those end a line too.
func lastLine(s string) string {
	lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
