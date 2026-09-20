package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

// The actions section of the config screen: one row per configured action,
// and a row that adds one. Enter on an action opens its form, alt+d deletes
// it after a y. A change is written to config.toml as it is made
// (config.AddAction, ReplaceAction, RemoveAction), and the surface binds the
// new keys at once.

// The fields of the action form, in the order Tab walks them.
const (
	fieldName = iota
	fieldCommand
	fieldKey
	actionFields
)

// actionForm is an action being added or changed.
type actionForm struct {
	open   bool
	index  int // the action changed, or len(actions) for a new one
	field  int // the field the cursor is in
	fields [actionFields]textinput.Model
}

// actionRow is the action the config screen's cursor is on, if it is on one.
func (m Model) actionRow() (int, bool) {
	i := m.crow - m.actionBase()
	return i, i >= 0 && i < len(m.actions)
}

// addRow is the row that adds an action, the screen's last.
func (m Model) addRow() int { return m.actionBase() + len(m.actions) }

// actionBase is the row of the first action, after the targets section.
func (m Model) actionBase() int { return m.addTargetRow() + 1 }

// openActionForm opens the form for action i, or for a new action when i is
// past the last.
func (m Model) openActionForm(i int) (tea.Model, tea.Cmd) {
	f := actionForm{open: true, index: i}
	for j, placeholder := range [actionFields]string{"sync", "git pull", "ctrl+g"} {
		in := textinput.New()
		styleField(&in, m.theme)
		in.Prompt = ""
		in.Placeholder = placeholder
		f.fields[j] = in
	}
	if i < len(m.actions) {
		act := m.actions[i]
		f.fields[fieldName].SetValue(act.Name)
		f.fields[fieldCommand].SetValue(joinCommand(act.Run))
		f.fields[fieldKey].SetValue(act.Key)
	}
	m.err = nil
	m.aform = f
	return m, m.aform.fields[fieldName].Focus()
}

// actionFormKey is a press while the form is up. Enter writes the action,
// Esc leaves it as it was.
func (m Model) actionFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.aform = actionForm{}
		return m, nil
	case key.Matches(msg, m.keys.Next, m.keys.Down):
		return m.moveField(+1)
	case msg.Type == tea.KeyShiftTab, key.Matches(msg, m.keys.Up):
		return m.moveField(-1)
	case key.Matches(msg, m.keys.Enter):
		return m.saveAction()
	case altRune(msg):
		return m, nil
	}
	in, cmd := m.aform.fields[m.aform.field].Update(msg)
	m.aform.fields[m.aform.field] = in
	return m, cmd
}

func (m Model) moveField(step int) (tea.Model, tea.Cmd) {
	m.aform.fields[m.aform.field].Blur()
	m.aform.field = (m.aform.field + step + actionFields) % actionFields
	return m, m.aform.fields[m.aform.field].Focus()
}

// saveAction writes the form's action and binds it.
func (m Model) saveAction() (tea.Model, tea.Cmd) {
	f := m.aform
	run, err := splitCommand(f.fields[fieldCommand].Value())
	if err != nil {
		m.err = err
		return m, nil
	}
	act := config.Action{
		Key:  strings.TrimSpace(f.fields[fieldKey].Value()),
		Name: strings.TrimSpace(f.fields[fieldName].Value()),
		Run:  run,
	}
	if err := m.checkAction(act, f.index); err != nil {
		m.err = err
		return m, nil
	}
	actions := slices.Clone(m.actions)
	if f.index == len(actions) {
		err = withConfigRoot(func(root string) error { return config.AddAction(root, m.actions, act) })
		actions = append(actions, act)
	} else {
		err = withConfigRoot(func(root string) error { return config.ReplaceAction(root, f.index, m.actions[f.index], act) })
		actions[f.index] = act
	}
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.aform = actionForm{}
	m.setActions(actions)
	m.crow = m.actionBase() + f.index
	return m, nil
}

// checkAction refuses an action whose name or key is already another's. An
// action is run by name (`revier run`), and a press means one thing. Whether
// the key reaches the surface at all is config.Load's rule, which the write
// runs.
func (m Model) checkAction(act config.Action, index int) error {
	switch {
	case act.Name == "":
		return errors.New("an action needs a name")
	case len(act.Run) == 0:
		return errors.New("an action needs a command")
	case act.Key == "":
		return errors.New("an action needs a key")
	}
	c, err := core.ParseChord(act.Key)
	if err != nil {
		return err
	}
	for i, other := range m.actions {
		switch {
		case i == index:
		case other.Name == act.Name:
			return fmt.Errorf("an action named %q exists already", act.Name)
		case actionChord(other) == c:
			return fmt.Errorf("%s is the key of action %q", c, other.Name)
		}
	}
	switch name, target := m.tkeys[c]; {
	case newKeyMap(nil).claims(c):
		return fmt.Errorf("%s is one of revier's own keys", c)
	case slices.Contains(queryKeys, string(c)):
		return fmt.Errorf("%s edits the query", c)
	case target:
		return fmt.Errorf("%s is the key of target %q", c, name)
	}
	return nil
}

// confirmDropAction takes the key that answers the delete question. Only y
// deletes; any other key keeps the action and is not acted on.
func (m Model) confirmDropAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.dropping = false
	i, ok := m.actionRow()
	if msg.String() != "y" || !ok {
		return m, nil
	}
	act := m.actions[i]
	if err := withConfigRoot(func(root string) error { return config.RemoveAction(root, i, act) }); err != nil {
		m.err = err
		return m, nil
	}
	m.setActions(slices.Delete(slices.Clone(m.actions), i, i+1))
	return m, nil
}

// dropPrompt is the delete question for the target or action under the
// config screen's cursor.
func (m Model) dropPrompt() string {
	question := ""
	if i, ok := m.actionRow(); ok {
		question = fmt.Sprintf("delete action %q?", m.actions[i].Name)
	}
	if i, ok := m.targetRow(); ok {
		question = fmt.Sprintf("delete target %q from every project?", m.targets[i].Name)
	}
	return m.theme.Attention.Render(" "+question+"  ") +
		m.theme.Help.Render("y: delete · any other key: keep")
}

// setActions binds a new set of actions: their keys, the footer, and the
// target keys, which yield to an action's key.
func (m *Model) setActions(actions []config.Action) {
	m.actions = actions
	m.keys = newKeyMap(actions)
	m.setKeys()
}

// actionFormLines is the form, one line per field, and the line the cursor
// is on.
func (m Model) actionFormLines(w int) ([]string, int) {
	th := m.theme
	labels := [actionFields]string{"name", "command", "key"}
	notes := [actionFields]string{"", "{{.Path}} is the project's directory", "ctrl or alt and a key"}
	out := make([]string, actionFields)
	for j, in := range m.aform.fields {
		line := cursor(th, j == m.aform.field) + th.Meta.Render(pad(labels[j], configLabelWidth)) + in.View()
		if notes[j] != "" {
			line += th.Path.Render("  " + notes[j])
		}
		out[j] = clipTo(line, w)
	}
	return out, m.aform.field
}
