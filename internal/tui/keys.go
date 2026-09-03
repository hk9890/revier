package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
)

// keyMap is every key the surface owns, declared once with the label the
// footer shows. A key and its help text cannot drift apart.
//
// There is no "q to quit": at the project level every printable rune is a
// filter character, and a project called `queue` has to be reachable.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Back      key.Binding
	Quit      key.Binding
	Backspace key.Binding

	// actions are the configured action keys, in configuration order.
	actions []key.Binding
}

func newKeyMap(actions []config.Action) keyMap {
	k := keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "down")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
	}
	for _, act := range actions {
		k.actions = append(k.actions, key.NewBinding(
			key.WithKeys(keyName(act.Key)),
			key.WithHelp(keyName(act.Key), act.Name),
		))
	}
	return k
}

// helpFor is the footer for a level. The same two keys mean different things
// at each - enter opens a project's targets, or runs one - so the label comes
// from the level and not from the binding.
func (k keyMap) helpFor(l level) []key.Binding {
	var out []key.Binding
	if l == levelTargets {
		out = []key.Binding{
			helpKey("enter", "go"),
			helpKey("esc", "back"),
		}
	} else {
		out = []key.Binding{
			helpKey("enter", "targets"),
			helpKey("type", "filter"),
			helpKey("esc", "clear/quit"),
		}
	}
	out = append(out, k.Quit)
	return append(out, k.actions...)
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
