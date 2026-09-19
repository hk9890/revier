package tui

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/checkout"
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
	m.tkeys = targetKeys(m.projects, m.keys)
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

// askDelete starts the confirmation for removing the highlighted project's
// file. A project with anything running is refused, as the picker refuses a
// running session (os-fzf.sh handle_delete): its windows would outlive the
// only entry that can reach them.
func (m Model) askDelete() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	if err := m.refuseRunning(v, "deleting"); err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.confirm = v.Project.Name
	return m, nil
}

// confirmDelete takes the key that answers the question. Only "y" deletes;
// every other key keeps the project and is not acted on, so a stray Enter
// cannot fall through and open what the user just declined to delete.
func (m Model) confirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := m.confirm
	m.confirm = ""
	if msg.String() != "y" {
		return m, nil
	}
	// Asked again: a survey may have arrived between the question and the
	// answer, and the project may have started since.
	for _, v := range m.views {
		if v.Project.Name == name {
			if err := m.refuseRunning(v, "deleting"); err != nil {
				m.err = err
				return m, nil
			}
		}
	}
	p, ok := m.project(name)
	if !ok || p.File == "" {
		return m, nil
	}
	if err := os.Remove(p.File); err != nil {
		m.err = err
		return m, nil
	}
	slog.Info("project deleted", "project", name, "path", p.File)
	m.projects = without(m.projects, name)
	m.views = m.known(m.views)
	m.tkeys = targetKeys(m.projects, m.keys)
	m.reload()
	return m, nil
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
	running := v.Running
	for _, t := range v.Targets {
		running = running || !t.Ref.IsZero()
	}
	for _, ref := range m.bound[name] {
		running = running || m.hostHere(ref.Host)
	}
	if running {
		return fmt.Errorf("%s is running; close its targets before %s it", name, doing)
	}
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
	for _, ref := range m.attached[name] {
		if m.core.Window != nil && ref.Host == m.core.Window.Name() {
			return fmt.Errorf("%s has an attached window open; close it before %s the project", name, doing)
		}
	}
	return nil
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
// the file, because the file is what goes.
func (m Model) deletePrompt() string {
	file := ""
	if p, ok := m.project(m.confirm); ok {
		file = contractHome(p.File)
	}
	return m.theme.Attention.Render(fmt.Sprintf(" delete %s? removes %s  ", m.confirm, file)) +
		m.theme.Help.Render("y: delete · any other key: keep")
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
