package tui

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/theme"
)

// plain is markdown's lines with their styling taken off, as a reader sees
// them.
func plain(text string, w int) []string {
	lines := markdown(text, w, theme.Default())
	for i := range lines {
		lines[i] = ansi.Strip(lines[i])
	}
	return lines
}
