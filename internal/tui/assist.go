package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/pkg/revier"
)

// ExitNoAssistant is the status `revier assist` ends with when no coding
// agent is installed. It is declared here because the surface is what reads
// it: the command's own message left the screen with the hand-over.
const ExitNoAssistant = 6

// ErrNoAssistant is that outcome, as the command and the surface both say it.
var ErrNoAssistant = errors.New("no coding agent to brief: install Claude Code, so that `claude` is on PATH")

// assistedMsg follows the assistant's exit. What it changed is in the files,
// so they are read again, as on the raise of the popup.
type assistedMsg struct{ err error }

// reloadAssisted is reloadFiles after the assistant. A configuration it left
// that does not load is said on the surface, where a raise only logs one.
func reloadAssisted() tea.Msg {
	msg := readFiles()
	msg.assisted = true
	return msg
}

// openAssist hands the terminal to `revier assist`, as an action is handed
// it. The surface runs the command rather than starting the agent itself, so
// the key and the shell reach one thing, and that thing still starts when the
// surface does not.
func (m Model) openAssist() (tea.Model, tea.Cmd) {
	self, err := os.Executable()
	if err != nil {
		m.err = err
		return m, nil
	}
	return m, tea.ExecProcess(exec.Command(self, m.assistArgs()...), func(err error) tea.Msg {
		return assistedMsg{err: assistErr(err)}
	})
}

// assistArgs is `revier assist` as the surface runs it. The project under the
// cursor goes with it: the agent is told where the user was, so "this
// project" means something to it.
func (m Model) assistArgs() []string {
	args := []string{"assist"}
	if name, ok := m.underCursor(); ok {
		args = append(args, "-p", string(name))
	}
	return args
}

// underCursor is the project of the row under the cursor. On the agent list
// that is the project of the agent there: the project list is off the screen,
// and its cursor is wherever the user left it.
func (m Model) underCursor() (revier.ProjectName, bool) {
	if m.agents.shown {
		it, ok := m.agents.selected()
		return it.project.Name, ok
	}
	return m.selectedName()
}

// assistErr is what the surface shows for how `revier assist` ended. Any
// other failure names the command: its own message left the screen with the
// hand-over, and a bare exit status says nothing about what ended.
func assistErr(err error) error {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == ExitNoAssistant:
		return ErrNoAssistant
	}
	return fmt.Errorf("revier assist: %w", err)
}
