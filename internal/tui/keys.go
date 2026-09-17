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
	Up       key.Binding
	Down     key.Binding
	Home     key.Binding
	End      key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Enter    key.Binding
	Next     key.Binding
	Prev     key.Binding
	Back     key.Binding
	Quit     key.Binding
	Edit     key.Binding
	Delete   key.Binding

	// actions are the configured action keys, in configuration order.
	actions []key.Binding
}

func newKeyMap(actions []config.Action) keyMap {
	k := keyMap{
		Up:   key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "up")),
		Down: key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "down")),
		// Home and end move the cursor to the first and last row, not in the
		// query: ctrl+a and ctrl+e do that there.
		Home:     key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first")),
		End:      key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "page down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		// Tab and not right: left and right move the cursor in the query.
		Next: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next section")),
		Prev: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous section")),
		Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
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
	for _, b := range append(k.own(), k.actions...) {
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

// own is every binding of the surface's own, the action bar's aside.
func (k keyMap) own() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Home, k.End, k.PageUp, k.PageDown, k.Enter, k.Next, k.Prev, k.Back, k.Quit, k.Edit, k.Delete}
}

// helpFor is the footer for a focus. Enter means something different in each
// section - a project opens, a target runs, an agent comes to the front - so
// the label comes from the focus and not from the binding.
func (k keyMap) helpFor(f focus) []key.Binding {
	enter, esc := "open", "clear/quit"
	switch f {
	case focusTargets:
		enter, esc = "go", "clear/back"
	case focusAgents:
		enter, esc = "go to agent", "clear/back"
	}
	out := []key.Binding{
		helpKey("enter", enter),
		helpKey("tab", "next section"),
		helpKey("type", "filter"),
		helpKey("esc", esc),
		k.Quit,
	}
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
	case dialogRemote:
		return []key.Binding{helpKey("enter", "name the link"), helpKey("type", "filter"), helpKey("esc", "clear/back"), k.Quit}
	case dialogNew:
		return []key.Binding{helpKey("enter", "add the project"), helpKey("tab", "complete"), helpKey("↑↓", "choose"), helpKey("esc", "back"), k.Quit}
	case dialogHelp:
		return []key.Binding{helpKey("↑↓", "scroll"), helpKey("esc", "back"), k.Quit}
	case dialogSessions:
		return []key.Binding{helpKey("enter", "restore"), helpKey(sessionsBarKey, "save"), helpKey("esc", "back"), k.Quit}
	case dialogSessionName:
		return []key.Binding{helpKey("enter", "save"), helpKey("esc", "back"), k.Quit}
	case dialogShutdown:
		return []key.Binding{helpKey("↑↓", "choose"), helpKey("enter", "next"), helpKey("esc", "back"), k.Quit}
	}
	return []key.Binding{helpKey("enter", enter), helpKey("esc", "back"), k.Quit}
}

// configHelp is what the config screen's cursor is on, which decides its
// footer.
type configHelp int

const (
	configOnSetting configHelp = iota
	configTyping               // the trigger key is typed
	configInForm               // an action's form is up
	configOnAction
	configOnAdd
	configInPanelForm // a panel of a target's form is up
	configOnFormPanel // the target form's cursor is on a panel
	configOnFormHome  // the target form's cursor is on the home flag
	configInTargetForm
)

func (m Model) configHelp() configHelp {
	_, onAction := m.actionRow()
	_, onTarget := m.targetRow()
	switch {
	case m.chord.Focused():
		return configTyping
	case m.aform.open:
		return configInForm
	case m.tform.panel.open:
		return configInPanelForm
	case m.tform.open:
		switch m.tform.rows()[m.tform.cursor].kind {
		case rowPanel:
			return configOnFormPanel
		case rowAddPanel:
			return configOnAdd
		case rowHome:
			return configOnFormHome
		}
		return configInTargetForm
	case onAction, onTarget:
		return configOnAction
	case m.crow == m.addRow(), m.crow == m.addTargetRow():
		return configOnAdd
	}
	return configOnSetting
}

// helpForConfig is the footer on the config screen.
func (k keyMap) helpForConfig(h configHelp) []key.Binding {
	switch h {
	case configTyping:
		return []key.Binding{helpKey("enter", "save"), helpKey("esc", "cancel"), k.Quit}
	case configInForm:
		return []key.Binding{helpKey("enter", "save"), helpKey("tab", "next field"), helpKey("esc", "cancel"), k.Quit}
	case configOnAction:
		return []key.Binding{
			helpKey("↑↓", "move"), helpKey("enter", "edit"), helpKey(k.Delete.Help().Key, "delete"),
			helpKey("esc", "back"), k.Quit,
		}
	case configOnAdd:
		return []key.Binding{helpKey("↑↓", "move"), helpKey("enter", "add"), helpKey("esc", "back"), k.Quit}
	case configInTargetForm:
		return []key.Binding{helpKey("enter", "save"), helpKey("↑↓", "move"), helpKey("esc", "cancel"), k.Quit}
	case configOnFormHome:
		return []key.Binding{helpKey("←→", "change"), helpKey("enter", "save"), helpKey("↑↓", "move"), helpKey("esc", "cancel"), k.Quit}
	case configOnFormPanel:
		return []key.Binding{
			helpKey("enter", "edit"), helpKey(k.Delete.Help().Key, "delete"), helpKey("↑↓", "move"),
			helpKey("esc", "cancel"), k.Quit,
		}
	case configInPanelForm:
		return []key.Binding{helpKey("enter", "keep"), helpKey("←→", "kind"), helpKey("tab", "next field"), helpKey("esc", "cancel"), k.Quit}
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
