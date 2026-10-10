package tui

import (
	"errors"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
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
	return m, tea.ExecProcess(exec.Command(self, "assist"), func(err error) tea.Msg {
		return assistedMsg{err: assistErr(err)}
	})
}

// assistErr is what the surface shows for how `revier assist` ended.
func assistErr(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == ExitNoAssistant {
		return ErrNoAssistant
	}
	return err
}
