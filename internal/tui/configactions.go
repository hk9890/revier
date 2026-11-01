package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

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

// actionForm is an action being added or changed. It keeps the action it
// changes, not its place: the files read again at the focus can replace the
// list while the form is open.
type actionForm struct {
	open   bool
	adds   bool
	was    config.Action // the action changed, when the form does not add one
	field  int           // the field the cursor is in
	fields [actionFields]textinput.Model
}

// at is where the form's action is in actions now: past the last for a new
// one. An action changed that is gone is false, and past the last too, where
// the screen shows its form.
func (f actionForm) at(actions []config.Action) (int, bool) {
	if f.adds {
		return len(actions), true
	}
	i := actionIndex(actions, f.was)
	if i < 0 {
		return len(actions), false
	}
	return i, true
}

// actionIndex is where act is in actions, or -1 when it is not there.
func actionIndex(actions []config.Action, act config.Action) int {
	return slices.IndexFunc(actions, func(a config.Action) bool {
		return a.Name == act.Name && a.Key == act.Key && slices.Equal(a.Run, act.Run)
	})
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
	f := actionForm{open: true, adds: i >= len(sf.actions)}
	for j, placeholder := range [actionFields]string{"sync", "git pull", "ctrl+g"} {
		f.fields[j] = formInput(sf.theme, placeholder)
	}
	if !f.adds {
		act := sf.actions[i]
		f.was = act
		f.fields[fieldName].SetValue(act.Name)
		f.fields[fieldCommand].SetValue(joinCommand(act.Run))
		f.fields[fieldKey].SetValue(act.Key)
	}
	cs.aform = f
	return cs.aform.fields[fieldName].Focus()
}

// actionFormKey is a press while the form is up. Enter writes the action,
// Esc leaves it as it was.
func (cs *configScreen) actionFormKey(sf surface, msg keyPress) (configResult, tea.Cmd) {
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
	case msg.shiftTab(), key.Matches(msg, sf.keys.Up):
		return res, cs.moveField(-1)
	case key.Matches(msg, sf.keys.Enter):
		return cs.saveAction(sf), nil
	case altRune(msg):
		return res, nil
	}
	in, cmd := cs.aform.fields[cs.aform.field].Update(msg.input())
	cs.aform.fields[cs.aform.field] = in
	return res, cmd
}

func (cs *configScreen) moveField(step int) tea.Cmd {
	cs.aform.fields[cs.aform.field].Blur()
	cs.aform.field = (cs.aform.field + step + actionFields) % actionFields
	return cs.aform.fields[cs.aform.field].Focus()
}

// saveAction writes the form's action, and hands the actions back to be
// bound. An action changed that left config.toml is not written, and the
// form stays.
func (cs *configScreen) saveAction(sf surface) configResult {
	f := cs.aform
	i, ok := f.at(sf.actions)
	if !ok {
		return configResult{err: fmt.Errorf("action %q is no longer in config.toml as the form read it; Esc and open it again", f.was.Name)}
	}
	run, err := splitCommand(f.fields[fieldCommand].Value())
	if err != nil {
		return configResult{err: err}
	}
	act := config.Action{
		Key:  strings.TrimSpace(f.fields[fieldKey].Value()),
		Name: strings.TrimSpace(f.fields[fieldName].Value()),
		Run:  run,
	}
	if err := checkAction(sf, act, i); err != nil {
		return configResult{err: err}
	}
	actions := slices.Clone(sf.actions)
	if f.adds {
		err = cs.write(func(root string) error { return config.AddAction(root, sf.actions, act) })
		actions = append(actions, act)
	} else {
		err = cs.write(func(root string) error { return config.ReplaceAction(root, i, sf.actions[i], act) })
		actions[i] = act
	}
	if err != nil {
		return configResult{err: err}
	}
	cs.aform = actionForm{}
	cs.row = cs.actionBase(sf) + i
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
// any other key but ctrl+c, which quits before it gets here, keeps the
// action and is not acted on. An action that left config.toml while the
// question waited is not deleted, nor is the one now in its place.
func (cs *configScreen) dropAction(sf surface, msg keyPress) configResult {
	res := configResult{err: sf.err}
	act := cs.drop.action
	cs.drop = dropQuestion{}
	if msg.String() != "y" {
		return res
	}
	i := actionIndex(sf.actions, act)
	if i < 0 {
		res.err = fmt.Errorf("action %q is no longer in config.toml as the question read it; nothing is deleted", act.Name)
		return res
	}
	if err := cs.write(func(root string) error { return config.RemoveAction(root, i, act) }); err != nil {
		res.err = err
		return res
	}
	actions := slices.Delete(slices.Clone(sf.actions), i, i+1)
	res.actions = &actions
	return res
}

// dropPrompt is the delete question for the target or action it was asked
// for.
func (cs *configScreen) dropPrompt(sf surface) string {
	question := fmt.Sprintf("delete action %q?", cs.drop.action.Name)
	if t := cs.drop.target; t != nil {
		question = fmt.Sprintf("delete target %q from every project?", t.Name)
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
		line := cursor(th, j == f.field) + th.Meta.Render(pad(labels[j], configLabelWidth)) + inputView(in)
		if notes[j] != "" {
			line += th.Path.Render("  " + notes[j])
		}
		out[j] = clipTo(line, w)
	}
	return out, f.field
}
