package tui

import (
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
	in.PromptStyle = th.Accent
	in.TextStyle = th.ProjectName
	in.Cursor.Style = th.Accent
	in.Placeholder = "filter"
	in.PlaceholderStyle = th.NameDim
	in.CharLimit = 64
	// Focused from the start: the surface filters as you type, so the query
	// line is always where a keystroke lands. Init calls Focus again for the
	// blink command; this call is what makes the input accept keys at all.
	_ = in.Focus()
	return in
}

// promptKeys are the keys the input gets. Everything else is the surface's:
// up and down move the list, enter activates, esc goes back. Without this
// split the input would swallow the keys that drive the list.
func (m Model) promptKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete,
		tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd:
		return true
	}
	switch msg.String() {
	// Word and line editing, the readline keys a query field is expected to
	// have. ctrl+n and ctrl+p are not here: they move the list.
	case "ctrl+w", "ctrl+u", "ctrl+a", "ctrl+e", "alt+backspace":
		return true
	}
	return false
}

// edit feeds a key to the input and re-filters if the query changed.
func (m Model) edit(msg tea.KeyMsg) (Model, tea.Cmd) {
	before := m.input.Value()
	in, cmd := m.input.Update(msg)
	m.input = in
	if in.Value() != before {
		m.setFilter(in.Value())
	}
	return m, cmd
}

// promptView is the query line: the mark, the text, and a cursor that says
// where typing lands.
func (m Model) promptView() string {
	return " " + m.input.View()
}
