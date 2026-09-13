package tui

import (
	"slices"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/theme"
)

// promptMark is what stands in front of the query. The picker being replaced
// uses a Nerd Font magnifier (os-fzf.sh:777, --prompt="  "); this has to
// render in any font, so it is an angle bracket.
const promptMark = "❯ "

// newPrompt is the filter input. It owns the query text: the surface used to
// edit a string by hand, which meant no cursor, no word deletion and nowhere
// on screen that said "type here".
func newPrompt(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = projectPlaceholder
	in.CharLimit = 64
	// Focused from the start: the surface filters as you type, so the query
	// line is always where a keystroke lands. Init calls Focus again for the
	// blink command; this call is what makes the input accept keys at all.
	_ = in.Focus()
	return in
}

// styleField gives a text field the theme's colours. The fields are restyled
// in place when the theme changes, so their text and cursor survive it.
func styleField(in *textinput.Model, th theme.Theme) {
	in.PromptStyle = th.Accent
	in.TextStyle = th.ProjectName
	in.Cursor.Style = th.Accent
	in.PlaceholderStyle = th.NameDim
}

// What the query line says when it is empty: which rows a keystroke filters.
const (
	projectPlaceholder = "filter"
	targetPlaceholder  = "filter targets"
)

// promptKeys are the keys the input gets. Everything else is the surface's:
// up and down move the list, enter activates, esc goes back. Without this
// split the input would swallow the keys that drive the list.
func (m Model) promptKey(msg tea.KeyMsg) bool {
	if altRune(msg) {
		return false
	}
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete,
		tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd:
		return true
	}
	return slices.Contains(queryKeys, msg.String())
}

// altRune reports an alt chord of a printable key. It is a key of the
// surface's, or of nothing, and never text: a field that took it would type
// its letter, so alt+h on the new-project screen wrote an h into the path.
func altRune(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && msg.Alt
}

// queryKeys are word and line editing, the readline keys a query field is
// expected to have. ctrl+n and ctrl+p are not here: they move the list.
var queryKeys = []string{"ctrl+w", "ctrl+u", "ctrl+a", "ctrl+e", "alt+backspace"}

// edit feeds a key to the input and re-filters if the query changed. The
// query line is one field whose scope follows the cursor: over the projects
// while the cursor is on the list, over the pane's target rows while it is
// there (decisions.md D43).
func (m Model) edit(msg tea.KeyMsg) (Model, tea.Cmd) {
	before := m.input.Value()
	in, cmd := m.input.Update(msg)
	m.input = in
	if in.Value() == before {
		return m, cmd
	}
	if m.focus == focusPane {
		m.setTargetFilter(in.Value())
	} else {
		m.setFilter(in.Value())
	}
	return m, cmd
}

// promptView is the query line: the mark, the text, and a cursor that says
// where typing lands.
func (m Model) promptView() string {
	return " " + m.input.View()
}
