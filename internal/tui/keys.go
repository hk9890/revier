package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
)

// keyMap is every key the surface owns, declared once with the label the
// footer shows. A key and its help text cannot drift apart.
//
// There is no "q to quit": on the list every printable rune is a
// filter character, and a project called `queue` has to be reachable.
type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Targets key.Binding
	Back    key.Binding
	Quit    key.Binding
	Edit    key.Binding
	Delete  key.Binding

	// actions are the configured action keys, in configuration order.
	actions []key.Binding
}

func newKeyMap(actions []config.Action) keyMap {
	k := keyMap{
		Up:    key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "up")),
		Down:  key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "down")),
		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		// Tab and not right: left and right move the cursor in the query.
		Targets: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "targets")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		// Alt and a letter, because every other free key is spoken for: a
		// bare letter filters, del and ctrl+e edit the query, and the ctrl
		// chords are where target keys and configured actions live. The
		// surface matches both before the query is offered the key, so the
		// letter never reaches the filter.
		Edit:   key.NewBinding(key.WithKeys("alt+e"), key.WithHelp("alt+e", "edit")),
		Delete: key.NewBinding(key.WithKeys("alt+d"), key.WithHelp("alt+d", "delete")),
	}
	for _, act := range actions {
		c := actionChord(act)
		k.actions = append(k.actions, key.NewBinding(
			key.WithKeys(string(c)),
			key.WithHelp(string(c), act.Name),
		))
	}
	return k
}

// actionChord is an action's key in canonical form. A key that does not parse
// is empty, which no press is; config.Load has refused it already.
func actionChord(act config.Action) core.Chord {
	c, _ := core.ParseChord(act.Key)
	return c
}

// claims reports whether a press is the surface's own or an action's, which
// the surface matches before any target key.
func (k keyMap) claims(c core.Chord) bool {
	for _, b := range append([]key.Binding{k.Up, k.Down, k.Enter, k.Targets, k.Back, k.Quit, k.Edit, k.Delete}, k.actions...) {
		for _, name := range b.Keys() {
			if own, err := core.ParseChord(name); err == nil && own == c {
				return true
			}
		}
	}
	// The action bar's keys are the surface's too: they are not on any
	// binding, because a button carries its own key.
	for _, a := range barActions {
		if own, err := core.ParseChord(a.key); err == nil && own == c {
			return true
		}
	}
	return false
}

// helpFor is the footer for a focus. The same two keys mean different things
// on each - enter opens a project, or runs one of its targets - so the label
// comes from the focus and not from the binding.
func (k keyMap) helpFor(f focus) []key.Binding {
	var out []key.Binding
	if f == focusPane {
		out = []key.Binding{
			helpKey("enter", "go"),
			helpKey("tab", "projects"),
			helpKey("type", "filter"),
			helpKey("esc", "back"),
		}
	} else {
		out = []key.Binding{
			helpKey("enter", "open"),
			k.Targets,
			helpKey("type", "filter"),
			helpKey("esc", "clear/quit"),
		}
	}
	out = append(out, k.Quit)
	return append(out, k.actions...)
}

// helpForDialog is the footer while the link dialog is up. Its two steps
// take the same keys and mean different things by them, and none of the
// surface's own keys act under it.
func (k keyMap) helpForDialog(d dialog) []key.Binding {
	enter := "link"
	switch d {
	case dialogHosts:
		enter = "list its projects"
	case dialogNew:
		return []key.Binding{helpKey("enter", "add the project"), helpKey("esc", "back"), k.Quit}
	}
	return []key.Binding{helpKey("enter", enter), helpKey("esc", "back"), k.Quit}
}

// helpForConfig is the footer on the config screen, and while its trigger
// key is typed.
func (k keyMap) helpForConfig(typing bool) []key.Binding {
	if typing {
		return []key.Binding{helpKey("enter", "save"), helpKey("esc", "cancel"), k.Quit}
	}
	return []key.Binding{
		helpKey("↑↓", "move"), helpKey("←→", "change"), helpKey("enter", "change/edit"),
		helpKey("esc", "back"), k.Quit,
	}
}

// targetHelp is the highlighted project's own target keys. They come from the
// row rather than from configuration, because which keys do anything depends
// on which project the cursor is on.
func (k keyMap) targetHelp(keys []targetKeyHelp) []key.Binding {
	out := make([]key.Binding, 0, len(keys))
	for _, t := range keys {
		out = append(out, helpKey(t.key, t.name))
	}
	return out
}

// helpKey is a help entry. The keys it declares are never matched against -
// key routing uses the fields of keyMap - so a label like "type" is allowed.
func helpKey(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}

func newHelp(th theme.Theme) help.Model {
	h := help.New()
	h.Styles.ShortKey = th.Accent
	h.Styles.ShortDesc = th.Help
	h.Styles.ShortSeparator = th.Border
	h.Styles.Ellipsis = th.Help
	return h
}
