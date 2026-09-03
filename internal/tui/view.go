package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
)

// The header and the footer each take one line, with one blank line above the
// footer so the list never touches it.
const chromeHeight = 3

// layout gives the lists whatever the header and footer leave. It runs on
// every size change and once at construction, so a model that never receives
// a WindowSizeMsg still renders.
func (m *Model) layout() {
	h := m.height - chromeHeight
	if h < 1 {
		h = 1
	}
	m.plist.SetSize(m.width, h)
	m.tlist.SetSize(m.width, h)
	m.help.Width = m.width
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	if m.level == levelTargets {
		b.WriteString(m.tlist.View())
	} else {
		b.WriteString(m.plist.View())
	}
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

// header is the one line that says what is on screen. At the project level it
// counts, because with ninety projects the counts are the reason to look.
func (m Model) header() string {
	th := m.theme
	if m.level == levelTargets {
		return th.Header.Render(" revier  " + string(m.current))
	}
	running, attention := 0, 0
	for _, v := range m.views {
		if v.Running {
			running++
		}
		if v.Attention() {
			attention++
		}
	}
	line := th.Header.Render(fmt.Sprintf(" revier  %d projects", len(m.views))) +
		th.Path.Render(" · ") + th.Running.Render(fmt.Sprintf("%d running", running)) +
		th.Path.Render(" · ") + th.Attention.Render(fmt.Sprintf("%d need you", attention))
	if m.filter != "" {
		line += th.Path.Render("   /") + th.Accent.Render(m.filter) +
			th.Path.Render(fmt.Sprintf(" (%d)", len(m.plist.VisibleItems())))
	}
	return line
}

// footer is the key legend, or the last failure. An error replaces the legend
// rather than being added to it: a survey that failed is the only thing worth
// reading on that line.
func (m Model) footer() string {
	if m.err != nil {
		return m.theme.Attention.Render(" " + m.err.Error())
	}
	return " " + m.help.ShortHelpView(m.keys.helpFor(m.level))
}

// fill pads a rendered row to the width of the list, so the selection
// highlight spans the row instead of ending at the last character.
func fill(row string, width int, selected bool, th theme.Theme) string {
	gap := width - lipgloss.Width(row)
	if gap <= 0 {
		return row
	}
	pad := strings.Repeat(" ", gap)
	if selected {
		return row + th.OnSelection(lipgloss.NewStyle()).Render(pad)
	}
	return row + pad
}

// pad widens a cell to a column. It measures rendered width, so a glyph that
// counts as two cells does not push the columns after it out of line.
func pad(s string, width int) string {
	if n := lipgloss.Width(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// selStyle re-renders text that already carries its own colour so it picks up
// the selection background. lipgloss cannot add a background to a rendered
// string, so the caller styles the text and this wraps the result.
func selStyle(rendered string, selected bool, th theme.Theme) string {
	if !selected {
		return rendered
	}
	return th.OnSelection(lipgloss.NewStyle()).Render(rendered)
}
