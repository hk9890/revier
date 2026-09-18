package tui

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The project screen, alt+e or the name at the top of the pane: the project
// file of the highlighted project, as the config screen is config.toml
// (decisions.md D81). Its name, path and git_url are typed in place, and its
// targets are listed as the project has them, config.toml's among them, each
// with the form the config screen uses. A change is written as it is made,
// to the project file alone, and the surface takes the project as it now
// loads.
//
// A name is the file's name (D80), so a new one moves the file; a link's host
// and its name there are the link's identity and are shown, not offered.

// projectField is a row of the screen's first section.
type projectField int

const (
	projName projectField = iota
	projPath
	projGitURL
)

// projectFieldKeys are the file's keys for the fields a value is written to.
var projectFieldKeys = map[projectField]string{projPath: "path", projGitURL: "git_url"}

func newFieldInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	styleField(&in, th)
	in.Prompt = ""
	return in
}

// openProject is alt+e and a click on the pane's name.
func (m Model) openProject() (tea.Model, tea.Cmd) {
	p, ok := m.highlighted()
	if !ok || p.File == "" {
		return m, nil
	}
	text, err := config.ReadProject(p.File, m.shared)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.toList()
	m.proj, m.ptext, m.prow = p.Name, text, 0
	m.pedit.Blur()
	m.aform = actionForm{}
	m.tform = targetForm{}
	m.dropping = false
	m.body.SetYOffset(0)
	m.dialog = dialogProject
	return m, nil
}

// projectFields are the first section's rows. A link has no directory or
// checkout here, only its name.
func (m Model) projectFields() []projectField {
	if m.ptext.Remote != nil {
		return []projectField{projName}
	}
	return []projectField{projName, projPath, projGitURL}
}

// projectTargetRow is the target the cursor is on, if any.
func (m Model) projectTargetRow() (int, bool) {
	i := m.prow - len(m.projectFields())
	return i, i >= 0 && i < len(m.ptext.Targets)
}

// addProjectTargetRow is the row that adds a target.
func (m Model) addProjectTargetRow() int {
	return len(m.projectFields()) + len(m.ptext.Targets)
}

// projectValue is a field as the file has it.
func (m Model) projectValue(f projectField) string {
	switch f {
	case projPath:
		return m.ptext.Path
	case projGitURL:
		return m.ptext.GitURL
	}
	return string(m.proj)
}

// projectKey is every press on the project screen.
func (m Model) projectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.pedit.Focused():
		return m.projectEditKey(msg)
	case m.tform.open:
		return m.targetFormKey(msg)
	case m.dropping:
		return m.confirmDropProjectTarget(msg)
	}
	m.err = nil
	i, onTarget := m.projectTargetRow()
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back, m.keys.Edit):
		m.dialog = dialogNone
		m.layout()
	case key.Matches(msg, m.keys.Up):
		m.prow = max(m.prow-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.prow = min(m.prow+1, m.addProjectTargetRow())
	case onTarget && key.Matches(msg, m.keys.Delete):
		pt := m.ptext.Targets[i]
		if pt.Source == config.FromShared || pt.Source == config.Derived {
			m.err = fmt.Errorf("target %q is not in the project file; there is nothing to delete", pt.Target.Name)
			return m, nil
		}
		m.dropping = true
	case onTarget && key.Matches(msg, m.keys.Enter):
		return m.openProjectTargetForm(i)
	case m.prow == m.addProjectTargetRow() && key.Matches(msg, m.keys.Enter):
		return m.openProjectTargetForm(len(m.ptext.Targets))
	case key.Matches(msg, m.keys.Enter):
		// As wide as the row leaves it, so a long path scrolls under the
		// cursor instead of running off the right edge, typed blind.
		m.pedit.Width = max(m.listWidth()-lipgloss.Width(cursor(m.theme, true))-configLabelWidth-1, 8)
		m.pedit.SetValue(m.projectValue(m.projectFields()[m.prow]))
		m.pedit.CursorEnd()
		return m, m.pedit.Focus()
	}
	return m, nil
}

// projectEditKey is a press while a field is typed. Enter writes it, Esc
// leaves it as it was.
func (m Model) projectEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.pedit.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if err := m.saveProjectField(m.projectFields()[m.prow], strings.TrimSpace(m.pedit.Value())); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.pedit.Blur()
		return m, nil
	case altRune(msg):
		return m, nil
	}
	in, cmd := m.pedit.Update(msg)
	m.pedit = in
	return m, cmd
}

func (m *Model) saveProjectField(f projectField, value string) error {
	if f == projName {
		return m.renameProject(revier.ProjectName(value))
	}
	if value == m.projectValue(f) {
		return nil
	}
	p, _ := m.project(m.proj)
	written, err := config.SetProjectValue(p.File, m.shared, projectFieldKeys[f], value)
	if err != nil {
		return err
	}
	return m.projectWritten(m.proj, written)
}

// renameProject moves the project file, and what state and the saved
// sessions hold under the old name with it. A running project is refused:
// its instances are found by titles its name is part of.
func (m *Model) renameProject(to revier.ProjectName) error {
	from := m.proj
	if to == from {
		return nil
	}
	for _, v := range m.views {
		if v.Project.Name == from {
			if err := m.refuseRunning(v, "renaming"); err != nil {
				return err
			}
		}
	}
	var written core.Project
	err := withConfigRoot(func(root string) (err error) {
		written, err = config.Rename(root, from, to, m.shared)
		return err
	})
	if err != nil {
		return err
	}
	m.updateState(func(st *state.State) bool {
		st.Rename(from, to)
		return true
	})
	if err := session.Rename(m.stateRoot, from, to); err != nil {
		slog.Warn("rename: the saved sessions keep the old name", "from", from, "to", to, "err", err)
	}
	return m.projectWritten(from, written)
}

// projectWritten takes what a write to the project file left: the project for
// the surface, and the file as written for the screen.
func (m *Model) projectWritten(was revier.ProjectName, p core.Project) error {
	m.replaceProject(was, p)
	m.proj = p.Name
	text, err := config.ReadProject(p.File, m.shared)
	if err != nil {
		return err
	}
	m.ptext = text
	return nil
}

// openProjectTargetForm opens the form for the project's target i, or for a
// new one when i is past the last. A derived target is not in the file, so
// saving it declares one.
func (m Model) openProjectTargetForm(i int) (tea.Model, tea.Cmd) {
	f := m.newTargetForm(i, nil)
	if i < len(m.ptext.Targets) {
		pt := m.ptext.Targets[i]
		f = m.newTargetForm(i, &pt.Target)
		if pt.Source != config.Derived {
			f.was = pt.Target.Name
		}
		if pt.Shared != nil {
			v := valuesOf(*pt.Shared)
			f.shared = &v
		}
	}
	m.err = nil
	m.tform = f
	return m, m.tform.fields[tfName].Focus()
}

// saveProjectTarget writes the form's target to the project file.
func (m Model) saveProjectTarget(t revier.Target, from []int) (tea.Model, tea.Cmd) {
	p, _ := m.project(m.proj)
	written, err := config.SaveProjectTarget(p.File, m.shared, m.tform.was, config.TargetEdit{Target: t, PanelFrom: from})
	if err == nil {
		err = m.projectWritten(m.proj, written)
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.tform = targetForm{}
	for i, pt := range m.ptext.Targets {
		if pt.Target.Name == t.Name {
			m.prow = len(m.projectFields()) + i
		}
	}
	return m, nil
}

// confirmDropProjectTarget takes the key that answers the delete question.
// Only y deletes the file's entry: a target of the project's own goes, and
// an override leaves config.toml's target as it is there.
func (m Model) confirmDropProjectTarget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dropping = false
	i, ok := m.projectTargetRow()
	if msg.String() != "y" || !ok {
		return m, nil
	}
	p, _ := m.project(m.proj)
	written, err := config.RemoveProjectTarget(p.File, m.shared, m.ptext.Targets[i].Target.Name)
	if err == nil {
		err = m.projectWritten(m.proj, written)
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	m.prow = min(m.prow, m.addProjectTargetRow())
	return m, nil
}

// dropProjectPrompt is the delete question for the target under the cursor.
func (m Model) dropProjectPrompt() string {
	question := ""
	if i, ok := m.projectTargetRow(); ok {
		pt := m.ptext.Targets[i]
		question = fmt.Sprintf("delete target %q?", pt.Target.Name)
		if pt.Source == config.Overridden {
			question = fmt.Sprintf("drop this project's changes to target %q?", pt.Target.Name)
		}
	}
	return m.theme.Attention.Render(" "+question+"  ") +
		m.theme.Help.Render("y: delete · any other key: keep")
}

// projectHelp is the footer on the project screen.
func (m Model) projectHelp() []key.Binding {
	k := m.keys
	_, onTarget := m.projectTargetRow()
	switch {
	case m.pedit.Focused():
		return k.helpForConfig(configTyping)
	case m.tform.open:
		return k.helpForConfig(m.configHelp())
	case onTarget:
		return k.helpForConfig(configOnAction)
	case m.prow == m.addProjectTargetRow():
		return k.helpForConfig(configOnAdd)
	}
	return []key.Binding{helpKey("↑↓", "move"), helpKey("enter", "edit"), helpKey("esc", "back"), k.Quit}
}

// sourceNote says where a target comes from.
func sourceNote(s config.Source) string {
	switch s {
	case config.FromShared:
		return "config.toml"
	case config.Overridden:
		return "config.toml, changed here"
	case config.Derived:
		return "the link's own; saving it writes it here"
	}
	return "this project"
}

// projectScreen stands in the list's place while the screen is up, and the
// line the cursor is on, which the body keeps in view.
func (m Model) projectScreen() (string, int) {
	th := m.theme
	w := m.listWidth()
	var b strings.Builder
	at := 0
	row := func(r int, label, value, note string) {
		// While a form is open its own cursor is the one on screen.
		sel := m.prow == r && !m.tform.open
		if sel {
			at = strings.Count(b.String(), "\n")
		}
		b.WriteString(settingLine(th, sel, label, value, note, w) + "\n")
	}
	info := func(label, value string) {
		b.WriteString(clipTo(th.Path.Render("  ")+th.Meta.Render(pad(label, configLabelWidth))+th.NameDim.Render(value), w) + "\n")
	}

	b.WriteString(m.heading("Project", w))
	labels := map[projectField]string{projName: "name", projPath: "path", projGitURL: "git url"}
	notes := map[projectField]string{
		projName:   "the file's name; renaming moves the file",
		projPath:   "the directory",
		projGitURL: "where Enter clones a missing directory from",
	}
	for r, f := range m.projectFields() {
		value := m.projectValue(f)
		if m.pedit.Focused() && m.prow == r {
			value = m.pedit.View()
		}
		row(r, labels[f], value, notes[f])
	}
	if l := m.ptext.Remote; l != nil {
		info("host", l.Host)
		info("name there", string(l.Project))
		if m.ptext.Path != "" {
			info("path there", m.ptext.Path)
		}
		if m.ptext.GitURL != "" {
			info("git url there", m.ptext.GitURL)
		}
	}

	b.WriteString(m.heading("Targets", w))
	base := len(m.projectFields())
	form := func() {
		lines, line := m.targetFormLines(w)
		at = strings.Count(b.String(), "\n") + line
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, pt := range m.ptext.Targets {
		row(base+i, keyLabel(pt.Target.Key), string(pt.Target.Name), sourceNote(pt.Source)+" · "+describeTarget(pt.Target))
		if m.tform.open && m.tform.index == i {
			form()
		}
	}
	row(m.addProjectTargetRow(), "+", "add a target", "this project only")
	if m.tform.open && m.tform.index == len(m.ptext.Targets) {
		form()
	}

	if len(m.ptext.Vars) > 0 {
		b.WriteString(m.heading("Vars", w))
		names := make([]string, 0, len(m.ptext.Vars))
		for name := range m.ptext.Vars {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info(name, m.ptext.Vars[name])
		}
	}
	return b.String(), at
}

// settingLine is one row of a settings screen: the cursor, the label, the
// value and a note, lit across the width when the cursor is on it.
func settingLine(th theme.Theme, sel bool, label, value, note string, w int) string {
	style := func(s lipgloss.Style) lipgloss.Style {
		if sel {
			return th.OnSelection(s)
		}
		return s
	}
	line := cursor(th, sel) + style(th.Meta).Render(pad(label, configLabelWidth)) + style(th.ProjectName).Render(value)
	if note != "" {
		line += style(th.Path).Render("  " + note)
	}
	return fill(clipTo(line, w), w, style)
}

// nameButton is the project's name at the top of the pane, drawn as a bar
// button: it opens the project screen, and carries the key that does too.
// The line is a row like the ones under it, "Project" in the label column,
// and the key hint says what it does, as every bar button's does.
func (m Model) nameButton(name revier.ProjectName, w int) string {
	th := m.theme
	label, keys := th.Header, th.Help
	if m.over.kind == hoverName {
		label, keys = th.OnHover(label), th.OnHover(keys)
	}
	edit := m.keys.Edit.Help()
	return clipTo(th.Meta.Render(pad("Project", nameButtonStart))+label.Render(" "+string(name)+" ")+keys.Render(edit.Desc+" "+edit.Key+" "), w)
}

// nameButtonStart is the column the button starts on: the label column less
// the button's own leading space, so the name stands level with the values
// under it.
const nameButtonStart = detailLabelWidth - 1

// nameButtonWidth is the columns the button takes.
func (m Model) nameButtonWidth(name revier.ProjectName) int {
	edit := m.keys.Edit.Help()
	return lipgloss.Width(" " + string(name) + " " + edit.Desc + " " + edit.Key + " ")
}
