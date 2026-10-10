package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/pkg/revier"
)

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
	// What the command says on stderr is kept and not shown: the screen is
	// the surface's again the moment the command ends, so the reason it
	// failed has to reach the footer. The agent is not on this stream; the
	// command gives it the terminal.
	var said bytes.Buffer
	cmd := exec.Command(self, m.assistArgs()...)
	cmd.Stderr = &said
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return assistedMsg{err: assistErr(err, said.String())}
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

// assistErr is what the surface shows for how `revier assist` ended: the
// reason the command gave, which is the last line it said when that line is
// its failure line. A command that ended without one - killed, or failed
// after a warning alone - is named, since a bare exit status says nothing
// about what ended.
func assistErr(err error, said string) error {
	if err == nil {
		return nil
	}
	if reason, ok := strings.CutPrefix(lastLine(said), "revier: "); ok && !strings.HasPrefix(reason, "warning: ") {
		return errors.New(reason)
	}
	return fmt.Errorf("revier assist: %w", err)
}
