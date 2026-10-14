package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/theme"
)

// promptMark is what stands in front of the query. It has to render in any
// font, so it is an angle bracket and not a Nerd Font glyph.
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
	st := in.Styles()
	for _, state := range []*textinput.StyleState{&st.Focused, &st.Blurred} {
		state.Prompt = th.Accent
		state.Text = th.ProjectName
		state.Placeholder = th.NameDim
	}
	st.Cursor.Color = th.Accent.GetForeground()
	in.SetStyles(st)
}

// inputView is a text field as the surface draws it. An empty field shows its
// placeholder: whole in a field with no width, and in one with a width cut
// with an ellipsis to the room its text and cursor take, and padded to it.
// textinput draws a field with no width with the placeholder's first
// character only, and cuts one with a width with no mark, so it is handed a
// placeholder that needs neither.
func inputView(in textinput.Model) string {
	if in.Value() != "" || in.Placeholder == "" {
		return in.View()
	}
	first, _ := ansi.FirstGraphemeCluster(in.Placeholder, ansi.GraphemeWidth)
	rest := in.Placeholder[len(first):]
	pad := 0
	if w := in.Width(); w > 0 {
		room := w + 1 - lipgloss.Width(first)
		rest = ansi.Truncate(rest, room, "…")
		pad = max(0, room-lipgloss.Width(rest))
	}
	in.Placeholder = first + rest
	in.SetWidth(max(lipgloss.Width(in.Placeholder)-1, 0))
	return in.View() + strings.Repeat(" ", pad)
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
func promptKey(msg keyPress) bool {
	if altRune(msg) {
		return false
	}
	if msg.runes() || msg.space() {
		return true
	}
	for _, code := range []rune{tea.KeyBackspace, tea.KeyDelete, tea.KeyLeft, tea.KeyRight} {
		if msg.bare(code) {
			return true
		}
	}
	return slices.Contains(queryKeys, msg.String())
}

// altRune reports an alt chord of a printable key. It is a key of the
// surface's, or of nothing, and never text: a field that took it would type
// its letter, so alt+h on the new-project screen wrote an h into the path,
// and the popup's own key pressed over a screen would write a space.
func altRune(msg keyPress) bool {
	return (msg.runes() || msg.space()) && msg.Mod.Contains(tea.ModAlt)
}

// queryKeys are word and line editing, the readline keys a query field is
// expected to have. ctrl+n and ctrl+p are not here: they move the list.
var queryKeys = []string{"ctrl+w", "ctrl+u", "ctrl+a", "ctrl+e", "alt+backspace"}

// edit feeds a key to the field of the section the cursor is in, and
// re-filters that section if its query changed.
func (m Model) edit(msg keyPress) (Model, tea.Cmd) {
	in := m.field()
	before := in.Value()
	next, cmd := in.Update(msg.input())
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
		return inputView(in)
	}
	text := in.Value()
	if text == "" {
		text = in.Placeholder
	}
	line := promptMark + text
	if in.Width() > 0 {
		line = ellipsis(line, lipgloss.Width(promptMark)+in.Width()+1)
	}
	return m.theme.NameDim.Render(line)
}
