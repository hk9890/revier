package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

// The targets section of the config screen: the shared targets of
// config.toml (decisions.md D57), one row each, and a row that adds one.
// Enter opens a target's form: its name, key and home flag, then its runtime
// and its window realization, each launch, match and place, and the
// runtime's panels as a list with a form of their own. alt+d deletes a
// target after a y. A change is written as it is saved
// (config.AddTarget, ReplaceTarget, RemoveTarget), and the projects are
// loaded again with it, so the surface has the new targets at once.

// The text fields of a target form.
const (
	tfName = iota
	tfKey
	tfRuntimeName
	tfRuntimeCommand
	tfRuntimeTitle
	tfRuntimeClass
	tfRuntimePlace
	tfWindowName
	tfWindowCommand
	tfWindowTitle
	tfWindowClass
	tfWindowPlace
	targetFields
)

// formRowKind is what a row of the target form holds.
type formRowKind int

const (
	rowField    formRowKind = iota // a text field
	rowHome                        // the home flag
	rowPanel                       // one panel of the runtime
	rowAddPanel                    // the row that adds a panel
)

// formRow is one row of the target form. section names the heading that
// stands above it, when it is the first row of one.
type formRow struct {
	kind    formRowKind
	field   int // the text field, for rowField; the panel, for rowPanel
	label   string
	note    string
	section string
}

// panelDraft is a panel as the form holds it, and the index it has in the
// file, or -1 for a new one.
type panelDraft struct {
	spec revier.PanelSpec
	from int
}

// targetForm is a shared target being added or changed.
type targetForm struct {
	open   bool
	index  int           // the target changed, or len(targets) for a new one
	base   revier.Target // the target as read, for the values the form does not show
	cursor int           // the row the cursor is on
	home   bool
	fields [targetFields]textinput.Model
	panels []panelDraft
	panel  panelForm
}

// The fields of the panel form.
const (
	pfKind = iota
	pfTitle
	pfCommand
	panelFields
)

// panelForm is one panel being added or changed, inside a target form.
type panelForm struct {
	open    bool
	index   int // the panel changed, or len(panels) for a new one
	field   int
	kind    revier.PanelKind
	title   textinput.Model
	command textinput.Model
}

var panelKinds = []string{string(revier.PanelAgent), string(revier.PanelShell), string(revier.PanelTool)}

// targetRow is the shared target the config screen's cursor is on, if any.
func (m Model) targetRow() (int, bool) {
	i := m.crow - int(configRows)
	return i, i >= 0 && i < len(m.targets)
}

// addTargetRow is the row that adds a shared target.
func (m Model) addTargetRow() int { return int(configRows) + len(m.targets) }

// rows is the target form's rows, in the order the cursor walks them.
func (f targetForm) rows() []formRow {
	out := []formRow{
		{kind: rowField, field: tfName, label: "name"},
		{kind: rowField, field: tfKey, label: "key", note: "a desktop key; empty for none"},
		{kind: rowHome, label: "home", note: "the target Enter opens and toggle-back returns to"},
		{kind: rowField, field: tfRuntimeName, label: "name", section: "runtime", note: "the window or tab title it opens with"},
		{kind: rowField, field: tfRuntimeCommand, label: "command", note: "empty when the panels are what starts"},
		{kind: rowField, field: tfRuntimeTitle, label: "match title", note: "a regexp"},
		{kind: rowField, field: tfRuntimeClass, label: "match class", note: "a regexp"},
		{kind: rowField, field: tfRuntimePlace, label: "place", note: "x y width height"},
	}
	for i := range f.panels {
		out = append(out, formRow{kind: rowPanel, field: i, label: "panel"})
	}
	out = append(out,
		formRow{kind: rowAddPanel, label: "+"},
		formRow{kind: rowField, field: tfWindowName, label: "name", section: "window", note: "the class a browser is started with"},
		formRow{kind: rowField, field: tfWindowCommand, label: "command"},
		formRow{kind: rowField, field: tfWindowTitle, label: "match title", note: "a regexp"},
		formRow{kind: rowField, field: tfWindowClass, label: "match class", note: "a regexp"},
		formRow{kind: rowField, field: tfWindowPlace, label: "place", note: "x y width height"},
	)
	return out
}

// openTargetForm opens the form for shared target i, or for a new one when i
// is past the last.
func (m Model) openTargetForm(i int) (tea.Model, tea.Cmd) {
	f := targetForm{open: true, index: i}
	for j := range f.fields {
		f.fields[j] = m.formInput("")
	}
	if i < len(m.targets) {
		t := m.targets[i]
		f.base = t
		f.home = t.Home
		f.fields[tfName].SetValue(string(t.Name))
		f.fields[tfKey].SetValue(t.Key)
		fill := func(r *revier.Realization, name, command, title, class, place int) {
			if r == nil {
				return
			}
			f.fields[name].SetValue(r.Name)
			f.fields[command].SetValue(joinCommand(r.Launch))
			f.fields[title].SetValue(r.Match.Title)
			f.fields[class].SetValue(r.Match.Class)
			f.fields[place].SetValue(r.Place)
		}
		fill(t.Runtime, tfRuntimeName, tfRuntimeCommand, tfRuntimeTitle, tfRuntimeClass, tfRuntimePlace)
		fill(t.Window, tfWindowName, tfWindowCommand, tfWindowTitle, tfWindowClass, tfWindowPlace)
		if t.Runtime != nil {
			for j, p := range t.Runtime.Panels {
				f.panels = append(f.panels, panelDraft{spec: p, from: j})
			}
		}
	}
	m.err = nil
	m.tform = f
	return m, m.tform.fields[tfName].Focus()
}

func (m Model) formInput(placeholder string) textinput.Model {
	in := textinput.New()
	styleField(&in, m.theme)
	in.Prompt = ""
	in.Placeholder = placeholder
	return in
}

// targetFormKey is a press while the target form is up.
func (m Model) targetFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.tform.panel.open {
		return m.panelFormKey(msg)
	}
	rows := m.tform.rows()
	row := rows[m.tform.cursor]
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.tform = targetForm{}
		return m, nil
	case key.Matches(msg, m.keys.Targets, m.keys.Down):
		return m.moveTargetCursor(+1)
	case msg.Type == tea.KeyShiftTab, key.Matches(msg, m.keys.Up):
		return m.moveTargetCursor(-1)
	case row.kind == rowPanel && key.Matches(msg, m.keys.Delete):
		m.tform.panels = slices.Delete(slices.Clone(m.tform.panels), row.field, row.field+1)
		return m, nil
	case row.kind == rowPanel && key.Matches(msg, m.keys.Enter):
		return m.openPanelForm(row.field)
	case row.kind == rowAddPanel && key.Matches(msg, m.keys.Enter):
		return m.openPanelForm(len(m.tform.panels))
	case key.Matches(msg, m.keys.Enter):
		return m.saveTarget()
	case row.kind == rowHome && (msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight || msg.Type == tea.KeySpace):
		m.tform.home = !m.tform.home
		return m, nil
	case row.kind == rowField:
		in, cmd := m.tform.fields[row.field].Update(msg)
		m.tform.fields[row.field] = in
		return m, cmd
	}
	return m, nil
}

// moveTargetCursor moves the form's cursor a row, and the typing with it.
func (m Model) moveTargetCursor(step int) (tea.Model, tea.Cmd) {
	rows := m.tform.rows()
	if r := rows[m.tform.cursor]; r.kind == rowField {
		m.tform.fields[r.field].Blur()
	}
	m.tform.cursor = min(max(m.tform.cursor+step, 0), len(rows)-1)
	if r := rows[m.tform.cursor]; r.kind == rowField {
		return m, m.tform.fields[r.field].Focus()
	}
	return m, nil
}

// saveTarget writes the form's target, and loads the projects again with it.
func (m Model) saveTarget() (tea.Model, tea.Cmd) {
	t, from, err := m.tform.target()
	if err != nil {
		m.err = err
		return m, nil
	}
	i := m.tform.index
	for j, other := range m.targets {
		if j != i && other.Name == t.Name {
			m.err = fmt.Errorf("a shared target named %q exists already", t.Name)
			return m, nil
		}
	}
	var written config.TargetsWritten
	if i == len(m.targets) {
		err = withConfigRoot(func(root string) (err error) {
			written, err = config.AddTarget(root, m.shared, t)
			return err
		})
	} else {
		err = withConfigRoot(func(root string) (err error) {
			written, err = config.ReplaceTarget(root, i, m.shared, config.TargetEdit{Target: t, PanelFrom: from})
			return err
		})
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.tform = targetForm{}
	m.setTargets(written)
	m.crow = int(configRows) + i
	return m, nil
}

// target is the target the form holds, built on the one it was opened with,
// and where each of its panels came from.
func (f targetForm) target() (revier.Target, []int, error) {
	t := f.base
	t.Name = revier.TargetName(strings.TrimSpace(f.fields[tfName].Value()))
	t.Key = strings.TrimSpace(f.fields[tfKey].Value())
	t.Home = f.home
	if t.Name == "" {
		return t, nil, errors.New("a target needs a name")
	}
	var panels []revier.PanelSpec
	var from []int
	for _, p := range f.panels {
		panels = append(panels, p.spec)
		from = append(from, p.from)
	}
	var err error
	if t.Runtime, err = f.realization(f.base.Runtime, tfRuntimeName, panels); err != nil {
		return t, nil, fmt.Errorf("runtime: %w", err)
	}
	if t.Window, err = f.realization(f.base.Window, tfWindowName, nil); err != nil {
		return t, nil, fmt.Errorf("window: %w", err)
	}
	if t.Runtime == nil && t.Window == nil {
		return t, nil, errors.New("a target needs a runtime or a window")
	}
	return t, from, nil
}

// realization is one realization as the form holds it, from its five fields
// starting at first: none when every field is empty and it has no panels.
func (f targetForm) realization(old *revier.Realization, first int, panels []revier.PanelSpec) (*revier.Realization, error) {
	value := func(field int) string { return strings.TrimSpace(f.fields[first+field].Value()) }
	command, err := splitCommand(value(1))
	if err != nil {
		return nil, err
	}
	if value(0) == "" && len(command) == 0 && value(2) == "" && value(3) == "" && value(4) == "" && len(panels) == 0 {
		return nil, nil
	}
	r := revier.Realization{}
	if old != nil {
		r = *old
	}
	r.Name, r.Launch, r.Place, r.Panels = value(0), command, value(4), panels
	r.Match.Title, r.Match.Class = value(2), value(3)
	return &r, nil
}

// openPanelForm opens the form for panel i of the target form, or for a new
// panel when i is past the last.
func (m Model) openPanelForm(i int) (tea.Model, tea.Cmd) {
	p := panelForm{open: true, index: i, kind: revier.PanelShell, title: m.formInput("shell"), command: m.formInput("empty for a shell")}
	if i < len(m.tform.panels) {
		spec := m.tform.panels[i].spec
		p.kind = spec.Kind
		p.title.SetValue(spec.Title)
		p.command.SetValue(joinCommand(spec.Command))
	}
	m.tform.panel = p
	return m, nil
}

// panelFormKey is a press while a panel is edited. Enter keeps the panel in
// the target form, which is written when the target is saved.
func (m Model) panelFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.tform.panel
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.tform.panel = panelForm{}
		return m, nil
	case key.Matches(msg, m.keys.Targets, m.keys.Down), msg.Type == tea.KeyShiftTab, key.Matches(msg, m.keys.Up):
		step := 1
		if msg.Type == tea.KeyShiftTab || key.Matches(msg, m.keys.Up) {
			step = -1
		}
		p.title.Blur()
		p.command.Blur()
		p.field = (p.field + step + panelFields) % panelFields
		switch p.field {
		case pfTitle:
			return m, p.title.Focus()
		case pfCommand:
			return m, p.command.Focus()
		}
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		command, err := splitCommand(p.command.Value())
		if err != nil {
			m.err = err
			return m, nil
		}
		spec := revier.PanelSpec{Kind: p.kind, Title: strings.TrimSpace(p.title.Value()), Command: command}
		panels := slices.Clone(m.tform.panels)
		if p.index < len(panels) {
			panels[p.index].spec = spec
		} else {
			panels = append(panels, panelDraft{spec: spec, from: -1})
		}
		m.err = nil
		m.tform.panels = panels
		m.tform.panel = panelForm{}
		return m, nil
	case p.field == pfKind && (msg.Type == tea.KeyLeft || msg.Type == tea.KeySpace):
		p.kind = revier.PanelKind(cycle(panelKinds, string(p.kind), -1))
	case p.field == pfKind && msg.Type == tea.KeyRight:
		p.kind = revier.PanelKind(cycle(panelKinds, string(p.kind), +1))
	case p.field == pfTitle:
		var cmd tea.Cmd
		p.title, cmd = p.title.Update(msg)
		return m, cmd
	case p.field == pfCommand:
		var cmd tea.Cmd
		p.command, cmd = p.command.Update(msg)
		return m, cmd
	}
	return m, nil
}

// confirmDropTarget takes the key that answers the delete question for a
// shared target. Only y deletes, and a delete a project would not load
// without is refused and named.
func (m Model) confirmDropTarget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dropping = false
	i, ok := m.targetRow()
	if msg.String() != "y" || !ok {
		return m, nil
	}
	var written config.TargetsWritten
	err := withConfigRoot(func(root string) (err error) {
		written, err = config.RemoveTarget(root, i, m.shared)
		return err
	})
	if err != nil {
		m.err = err
		return m, nil
	}
	m.setTargets(written)
	return m, nil
}

// setTargets takes what a change to the shared targets left: the targets for
// the screen, and every project loaded with them, for the surface.
func (m *Model) setTargets(w config.TargetsWritten) {
	m.shared = w.Shared
	m.targets, _ = config.DecodeTargets(w.Shared)
	m.projects = w.Projects
	m.tkeys = targetKeys(m.projects, m.keys)
}

// describeTarget is a shared target's row note: where it opens.
func describeTarget(t revier.Target) string {
	var parts []string
	if r := t.Runtime; r != nil {
		switch {
		case len(r.Panels) == 1:
			parts = append(parts, "runtime, 1 panel")
		case len(r.Panels) > 1:
			parts = append(parts, fmt.Sprintf("runtime, %d panels", len(r.Panels)))
		default:
			parts = append(parts, "runtime: "+joinCommand(r.Launch))
		}
	}
	if r := t.Window; r != nil {
		parts = append(parts, "window: "+joinCommand(r.Launch))
	}
	if t.Home {
		parts = append(parts, "home")
	}
	return strings.Join(parts, " · ")
}

// targetFormLines is the target form, one line a row with a heading above
// each section, and the line the cursor is on.
func (m Model) targetFormLines(w int) ([]string, int) {
	th := m.theme
	f := m.tform
	var out []string
	at := 0
	for r, row := range f.rows() {
		if row.section != "" {
			out = append(out, th.Path.Render("  ")+th.NameDim.Render(row.section))
		}
		sel := r == f.cursor
		if sel {
			at = len(out)
		}
		if row.kind == rowPanel && f.panel.open && f.panel.index == row.field ||
			row.kind == rowAddPanel && f.panel.open && f.panel.index == len(f.panels) {
			lines, field := m.panelFormLines(w)
			at = len(out) + field
			out = append(out, lines...)
			continue
		}
		style := func(s lipgloss.Style) lipgloss.Style {
			if sel && !f.panel.open {
				return th.OnSelection(s)
			}
			return s
		}
		var value string
		switch row.kind {
		case rowField:
			value = f.fields[row.field].View()
		case rowHome:
			value = style(th.ProjectName).Render("‹ no ›")
			if f.home {
				value = style(th.ProjectName).Render("‹ yes ›")
			}
		case rowPanel:
			p := f.panels[row.field].spec
			value = style(th.ProjectName).Render(pad(string(p.Kind), 6) + " " + p.Title)
			if len(p.Command) > 0 {
				value += style(th.Path).Render("  " + joinCommand(p.Command))
			}
		case rowAddPanel:
			value = style(th.ProjectName).Render("add a panel")
		}
		line := "  " + cursor(th, sel && !f.panel.open) + style(th.Meta).Render(pad(row.label, configLabelWidth)) + value
		if row.note != "" && row.kind != rowPanel {
			line += style(th.Path).Render("  " + row.note)
		}
		out = append(out, fill(clipTo(line, w), w, style))
	}
	return out, at
}

// panelFormLines is the panel form, and the line the cursor is on.
func (m Model) panelFormLines(w int) ([]string, int) {
	th := m.theme
	p := m.tform.panel
	values := []string{th.ProjectName.Render("‹ " + string(p.kind) + " ›"), p.title.View(), p.command.View()}
	labels := []string{"kind", "title", "command"}
	out := make([]string, panelFields)
	for j := range out {
		out[j] = clipTo("    "+cursor(th, j == p.field)+th.Meta.Render(pad(labels[j], configLabelWidth))+values[j], w)
	}
	return out, p.field
}
