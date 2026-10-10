package tui

import (
	"regexp"
	"strings"
	"unicode"
)

// escapeSeq is an escape sequence in the text an agent wrote or its terminal
// shows: a CSI with its parameters, an OSC up to its terminator, or a
// two-character escape.
var escapeSeq = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\)?|[@-Z\\\\-_])")

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
	s = escapeSeq.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
	return strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}
