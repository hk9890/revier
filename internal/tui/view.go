package tui

import (
	"fmt"
	"os"
	"path/filepath"
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
	w := m.width
	if pane := m.paneWidth(); pane > 0 {
		w = m.width - pane
		m.detail.Width, m.detail.Height = pane-paneChrome, h
	}
	m.plist.SetSize(w, h)
	m.tlist.SetSize(w, h)
	// One column less than the terminal: the footer is rendered with a leading
	// space. help truncates on its own width, and its own truncation gives up
	// once the line is nearly full, so View clips as well.
	m.help.Width = m.width - 1
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.clip(m.header()))
	b.WriteString("\n")
	body := m.plist.View()
	if m.level == levelTargets {
		body = m.tlist.View()
	}
	if m.paneWidth() > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.detail.View())
	}
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(m.clip(m.footer()))
	return b.String()
}

// clip cuts a styled line to the terminal width. Nothing may wrap: a header
// or a footer that wraps pushes a row of the list off the screen, and the
// list has already been sized for the space it was given.
func (m Model) clip(line string) string {
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
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
	keys := m.keys.helpFor(m.level)
	if m.level == levelProjects {
		if v, ok := m.selected(); ok {
			keys = append(keys, m.keys.targetHelp(m.targetKeysOf(v))...)
		}
	}
	return " " + m.help.ShortHelpView(keys)
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

// contractHome writes a path under the home directory as ~/..., which is how
// the user names it and how it fits the column.
func contractHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == home {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}

// truncate keeps the end of a path, not the start: the last two segments say
// which checkout this is, and the first say only where checkouts live.
func truncate(s string, width int) string {
	if width < 4 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-width+1:])
}
