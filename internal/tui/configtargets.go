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
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The targets section of the config screen: the shared targets of
// config.toml (decisions.md D59), one row each, and a row that adds one.
// Enter opens a target's form: its name, key and home flag, then its runtime
// and its window realization, each launch, match and place, and the
// runtime's panels as a list with a form of their own. alt+d deletes a
// target after a y. A change is written as it is saved
// (config.AddTarget, ReplaceTarget, RemoveTarget), and the projects are
// loaded again with it, so the surface has the new targets at once. The
// project screen opens the same form on a project's targets
// (projectscreen.go).

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

// targetForm is a target being added or changed: a shared one on the config
// screen, or a project's on the project screen.
type targetForm struct {
	open   bool
	index  int           // the target changed, or one past the last for a new one
	base   revier.Target // the target as read, for the values the form does not show
	cursor int           // the row the cursor is on
	home   bool
	fields [targetFields]textinput.Model
	panels []panelDraft
	panel  panelForm

	// On the project screen: the name the project file has the target
	// under, empty for none, and the shared target it is or overrides.
	was    revier.TargetName
	shared *targetValues
}

// targetValues is a target as the form's rows show it.
type targetValues struct {
	home   bool
	fields [targetFields]string
	panels []revier.PanelSpec
}

func valuesOf(t revier.Target) targetValues {
	v := targetValues{home: t.Home}
	v.fields[tfName], v.fields[tfKey] = string(t.Name), t.Key
	fill := func(r *revier.Realization, name, command, title, class, place int) {
		if r == nil {
			return
		}
		v.fields[name] = r.Name
		v.fields[command] = joinCommand(r.Launch)
		v.fields[title] = r.Match.Title
		v.fields[class] = r.Match.Class
		v.fields[place] = r.Place
	}
	fill(t.Runtime, tfRuntimeName, tfRuntimeCommand, tfRuntimeTitle, tfRuntimeClass, tfRuntimePlace)
	fill(t.Window, tfWindowName, tfWindowCommand, tfWindowTitle, tfWindowClass, tfWindowPlace)
	if t.Runtime != nil {
		v.panels = t.Runtime.Panels
	}
	return v
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
func (cs *configScreen) targetRow(sf surface) (int, bool) {
	i := cs.row - int(configRows)
	return i, i >= 0 && i < len(sf.targets)
}

// addTargetRow is the row that adds a shared target.
func (cs *configScreen) addTargetRow(sf surface) int { return int(configRows) + len(sf.targets) }

// openTargetForm opens the form for shared target i, or for a new one when i
// is past the last.
func (cs *configScreen) openTargetForm(sf surface, i int) tea.Cmd {
	var t *revier.Target
	if i < len(sf.targets) {
		t = &sf.targets[i]
	}
	cs.tform = newTargetForm(sf.theme, i, t)
	return cs.tform.fields[tfName].Focus()
}

// saveTarget writes the form's target, and hands back the shared targets and
// the projects loaded again with it.
func (cs *configScreen) saveTarget(sf surface, edit config.TargetEdit) configResult {
	i, t := cs.tform.index, edit.Target
	for j, other := range sf.targets {
		if j != i && other.Name == t.Name {
			return configResult{err: fmt.Errorf("a shared target named %q exists already", t.Name)}
		}
	}
	var written config.TargetsWritten
	err := withConfigRoot(func(root string) (err error) {
		if i == len(sf.targets) {
			written, err = config.AddTarget(root, sf.shared, t)
			return err
		}
		written, err = config.ReplaceTarget(root, i, sf.shared, edit)
		return err
	})
	if err != nil {
		return configResult{err: err}
	}
	cs.tform = targetForm{}
	cs.row = int(configRows) + i
	return configResult{targets: &written}
}

// dropTarget takes the key that answers the delete question for a shared
// target. Only y deletes, and a delete a project would not load without is
// refused and named.
func (cs *configScreen) dropTarget(sf surface, msg tea.KeyMsg) configResult {
	res := configResult{err: sf.err}
	cs.dropping = false
	i, ok := cs.targetRow(sf)
	if msg.String() != "y" || !ok {
		return res
	}
	var written config.TargetsWritten
	err := withConfigRoot(func(root string) (err error) {
		written, err = config.RemoveTarget(root, i, sf.shared)
		return err
	})
	if err != nil {
		res.err = err
		return res
	}
	res.targets = &written
	return res
}

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

// newTargetForm is the form at index for t, or for a new target when t is
// nil.
func newTargetForm(th theme.Theme, index int, t *revier.Target) targetForm {
	f := targetForm{open: true, index: index}
	for j := range f.fields {
		f.fields[j] = formInput(th, "")
	}
	if t == nil {
		return f
	}
	f.base = *t
	v := valuesOf(*t)
	f.home = v.home
	for j, value := range v.fields {
		f.fields[j].SetValue(value)
	}
	for j, p := range v.panels {
		f.panels = append(f.panels, panelDraft{spec: p, from: j})
	}
	return f
}

// formInput is a field of a form: no prompt, and a placeholder.
func formInput(th theme.Theme, placeholder string) textinput.Model {
	in := textinput.New()
	styleField(&in, th)
	in.Prompt = ""
	in.Placeholder = placeholder
	return in
}

// formResult is what a press in the target form leaves for the screen it is
// on.
type formResult struct {
	err  error              // the footer's
	save *config.TargetEdit // Enter: the target to write, which the screen does
}

// key is a press while the target form is up. Esc closes it; Enter hands the
// target to the screen, which writes it and closes the form.
func (f *targetForm) key(sf surface, msg tea.KeyMsg) (formResult, tea.Cmd) {
	if f.panel.open {
		return f.panelKey(sf, msg)
	}
	res := formResult{err: sf.err}
	rows := f.rows()
	row := rows[f.cursor]
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		*f = targetForm{}
		return formResult{}, nil
	case key.Matches(msg, sf.keys.Next, sf.keys.Down):
		return res, f.move(+1)
	case msg.Type == tea.KeyShiftTab, key.Matches(msg, sf.keys.Up):
		return res, f.move(-1)
	case row.kind == rowPanel && key.Matches(msg, sf.keys.Delete):
		f.panels = slices.Delete(slices.Clone(f.panels), row.field, row.field+1)
	case row.kind == rowPanel && key.Matches(msg, sf.keys.Enter):
		f.openPanel(sf.theme, row.field)
	case row.kind == rowAddPanel && key.Matches(msg, sf.keys.Enter):
		f.openPanel(sf.theme, len(f.panels))
	case key.Matches(msg, sf.keys.Enter):
		t, from, err := f.target()
		if err != nil {
			res.err = err
			return res, nil
		}
		res.save = &config.TargetEdit{Target: t, PanelFrom: from}
	case row.kind == rowHome && (msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight || msg.Type == tea.KeySpace):
		f.home = !f.home
	case row.kind == rowField && row.field == tfName && f.shared != nil:
		// A shared target is found by its name; renamed, it would be another.
	case row.kind == rowField && !altRune(msg):
		in, cmd := f.fields[row.field].Update(msg)
		f.fields[row.field] = in
		return res, cmd
	}
	return res, nil
}

// move moves the form's cursor a row, and the typing with it.
func (f *targetForm) move(step int) tea.Cmd {
	rows := f.rows()
	if r := rows[f.cursor]; r.kind == rowField {
		f.fields[r.field].Blur()
	}
	f.cursor = min(max(f.cursor+step, 0), len(rows)-1)
	if r := rows[f.cursor]; r.kind == rowField {
		return f.fields[r.field].Focus()
	}
	return nil
}

// helpKind is what the form's cursor is on, which decides the footer.
func (f targetForm) helpKind() configHelp {
	if f.panel.open {
		return configInPanelForm
	}
	switch f.rows()[f.cursor].kind {
	case rowPanel:
		return configOnFormPanel
	case rowAddPanel:
		return configOnAdd
	case rowHome:
		return configOnFormHome
	}
	return configInTargetForm
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

// openPanel opens the form for panel i of the target form, or for a new
// panel when i is past the last.
func (f *targetForm) openPanel(th theme.Theme, i int) {
	p := panelForm{open: true, index: i, kind: revier.PanelShell, title: formInput(th, "shell"), command: formInput(th, "empty for a shell")}
	if i < len(f.panels) {
		spec := f.panels[i].spec
		p.kind = spec.Kind
		p.title.SetValue(spec.Title)
		p.command.SetValue(joinCommand(spec.Command))
	}
	f.panel = p
}

// panelKey is a press while a panel is edited. Enter keeps the panel in the
// target form, which is written when the target is saved.
func (f *targetForm) panelKey(sf surface, msg tea.KeyMsg) (formResult, tea.Cmd) {
	res := formResult{err: sf.err}
	p := &f.panel
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		f.panel = panelForm{}
		return formResult{}, nil
	case key.Matches(msg, sf.keys.Next, sf.keys.Down), msg.Type == tea.KeyShiftTab, key.Matches(msg, sf.keys.Up):
		step := 1
		if msg.Type == tea.KeyShiftTab || key.Matches(msg, sf.keys.Up) {
			step = -1
		}
		p.title.Blur()
		p.command.Blur()
		p.field = (p.field + step + panelFields) % panelFields
		switch p.field {
		case pfTitle:
			return res, p.title.Focus()
		case pfCommand:
			return res, p.command.Focus()
		}
		return res, nil
	case key.Matches(msg, sf.keys.Enter):
		command, err := splitCommand(p.command.Value())
		if err != nil {
			return formResult{err: err}, nil
		}
		spec := revier.PanelSpec{Kind: p.kind, Title: strings.TrimSpace(p.title.Value()), Command: command}
		panels := slices.Clone(f.panels)
		if p.index < len(panels) {
			panels[p.index].spec = spec
		} else {
			panels = append(panels, panelDraft{spec: spec, from: -1})
		}
		f.panels = panels
		f.panel = panelForm{}
		return formResult{}, nil
	case p.field == pfKind && (msg.Type == tea.KeyLeft || msg.Type == tea.KeySpace):
		p.kind = revier.PanelKind(cycle(panelKinds, string(p.kind), -1))
	case p.field == pfKind && msg.Type == tea.KeyRight:
		p.kind = revier.PanelKind(cycle(panelKinds, string(p.kind), +1))
	case altRune(msg):
	case p.field == pfTitle:
		var cmd tea.Cmd
		p.title, cmd = p.title.Update(msg)
		return res, cmd
	case p.field == pfCommand:
		var cmd tea.Cmd
		p.command, cmd = p.command.Update(msg)
		return res, cmd
	}
	return res, nil
}

// describeTarget is a target's row note, on either screen: where it opens. A
// tab says the target it opens inside, and a target that lists its tabs says
// them in the place of what it starts.
func describeTarget(t revier.Target) string {
	var parts []string
	if r := t.Runtime; r != nil {
		where := "runtime"
		if r.Inside != "" {
			where = "tab of " + string(r.Inside)
		}
		switch {
		case len(r.Tabs) > 0:
			tabs := make([]string, len(r.Tabs))
			for i, tab := range r.Tabs {
				tabs[i] = string(tab)
			}
			parts = append(parts, where+", tabs: "+strings.Join(tabs, ", "))
		case len(r.Panels) == 1:
			parts = append(parts, where+", 1 panel")
		case len(r.Panels) > 1:
			parts = append(parts, fmt.Sprintf("%s, %d panels", where, len(r.Panels)))
		default:
			parts = append(parts, where+": "+joinCommand(r.Launch))
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

// lines is the target form, one line a row with a heading above each
// section, and the line the cursor is on.
func (f targetForm) lines(th theme.Theme, w int) ([]string, int) {
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
			lines, field := f.panel.lines(th, w)
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
		if note := f.note(row); note != "" {
			line += style(th.Path).Render("  " + note)
		}
		out = append(out, fill(clipTo(line, w), w, style))
	}
	return out, at
}

// note is what a row says beside its value. Where the target is config.toml's,
// that is config.toml's value: that it is the one in use, or what it would be
// without the project's.
func (f targetForm) note(row formRow) string {
	if f.shared == nil {
		if row.kind == rowPanel {
			return ""
		}
		return row.note
	}
	var same bool
	var value string
	switch row.kind {
	case rowField:
		value = f.shared.fields[row.field]
		same = strings.TrimSpace(f.fields[row.field].Value()) == value
	case rowHome:
		value, same = "no", f.home == f.shared.home
		if f.shared.home {
			value = "yes"
		}
	case rowAddPanel:
		panels := make([]revier.PanelSpec, len(f.panels))
		for j, p := range f.panels {
			panels[j] = p.spec
		}
		same = slices.EqualFunc(panels, f.shared.panels, func(a, b revier.PanelSpec) bool {
			return a.Kind == b.Kind && a.Title == b.Title && slices.Equal(a.Command, b.Command)
		})
		value = fmt.Sprintf("%d panels", len(f.shared.panels))
	default:
		return ""
	}
	switch {
	case row.kind == rowField && row.field == tfName:
		return "config.toml's; a shared target keeps its name"
	case same:
		return "from config.toml"
	case value == "":
		return "config.toml: none"
	}
	return "config.toml: " + value
}

// lines is the panel form, and the line the cursor is on.
func (p panelForm) lines(th theme.Theme, w int) ([]string, int) {
	values := []string{th.ProjectName.Render("‹ " + string(p.kind) + " ›"), p.title.View(), p.command.View()}
	labels := []string{"kind", "title", "command"}
	out := make([]string, panelFields)
	for j := range out {
		out[j] = clipTo("    "+cursor(th, j == p.field)+th.Meta.Render(pad(labels[j], configLabelWidth))+values[j], w)
	}
	return out, p.field
}
