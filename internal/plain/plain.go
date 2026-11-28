// Package plain takes off what an agent, a tool or a status command wrote
// for the terminal, so that what is left is drawn as text, by the TUI and by
// the command line. An agent's message must be drawn as text: the harness
// cannot clean it for revier, since it does not own what a tool wrote into
// the transcript it keeps.
package plain

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Line is Text on one line: a tab is a space, and a newline goes. It is for
// text drawn in one cell or one column, where a byte the width cannot measure
// breaks the frame.
func Line(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t':
			return ' '
		case '\n':
			return -1
		}
		return r
	}, Text(s))
}

// Text is s with every escape sequence and every control character taken off
// but the newline and the tab, and each "\r\n" a newline.
func Text(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}
