package tui

import (
	"strings"

	"github.com/hk9890/revier/internal/plain"
)

// tabWidth is the columns a tab takes: what lipgloss draws one as.
const tabWidth = 4

// plainText is an agent's message with nothing in it the surface did not put
// there: no escape sequence, which would repaint the screen, move the cursor
// or speak to the emulator from inside a message quoting a tool's output; no
// other control character but the newlines the lines are split on; and a tab
// written as the spaces lipgloss draws it as. The row that shows a message's
// first line styles it itself, so nothing the message carries is styling
// (decisions.md D108).
func plainText(s string) string {
	return strings.ReplaceAll(plain.Text(s), "\t", strings.Repeat(" ", tabWidth))
}
