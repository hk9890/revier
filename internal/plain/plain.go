// Package plain takes off what an agent, a tool or a status command wrote
// for the terminal, so that what is left is drawn as text (decisions.md D108).
package plain

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

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
