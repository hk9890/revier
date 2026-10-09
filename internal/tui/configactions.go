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
	"github.com/hk9890/revier/internal/theme"
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
func (cs *configScreen) actionRow(sf surface) (int, bool) {
	i := cs.row - cs.actionBase(sf)
	return i, i >= 0 && i < len(sf.actions)
}

// addRow is the row that adds an action, the screen's last.
func (cs *configScreen) addRow(sf surface) int { return cs.actionBase(sf) + len(sf.actions) }

// actionBase is the row of the first action, after the targets section.
func (cs *configScreen) actionBase(sf surface) int { return cs.addTargetRow(sf) + 1 }

// openActionForm opens the form for action i, or for a new action when i is
// past the last.
func (cs *configScreen) openActionForm(sf surface, i int) tea.Cmd {
	f := actionForm{open: true, index: i}
	for j, placeholder := range [actionFields]string{"sync", "git pull", "ctrl+g"} {
		f.fields[j] = formInput(sf.theme, placeholder)
	}
	if i < len(sf.actions) {
		act := sf.actions[i]
		f.fields[fieldName].SetValue(act.Name)
		f.fields[fieldCommand].SetValue(joinCommand(act.Run))
		f.fields[fieldKey].SetValue(act.Key)
	}
	cs.aform = f
	return cs.aform.fields[fieldName].Focus()
}

// actionFormKey is a press while the form is up. Enter writes the action,
// Esc leaves it as it was.
func (cs *configScreen) actionFormKey(sf surface, msg tea.KeyMsg) (configResult, tea.Cmd) {
	res := configResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		cs.aform = actionForm{}
		return res, nil
	case key.Matches(msg, sf.keys.Next, sf.keys.Down):
		return res, cs.moveField(+1)
	case msg.Type == tea.KeyShiftTab, key.Matches(msg, sf.keys.Up):
		return res, cs.moveField(-1)
	case key.Matches(msg, sf.keys.Enter):
		return cs.saveAction(sf), nil
	case altRune(msg):
		return res, nil
	}
	in, cmd := cs.aform.fields[cs.aform.field].Update(msg)
	cs.aform.fields[cs.aform.field] = in
	return res, cmd
}

func (cs *configScreen) moveField(step int) tea.Cmd {
	cs.aform.fields[cs.aform.field].Blur()
	cs.aform.field = (cs.aform.field + step + actionFields) % actionFields
	return cs.aform.fields[cs.aform.field].Focus()
}

// saveAction writes the form's action, and hands the actions back to be
// bound.
func (cs *configScreen) saveAction(sf surface) configResult {
	f := cs.aform
	run, err := splitCommand(f.fields[fieldCommand].Value())
	if err != nil {
		return configResult{err: err}
	}
	act := config.Action{
		Key:  strings.TrimSpace(f.fields[fieldKey].Value()),
		Name: strings.TrimSpace(f.fields[fieldName].Value()),
		Run:  run,
	}
	if err := checkAction(sf, act, f.index); err != nil {
		return configResult{err: err}
	}
	actions := slices.Clone(sf.actions)
	if f.index == len(actions) {
		err = withConfigRoot(func(root string) error { return config.AddAction(root, sf.actions, act) })
		actions = append(actions, act)
	} else {
		err = withConfigRoot(func(root string) error { return config.ReplaceAction(root, f.index, sf.actions[f.index], act) })
		actions[f.index] = act
	}
	if err != nil {
		return configResult{err: err}
	}
	cs.aform = actionForm{}
	cs.row = cs.actionBase(sf) + f.index
	return configResult{actions: &actions}
}

// checkAction refuses an action whose name or key is already another's. An
// action is run by name (`revier run`), and a press means one thing. Whether
// the key reaches the surface at all is config.Load's rule, which the write
// runs.
func checkAction(sf surface, act config.Action, index int) error {
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
	for i, other := range sf.actions {
		switch {
		case i == index:
		case other.Name == act.Name:
			return fmt.Errorf("an action named %q exists already", act.Name)
		case actionChord(other) == c:
			return fmt.Errorf("%s is the key of action %q", c, other.Name)
		}
	}
	switch name, target := sf.tkeys[c]; {
	case newKeyMap(nil).claims(c):
		return fmt.Errorf("%s is one of revier's own keys", c)
	case slices.Contains(queryKeys, string(c)):
		return fmt.Errorf("%s edits the query", c)
	case target:
		return fmt.Errorf("%s is the key of target %q", c, name)
	}
	return nil
}

// dropAction takes the key that answers the delete question. Only y deletes;
// any other key keeps the action and is not acted on.
func (cs *configScreen) dropAction(sf surface, msg tea.KeyMsg) configResult {
	res := configResult{err: sf.err}
	cs.dropping = false
	i, ok := cs.actionRow(sf)
	if msg.String() != "y" || !ok {
		return res
	}
	act := sf.actions[i]
	if err := withConfigRoot(func(root string) error { return config.RemoveAction(root, i, act) }); err != nil {
		res.err = err
		return res
	}
	actions := slices.Delete(slices.Clone(sf.actions), i, i+1)
	res.actions = &actions
	return res
}

// dropPrompt is the delete question for the target or action under the
// cursor.
func (cs *configScreen) dropPrompt(sf surface) string {
	question := ""
	if i, ok := cs.actionRow(sf); ok {
		question = fmt.Sprintf("delete action %q?", sf.actions[i].Name)
	}
	if i, ok := cs.targetRow(sf); ok {
		question = fmt.Sprintf("delete target %q from every project?", sf.targets[i].Name)
	}
	return sf.theme.Attention.Render(" "+question+"  ") +
		sf.theme.Help.Render("y: delete · any other key: keep")
}

// setActions binds a new set of actions: their keys, the footer, and the
// target keys, which yield to an action's key.
func (m *Model) setActions(actions []config.Action) {
	m.actions = actions
	m.keys = newKeyMap(actions)
	m.setKeys()
}

// lines is the form, one line per field, and the line the cursor
// is on.
func (f actionForm) lines(th theme.Theme, w int) ([]string, int) {
	labels := [actionFields]string{"name", "command", "key"}
	notes := [actionFields]string{"", "{{.Path}} is the project's directory", "ctrl or alt and a key"}
	out := make([]string, actionFields)
	for j, in := range f.fields {
		line := cursor(th, j == f.field) + th.Meta.Render(pad(labels[j], configLabelWidth)) + in.View()
		if notes[j] != "" {
			line += th.Path.Render("  " + notes[j])
		}
		out[j] = clipTo(line, w)
	}
	return out, f.field
}
