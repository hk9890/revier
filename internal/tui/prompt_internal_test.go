package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/theme"
)

// A focused empty field shows its placeholder in the room its text and cursor
// take, and cuts it where the same field unfocused cuts it: focusing the query
// must not shorten what it says.
func TestAFocusedPlaceholderIsCutWhereAnUnfocusedOneIs(t *testing.T) {
	in := newPrompt(theme.Default(), projectPlaceholder)
	in.Focus()
	for _, width := range []int{4, 10, 14, 30} {
		in.SetWidth(width)
		room := lipgloss.Width(promptMark) + width + 1
		got := ansi.Strip(inputView(in))
		if want := ellipsis(promptMark+projectPlaceholder, room); strings.TrimRight(got, " ") != want {
			t.Errorf("width %d: %q, want %q", width, got, want)
		}
		if w := lipgloss.Width(got); w != room {
			t.Errorf("width %d: %q is %d cells, want %d", width, got, w, room)
		}
	}
}

// A field with no width shows its placeholder whole, however short.
func TestAPlaceholderInAFieldWithNoWidthIsWhole(t *testing.T) {
	for _, placeholder := range []string{"a", "ab", "abc", projectPlaceholder} {
		in := newPrompt(theme.Default(), placeholder)
		in.Focus()
		if got, want := ansi.Strip(inputView(in)), promptMark+placeholder; got != want {
			t.Errorf("placeholder %q: %q, want %q", placeholder, got, want)
		}
	}
}
