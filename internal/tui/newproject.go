package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The new-project screen: one field, the directory. The name is the
// directory's base name, as `revier new` derives it, so there is nothing
// else to ask. It writes the same file that command writes, and the project
// is a row the moment it lands.

// newPathInput is the directory field.
func newPathInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = "~/dev/example"
	in.CharLimit = 512
	return in
}

// openNew is the "new" button and alt+n. The cursor comes back to the list
// first, so the surface the screen stands over is the one it is left on.
func (m Model) openNew() (tea.Model, tea.Cmd) {
	m.err = nil
	m.path.SetValue("")
	m.toList()
	m.dialog = dialogNew
	return m, m.path.Focus()
}

// newKey is every press on the new-project screen. Enter writes the project,
// Esc goes back, and everything else is the field's: it is the one place on
// the surface where a printable rune is text rather than a filter.
func (m Model) newKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.dialog = dialogNone
		m.path.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.createProject()
	}
	if altRune(msg) {
		return m, nil
	}
	m.err = nil
	in, cmd := m.path.Update(msg)
	m.path = in
	return m, cmd
}

// createProject writes the project file for the directory in the field. A
// directory that is not there is refused: revier clones a project whose
// checkout is missing from its git_url, and there is none to clone from yet.
func (m Model) createProject() (tea.Model, tea.Cmd) {
	m.err = nil
	dir := strings.TrimSpace(m.path.Value())
	if dir == "" {
		m.err = fmt.Errorf("give the directory of the project to add")
		return m, nil
	}
	dir = config.ExpandHome(dir)
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			m.err = err
			return m, nil
		}
		dir = abs
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		m.err = fmt.Errorf("%s is not a directory here", contractHome(dir))
		return m, nil
	}
	name := config.NameFor(dir)
	if p, ok := m.project(name); ok {
		m.err = fmt.Errorf("project %q already exists: %s", name, contractHome(p.File))
		return m, nil
	}
	root, err := config.Root()
	if err != nil {
		m.err = err
		return m, nil
	}
	p, err := config.Create(root, name, dir, "")
	if err != nil {
		m.err = err
		return m, nil
	}
	m.projects = append(m.projects, p)
	m.tkeys = targetKeys(m.projects, m.keys)
	// Provisional, until the survey answers: nothing of it is running, and
	// its directory is the one just checked.
	m.views = sorted(append(m.views, revier.ProjectView{Project: p.Project, PathExists: true}))
	m.dialog = dialogNone
	m.path.Blur()
	m.reload()
	m.selectName(p.Name)
	return m, nil
}

// newScreen is what stands in the list's place while the screen is up: what
// the field is for, and what pressing Enter will write.
func (m Model) newScreen() string {
	th := m.theme
	w := m.listWidth()
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	dir := strings.TrimSpace(m.path.Value())
	if dir == "" {
		return say(th.NameDim, "The directory of a project on this machine.") + "\n" +
			say(th.Meta, "Its name is the directory's own.")
	}
	name := config.NameFor(config.ExpandHome(dir))
	root, err := config.Root()
	if err != nil {
		return say(th.Attention, err.Error())
	}
	return say(th.NameDim, "Enter writes") + "\n" +
		say(th.Path, contractHome(config.ProjectFile(root, name))) + "\n" +
		say(th.Meta, m.newTargets(name))
}

// newTargets says what the written project will run. With shared targets in
// config.toml the file declares no targets of its own, and the project gets
// the shared ones.
func (m Model) newTargets(name revier.ProjectName) string {
	if len(m.targets) == 0 {
		return fmt.Sprintf("an agent, a shell and an editor for %q", name)
	}
	names := make([]string, len(m.targets))
	for i, t := range m.targets {
		names[i] = string(t.Name)
	}
	return fmt.Sprintf("%q with the shared targets: %s", name, strings.Join(names, ", "))
}
