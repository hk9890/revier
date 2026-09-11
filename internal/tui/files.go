package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The surface holds prepared projects for its lifetime (decisions.md D17). The
// two keys here are the exception: the file the surface itself handed to the
// editor, or removed, is the one change it knows about, so it applies that one
// without a restart.

// editedMsg follows the editor exiting.
type editedMsg struct {
	project revier.ProjectName
	file    string
	err     error
}

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

// editFile hands the highlighted project's file to $EDITOR, with the terminal,
// as `os edit` does. $EDITOR is run as it is, without a shell, the way the
// shell tool runs it.
func (m Model) editFile() (tea.Model, tea.Cmd) {
	p, ok := m.highlighted()
	if !ok || p.File == "" {
		return m, nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		m.err = errors.New("$EDITOR is not set, so there is nothing to edit the project file with")
		return m, nil
	}
	name, file := p.Name, p.File
	return m, tea.ExecProcess(exec.Command(editor, file), func(err error) tea.Msg {
		return editedMsg{project: name, file: file, err: err}
	})
}

// reread applies an edited file. A file that no longer loads leaves the
// project as it was: the error names what is wrong, and the next edit can fix
// it, while a project dropped from the list could not be reached to edit.
func (m *Model) reread(msg editedMsg) error {
	p, err := config.LoadProject(msg.file)
	if err != nil {
		return fmt.Errorf("%w; %s is shown as it was before the edit", err, msg.project)
	}
	for i := range m.projects {
		if m.projects[i].Name == msg.project {
			m.projects[i] = p
		}
	}
	m.tkeys = targetKeys(m.projects)
	return msg.err
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
	if err := refuseRunning(v); err != nil {
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
			if err := refuseRunning(v); err != nil {
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
	m.projects = without(m.projects, name)
	m.views = m.known(m.views)
	m.tkeys = targetKeys(m.projects)
	m.reload()
	return m, nil
}

func refuseRunning(v revier.ProjectView) error {
	running := v.Running
	for _, t := range v.Targets {
		running = running || !t.Ref.IsZero()
	}
	if running {
		return fmt.Errorf("%s is running; close its targets before deleting it", v.Project.Name)
	}
	return nil
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
