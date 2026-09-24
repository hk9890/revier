package tui

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/theme"
)

var (
	mdHeading   = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	mdItem      = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	mdCode      = regexp.MustCompile("`([^`]+)`")
	mdBold      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdLink      = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdSeparator = regexp.MustCompile(`^:?-+:?$`)
	// mdEscape is an escape sequence in the text an agent wrote: a CSI with
	// its parameters, an OSC up to its terminator, or a two-character escape.
	mdEscape = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\)?|[@-Z\\\\-_])")
)

// tabWidth is the columns a tab takes: what lipgloss draws one as.
const tabWidth = 4

// codeMark is the first of the characters a code span stands in for while the
// rest of its line is styled: Unicode's last private-use plane, which no font
// an agent's text is drawn in puts glyphs in.
const codeMark = 0x100000

// markdown sets the Markdown an agent writes as the pane's lines, at a width:
// a heading in the heading colour, bold as bold, inline code in the path
// colour, a list item with a hanging indent, a code block indented and cut
// rather than wrapped, and a table in aligned columns. It covers what agents
// write, not the whole of CommonMark: anything else reads as a paragraph.
//
// The text is made plain first, so a line is as wide measured as it is drawn
// and carries only what this function styled it with.
func markdown(text string, w int, th theme.Theme) []string {
	var out []string
	blank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	lines := strings.Split(plainText(text), "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case trimmed == "":
			blank()
		case strings.HasPrefix(trimmed, "```"):
			blank()
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				out = append(out, clipTo(th.Meta.Render("  "+lines[i]), w))
			}
		case strings.HasPrefix(trimmed, "|"):
			blank()
			var rows []string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, strings.TrimSpace(lines[i]))
			}
			i--
			out = append(out, table(rows, w, th)...)
		case mdHeading.MatchString(trimmed):
			blank()
			for _, part := range wrap(inline(mdHeading.FindStringSubmatch(trimmed)[1], th), w) {
				out = append(out, th.Heading.Render(part))
			}
		case mdItem.MatchString(lines[i]):
			m := mdItem.FindStringSubmatch(lines[i])
			marker := m[2]
			if strings.ContainsAny(marker, "-*+") {
				marker = "•"
			}
			out = append(out, hanging(strings.Repeat(" ", len(m[1]))+marker+" ", inline(m[3], th), w)...)
		default:
			out = append(out, wrap(inline(trimmed, th), w)...)
		}
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// plainText is an agent's message with nothing in it the pane did not put there:
// no escape sequence, which would repaint the screen, move the cursor or speak
// to the emulator from inside a message quoting a tool's output; no other
// control character but the newlines the lines are split on; and a tab written
// as the spaces lipgloss draws it as. The pane styles the message itself, so
// nothing the message carries is styling.
func plainText(s string) string {
	s = mdEscape.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
	return strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// inline styles the spans of one line: code between a pair of backticks in the
// path colour, bold as bold, and a link as its text. Code is set aside while
// the rest is styled, so bold can hold code and code keeps what looks like
// markup; a backtick with no pair is text.
func inline(s string, th theme.Theme) string {
	var code []string
	s = mdCode.ReplaceAllStringFunc(s, func(c string) string {
		code = append(code, th.Path.Render(c[1:len(c)-1]))
		return string(rune(codeMark + len(code) - 1))
	})
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdBold.ReplaceAllStringFunc(s, func(b string) string {
		return lipgloss.NewStyle().Bold(true).Render(b[2 : len(b)-2])
	})
	for i, c := range code {
		s = strings.Replace(s, string(rune(codeMark+i)), c, 1)
	}
	return s
}

// hanging wraps text after a lead, continuing under the text rather than under
// the lead, so a list item's lines stay in the item.
func hanging(lead, text string, w int) []string {
	indent := lipgloss.Width(lead)
	parts := wrap(text, w-indent)
	// A lead as wide as the column leaves nothing to wrap in. The item is cut
	// on the right rather than dropped: a line that vanishes reads as a
	// message that never held it.
	if len(parts) == 0 {
		return []string{clipTo(lead+text, w)}
	}
	for i := range parts {
		if i == 0 {
			parts[i] = lead + parts[i]
		} else {
			parts[i] = strings.Repeat(" ", indent) + parts[i]
		}
	}
	return parts
}

// table sets a Markdown table's rows in aligned columns, the header in bold,
// with the separator row dropped. A table wider than the pane is cut on the
// right: its first columns name the rows, and are the ones worth keeping.
//
// Each cell is measured as it is drawn, the header's bold included, so the
// padding that lines a column up is never counted from another string.
func table(rows []string, w int, th theme.Theme) []string {
	bold := lipgloss.NewStyle().Bold(true)
	var cells [][]string
	for _, row := range rows {
		var cols []string
		for _, c := range strings.Split(strings.Trim(row, "|"), "|") {
			cols = append(cols, strings.TrimSpace(c))
		}
		if separator(cols) {
			continue
		}
		for j := range cols {
			cols[j] = inline(cols[j], th)
			if len(cells) == 0 {
				cols[j] = bold.Render(cols[j])
			}
		}
		cells = append(cells, cols)
	}
	var widths []int
	for _, cols := range cells {
		for j, c := range cols {
			if j == len(widths) {
				widths = append(widths, 0)
			}
			widths[j] = max(widths[j], lipgloss.Width(c))
		}
	}
	out := make([]string, 0, len(cells))
	for _, cols := range cells {
		var b strings.Builder
		for j, c := range cols {
			b.WriteString(c)
			if j < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-lipgloss.Width(c)+2))
			}
		}
		out = append(out, clipTo(b.String(), w))
	}
	return out
}

// separator reports the row under a table's header: every cell dashes, with
// an alignment colon at either end.
func separator(cols []string) bool {
	for _, c := range cols {
		if !mdSeparator.MatchString(c) {
			return false
		}
	}
	return len(cols) > 0
}
