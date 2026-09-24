package tui

import (
	"slices"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
)

// promptMark is what stands in front of the query. The picker being replaced
// uses a Nerd Font magnifier (os-fzf.sh:777, --prompt="  "); this has to
// render in any font, so it is an angle bracket.
const promptMark = "❯ "

// newPrompt is the filter input. It owns the query text: the surface used to
// edit a string by hand, which meant no cursor, no word deletion and nowhere
// on screen that said "type here".
func newPrompt(th theme.Theme, placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = placeholder
	in.CharLimit = 64
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
	projectPlaceholder = "filter projects"
	agentPlaceholder   = "filter agents"
)

// promptKeys are the keys the input gets. Everything else is the surface's:
// up, down, home, end and the page keys move the list, enter activates, esc
// goes back. Without this split the input would swallow the keys that drive
// the list.
func (m Model) promptKey(msg tea.KeyMsg) bool {
	if altRune(msg) {
		return false
	}
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete,
		tea.KeyLeft, tea.KeyRight:
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

// edit feeds a key to the field of the section the cursor is in, and
// re-filters that section if its query changed.
func (m Model) edit(msg tea.KeyMsg) (Model, tea.Cmd) {
	in := m.field()
	before := in.Value()
	next, cmd := in.Update(msg)
	*in = next
	if next.Value() != before {
		m.query(m.focus, next.Value())
	}
	return m, cmd
}

// fieldView is a section's query field. The field the cursor is in shows its
// cursor; the others are dim, so there is one place that looks typed into.
// A dim field is cut to the width the field has when it is typed into: a
// longer line wraps in the wide pane and moves every row under it off the
// line a click finds it on.
func (m Model) fieldView(in textinput.Model, f focus) string {
	if m.focus == f && m.dialog == dialogNone {
		return in.View()
	}
	text := in.Value()
	if text == "" {
		text = in.Placeholder
	}
	line := promptMark + text
	if in.Width > 0 {
		line = ellipsis(line, lipgloss.Width(promptMark)+in.Width+1)
	}
	return m.theme.NameDim.Render(line)
}
