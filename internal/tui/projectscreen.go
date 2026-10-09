package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
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

// projectScreen is the project screen while it is up.
type projectScreen struct {
	name     revier.ProjectName // the project the screen edits
	text     config.ProjectText // its file as written
	row      int                // the row the cursor is on
	edit     textinput.Model    // a field's value, while it is typed
	tform    targetForm         // the target being added or changed, while its form is up
	dropping bool               // whether the target under the cursor waits on a y to be deleted
}

func newProjectScreen(th theme.Theme) projectScreen {
	return projectScreen{edit: newFieldInput(th)}
}

// projectResult is what a press on the screen leaves for the surface. The
// screen has written the project file; the surface takes the project as it
// now loads.
type projectResult struct {
	err     error         // the footer's
	closed  bool          // Esc on the rows: back to the surface
	written *core.Project // the project as its file now loads, under a new name after a rename
}

// openProject is alt+e and a click on the pane's name.
func (m Model) openProject() (tea.Model, tea.Cmd) {
	p, ok := m.highlighted()
	if !ok || p.File == "" {
		return m, nil
	}
	if err := m.proj.open(p, m.usable); err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.toList()
	m.body.SetYOffset(0)
	m.dialog = dialogProject
	return m, nil
}

// projectKey is every press on the project screen, and what it left for the
// surface: the project as its file now loads.
func (m Model) projectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	was := m.proj.name
	res, cmd := m.proj.key(m.surface(), m.renameRefusal(was), msg)
	m.err = res.err
	if p := res.written; p != nil {
		if p.Name != was {
			// A rename moved what state holds under the old name. What a
			// link's host said last is laid over the row at every refresh, so
			// it follows the name too: the row would otherwise lose its
			// agents until the host answers again.
			m.takeState()
			m.answers = m.answers.Renamed(was, p.Name)
		}
		m.replaceProject(was, *p)
	}
	if res.closed {
		m.dialog = dialogNone
		m.layout()
	}
	return m, cmd
}

// renameRefusal is why the project cannot be renamed now, or nil. A running
// project is refused: its instances are found by titles its name is part of.
func (m Model) renameRefusal(name revier.ProjectName) error {
	for _, v := range m.views {
		if v.Project.Name == name {
			if err := m.refuseRunning(v, "renaming"); err != nil {
				return err
			}
		}
	}
	return nil
}

// open reads the project's file for a new visit.
func (ps *projectScreen) open(p core.Project, usable []map[string]any) error {
	text, err := config.ReadProject(p.File, usable)
	if err != nil {
		return err
	}
	ps.name, ps.text, ps.row = p.Name, text, 0
	ps.edit.Blur()
	ps.tform = targetForm{}
	ps.dropping = false
	return nil
}

// fields are the first section's rows. A link has no directory or checkout
// here, only its name.
func (ps *projectScreen) fields() []projectField {
	if ps.text.Remote != nil {
		return []projectField{projName}
	}
	return []projectField{projName, projPath, projGitURL}
}

// targetRow is the target the cursor is on, if any.
func (ps *projectScreen) targetRow() (int, bool) {
	i := ps.row - len(ps.fields())
	return i, i >= 0 && i < len(ps.text.Targets)
}

// addTargetRow is the row that adds a target.
func (ps *projectScreen) addTargetRow() int {
	return len(ps.fields()) + len(ps.text.Targets)
}

// value is a field as the file has it.
func (ps *projectScreen) value(f projectField) string {
	switch f {
	case projPath:
		return ps.text.Path
	case projGitURL:
		return ps.text.GitURL
	}
	return string(ps.name)
}

// key is every press on the project screen. locked is why the project cannot
// be renamed now, nil when it can.
func (ps *projectScreen) key(sf surface, locked error, msg tea.KeyMsg) (projectResult, tea.Cmd) {
	switch {
	case ps.edit.Focused():
		return ps.editKey(sf, locked, msg)
	case ps.tform.open:
		form, cmd := ps.tform.key(sf, msg)
		if form.save != nil {
			return ps.saveTarget(sf, *form.save), cmd
		}
		return projectResult{err: form.err}, cmd
	case ps.dropping:
		return ps.dropTarget(sf, msg), nil
	}
	var res projectResult
	i, onTarget := ps.targetRow()
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back, sf.keys.Edit):
		res.closed = true
	case key.Matches(msg, sf.keys.Up):
		ps.row = max(ps.row-1, 0)
	case key.Matches(msg, sf.keys.Down):
		ps.row = min(ps.row+1, ps.addTargetRow())
	case onTarget && key.Matches(msg, sf.keys.Delete):
		pt := ps.text.Targets[i]
		if pt.Source == config.FromShared || pt.Source == config.Derived {
			res.err = fmt.Errorf("target %q is not in the project file; there is nothing to delete", pt.Target.Name)
			return res, nil
		}
		ps.dropping = true
	case onTarget && key.Matches(msg, sf.keys.Enter):
		return res, ps.openTargetForm(sf.theme, i)
	case ps.row == ps.addTargetRow() && key.Matches(msg, sf.keys.Enter):
		return res, ps.openTargetForm(sf.theme, len(ps.text.Targets))
	case key.Matches(msg, sf.keys.Enter):
		// As wide as the row leaves it, so a long path scrolls under the
		// cursor instead of running off the right edge, typed blind.
		ps.edit.Width = max(sf.list-lipgloss.Width(cursor(sf.theme, true))-configLabelWidth-1, 8)
		ps.edit.SetValue(ps.value(ps.fields()[ps.row]))
		ps.edit.CursorEnd()
		return res, ps.edit.Focus()
	}
	return res, nil
}

// editKey is a press while a field is typed. Enter writes it, Esc leaves it
// as it was.
func (ps *projectScreen) editKey(sf surface, locked error, msg tea.KeyMsg) (projectResult, tea.Cmd) {
	res := projectResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		ps.edit.Blur()
		return res, nil
	case key.Matches(msg, sf.keys.Enter):
		res = ps.saveField(sf, locked, ps.fields()[ps.row], strings.TrimSpace(ps.edit.Value()))
		if res.err == nil {
			ps.edit.Blur()
		}
		return res, nil
	case altRune(msg):
		return res, nil
	}
	in, cmd := ps.edit.Update(msg)
	ps.edit = in
	return res, cmd
}

// saveField writes a field's value to the project file. The name is the
// file's own, so a new one moves the file.
func (ps *projectScreen) saveField(sf surface, locked error, f projectField, value string) projectResult {
	if f == projName {
		return ps.rename(sf, locked, revier.ProjectName(value))
	}
	if value == ps.value(f) {
		return projectResult{}
	}
	p, _ := projectNamed(sf.projects, ps.name)
	written, err := config.SetProjectValue(p.File, projectFieldKeys[f], value, sf.usable)
	if err != nil {
		return projectResult{err: err}
	}
	return ps.took(sf, written)
}

// rename moves the project file, and what state and the saved sessions hold
// under the old name with it.
func (ps *projectScreen) rename(sf surface, locked error, to revier.ProjectName) projectResult {
	from := ps.name
	if to == from {
		return projectResult{}
	}
	if locked != nil {
		return projectResult{err: locked}
	}
	var written core.Project
	err := withConfigRoot(func(root string) (err error) {
		written, err = app.RenameProject(root, sf.stateRoot, sf.core.Ledger, from, to, sf.usable)
		return err
	})
	if err != nil {
		return projectResult{err: err}
	}
	return ps.took(sf, written)
}

// took takes what a write to the project file left: the file as written for
// the screen, and the project for the surface.
func (ps *projectScreen) took(sf surface, p core.Project) projectResult {
	res := projectResult{written: &p}
	ps.name = p.Name
	text, err := config.ReadProject(p.File, sf.usable)
	if err != nil {
		res.err = err
		return res
	}
	ps.text = text
	return res
}

// openTargetForm opens the form for the project's target i, or for a new one
// when i is past the last. A derived target is not in the file, so saving it
// declares one.
func (ps *projectScreen) openTargetForm(th theme.Theme, i int) tea.Cmd {
	f := newTargetForm(th, i, nil)
	if i < len(ps.text.Targets) {
		pt := ps.text.Targets[i]
		f = newTargetForm(th, i, &pt.Target)
		if pt.Source != config.Derived {
			f.was = pt.Target.Name
		}
		if pt.Shared != nil {
			v := valuesOf(*pt.Shared)
			f.shared = &v
		}
	}
	ps.tform = f
	return ps.tform.fields[tfName].Focus()
}

// saveTarget writes the form's target to the project file.
func (ps *projectScreen) saveTarget(sf surface, edit config.TargetEdit) projectResult {
	p, _ := projectNamed(sf.projects, ps.name)
	written, err := config.SaveProjectTarget(p.File, ps.tform.was, edit, sf.usable)
	if err != nil {
		return projectResult{err: err}
	}
	res := ps.took(sf, written)
	if res.err != nil {
		return res
	}
	ps.tform = targetForm{}
	for i, pt := range ps.text.Targets {
		if pt.Target.Name == edit.Target.Name {
			ps.row = len(ps.fields()) + i
		}
	}
	return res
}

// dropTarget takes the key that answers the delete question. Only y deletes
// the file's entry: a target of the project's own goes, and an override
// leaves config.toml's target as it is there.
func (ps *projectScreen) dropTarget(sf surface, msg tea.KeyMsg) projectResult {
	ps.dropping = false
	i, ok := ps.targetRow()
	if msg.String() != "y" || !ok {
		return projectResult{err: sf.err}
	}
	p, _ := projectNamed(sf.projects, ps.name)
	written, err := config.RemoveProjectTarget(p.File, ps.text.Targets[i].Target.Name, sf.usable)
	if err != nil {
		return projectResult{err: err}
	}
	res := ps.took(sf, written)
	if res.err != nil {
		return res
	}
	res.err = sf.err
	ps.row = min(ps.row, ps.addTargetRow())
	return res
}

// dropPrompt is the delete question for the target under the cursor.
func (ps *projectScreen) dropPrompt(th theme.Theme) string {
	question := ""
	if i, ok := ps.targetRow(); ok {
		pt := ps.text.Targets[i]
		question = fmt.Sprintf("delete target %q?", pt.Target.Name)
		if pt.Source == config.Overridden {
			question = fmt.Sprintf("drop this project's changes to target %q?", pt.Target.Name)
		}
	}
	return th.Attention.Render(" "+question+"  ") +
		th.Help.Render("y: delete · any other key: keep")
}

// help is the footer on the project screen.
func (ps *projectScreen) help(k keyMap) []key.Binding {
	_, onTarget := ps.targetRow()
	switch {
	case ps.edit.Focused():
		return k.helpForConfig(configTyping)
	case ps.tform.open:
		return k.helpForConfig(ps.tform.helpKind())
	case onTarget:
		return k.helpForConfig(configOnAction)
	case ps.row == ps.addTargetRow():
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

// screen stands in the list's place while the screen is up, and the line the
// cursor is on, which the body keeps in view.
func (ps *projectScreen) screen(sf surface) (string, int) {
	th := sf.theme
	w := sf.list
	var b strings.Builder
	at := 0
	row := func(r int, label, value, note string) {
		// While a form is open its own cursor is the one on screen.
		sel := ps.row == r && !ps.tform.open
		if sel {
			at = strings.Count(b.String(), "\n")
		}
		b.WriteString(settingLine(th, sel, label, value, note, w) + "\n")
	}
	info := func(label, value string) {
		b.WriteString(clipTo(th.Path.Render("  ")+th.Meta.Render(pad(label, configLabelWidth))+th.NameDim.Render(value), w) + "\n")
	}

	b.WriteString(heading(th, "Project", w))
	labels := map[projectField]string{projName: "name", projPath: "path", projGitURL: "git url"}
	notes := map[projectField]string{
		projName:   "the file's name; renaming moves the file",
		projPath:   "the directory",
		projGitURL: "where Enter clones a missing directory from",
	}
	for r, f := range ps.fields() {
		value := ps.value(f)
		if ps.edit.Focused() && ps.row == r {
			value = ps.edit.View()
		}
		row(r, labels[f], value, notes[f])
	}
	if l := ps.text.Remote; l != nil {
		info("host", l.Host)
		info("name there", string(l.Project))
		if ps.text.Path != "" {
			info("path there", ps.text.Path)
		}
		if ps.text.GitURL != "" {
			info("git url there", ps.text.GitURL)
		}
	}

	b.WriteString(heading(th, "Targets", w))
	base := len(ps.fields())
	form := func() {
		lines, line := ps.tform.lines(th, w)
		at = strings.Count(b.String(), "\n") + line
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, pt := range ps.text.Targets {
		row(base+i, keyLabel(pt.Target.Key), string(pt.Target.Name), sourceNote(pt.Source)+" · "+describeTarget(pt.Target))
		if ps.tform.open && ps.tform.index == i {
			form()
		}
	}
	row(ps.addTargetRow(), "+", "add a target", "this project only")
	if ps.tform.open && ps.tform.index == len(ps.text.Targets) {
		form()
	}

	if len(ps.text.Vars) > 0 {
		b.WriteString(heading(th, "Vars", w))
		names := make([]string, 0, len(ps.text.Vars))
		for name := range ps.text.Vars {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			info(name, ps.text.Vars[name])
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
	text, hint := m.nameButtonText(name)
	return clipTo(th.Meta.Render(pad("Project", nameButtonStart))+label.Render(text)+keys.Render(hint), w)
}

// nameButtonText is the button's two runs, the name and the key hint after
// it. The width the pointer is matched against is read off the same text,
// so the two cannot disagree.
func (m Model) nameButtonText(name revier.ProjectName) (text, hint string) {
	edit := m.keys.Edit.Help()
	return " " + string(name) + " ", edit.Desc + " " + edit.Key + " "
}

// nameButtonStart is the column the button starts on: the label column less
// the button's own leading space, so the name stands level with the values
// under it.
const nameButtonStart = detailLabelWidth - 1

// nameButtonWidth is the columns the button takes.
func (m Model) nameButtonWidth(name revier.ProjectName) int {
	text, hint := m.nameButtonText(name)
	return lipgloss.Width(text + hint)
}
