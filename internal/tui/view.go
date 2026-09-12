package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
)

// The chrome above and below the list: a header, the query line, the action
// bar, the rule under it, and the footer. The frame around all of it costs
// two more rows and two columns, and the margin outside the frame two more
// of each.
const (
	chromeHeight = 5
	frameHeight  = 2
	frameWidth   = 4 // border and one column of padding on each side
	marginRows   = 1
	marginCols   = 2
)

// margins are what surrounds the frame: nothing on a small terminal, where
// four rows and four columns of empty space cost two project rows, and one
// row and two columns otherwise. The rows go first, because rows are what a
// list is short of: a wide and short terminal keeps the columns.
func (m Model) margins() (rows, cols int) {
	if m.width < 100 {
		return 0, 0
	}
	if m.height < 24 {
		return 0, marginCols
	}
	return marginRows, marginCols
}

// inner is the size available inside the frame and the margin. The frame
// takes the whole terminal (decisions.md D38): the rows are a grid, so a
// wide row is a long activity line and not a state a screen away from its
// name.
func (m Model) inner() (w, h int) {
	mr, mc := m.margins()
	w = m.width - 2*mc - frameWidth
	h = m.height - 2*mr - frameHeight - chromeHeight
	if w < 20 {
		w = 20
	}
	if h < 2 {
		h = 2
	}
	return w, h
}

// layout gives the lists whatever the header and footer leave. It runs on
// every size change and once at construction, so a model that never receives
// a WindowSizeMsg still renders.
func (m *Model) layout() {
	w, h := m.inner()
	m.input.Width = w - lipgloss.Width(promptMark) - 2
	// The lists are sized by syncBody, which gives them room for every row
	// they hold; this viewport is the part of that the screen shows.
	m.body.Width, m.body.Height = m.listWidth(), h
	// One column less than the frame's content: the footer is rendered with a
	// leading space. help truncates on its own width and marks the cut with an
	// ellipsis; sized to the terminal instead, it never cut, and View's own
	// clip took the end of a word with no mark.
	m.help.Width = w - 1
}

func (m Model) View() string {
	w, _ := m.inner()
	var b strings.Builder
	b.WriteString(clipTo(m.header(), w))
	b.WriteString("\n")
	b.WriteString(clipTo(m.subtitle(), w))
	b.WriteString("\n")
	b.WriteString(clipTo(m.bar(), w))
	b.WriteString("\n")
	b.WriteString(m.rule(w))
	b.WriteString("\n")

	// The pane beside the list, or in its place on a terminal too narrow
	// for both (decisions.md D42).
	body := m.body.View()
	switch {
	case m.paneWidth() > 0:
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.detail.View())
	case m.paneCols() > 0:
		body = m.detail.View()
	}
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(clipTo(m.footer(), w))

	mr, mc := m.margins()
	// Width is the frame's outside, and the frame pads by one column on each
	// side, so the content box is w.
	return m.theme.Frame.
		Margin(mr, mc).
		Width(w + 2).
		Render(b.String())
}

// subtitle is the line under the header: the query, where typing filters,
// or what the link dialog's rows are.
func (m Model) subtitle() string {
	switch m.dialog {
	case dialogHosts:
		return "  " + m.theme.Meta.Render("the hosts ~/.ssh/config names")
	case dialogRemote:
		return "  " + m.theme.Meta.Render("projects the revier on "+m.host+" has")
	case dialogNew:
		return " " + m.path.View()
	}
	return m.promptView()
}

// rule separates the chrome from the list, and carries the count the way the
// picker does: how many rows survive the filter, out of how many there are.
func (m Model) rule(width int) string {
	count := fmt.Sprintf(" %d/%d ", len(m.plist.VisibleItems()), len(m.views))
	switch {
	case m.dialog == dialogHosts:
		count = fmt.Sprintf(" %d hosts ", len(m.hlist.Items()))
	case m.dialog == dialogRemote:
		count = fmt.Sprintf(" %d projects ", len(m.rlist.Items()))
	case m.dialog == dialogNew:
		count = ""
	case !m.ready():
		count = ""
	}
	line := width - lipgloss.Width(count)
	if line < 0 {
		line = 0
	}
	return m.theme.NameDim.Render(count) + m.theme.Border.Render(strings.Repeat("─", line))
}

// header is the one line that says what is on screen. It
// counts, because with ninety projects the counts are the reason to look. The
// filter is not here: it has its own line, with a cursor on it.
//
// A count that is zero is grey. "0 need you" in bold red read as an alarm on
// every screen where nothing was wrong.
func (m Model) header() string {
	th := m.theme
	badge := th.Badge.Render("revier") + " "
	switch m.dialog {
	case dialogHosts:
		return badge + th.NameDim.Render("› ") + th.Header.Render("link a project on another machine")
	case dialogRemote:
		return badge + th.NameDim.Render("› ") + th.Header.Render(m.host)
	case dialogNew:
		return badge + th.NameDim.Render("› ") + th.Header.Render("add a project on this machine")
	}
	if !m.ready() {
		return badge + th.NameDim.Render("surveying")
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
	// Each count carries the glyph its rows carry, so the header is also the
	// key to the list.
	count := func(glyph string, n int, text string, s lipgloss.Style) string {
		if n == 0 {
			s = th.Count
		}
		return s.Render(fmt.Sprintf("%s %d %s", glyph, n, text))
	}
	need := "need you"
	if attention == 1 {
		need = "needs you"
	}
	sep := th.Path.Render(" · ")
	return badge + th.Header.Render(fmt.Sprintf("%d projects", len(m.views))) +
		sep + count(th.Glyphs.Running, running, "running", th.Running) +
		sep + count(th.Glyphs.NeedsYou, attention, need, th.Attention)
}

// ready reports whether the survey's numbers can be shown. bubbletea paints
// once before the first survey answers, and on that frame every count is zero
// and the list is empty, which says there are no projects when there are
// ninety. With no project configured there is nothing to wait for.
func (m Model) ready() bool {
	return m.surveyed || len(m.projects) == 0
}

// empty is what the list shows in place of rows, in revier's words
// rather than the list component's "No items.": nothing before the first
// survey, where to add a project when none is configured, and that the filter
// is why the list is empty when it is.
//
// It wraps rather than clips: the directory is the part worth reading, and a
// scratch REVIER_CONFIG_HOME is longer than the list is wide.
func (m Model) empty() string {
	th := m.theme
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(m.listWidth()).Render(text)
	}
	switch {
	case m.dialog != dialogNone || !m.ready():
		return ""
	case len(m.projects) == 0:
		where := "projects/<name>.toml under the configuration directory"
		if root, err := config.Root(); err == nil {
			where = contractHome(filepath.Join(root, "projects")) + "/<name>.toml"
		}
		return say(th.NameDim, "No projects configured. Add one as") + "\n" + say(th.Path, where)
	default:
		return say(th.NameDim, fmt.Sprintf("No project matches %q.", m.filter))
	}
}

// footer is the key legend, or the last failure. An error replaces the legend
// rather than being added to it: a survey that failed is the only thing worth
// reading on that line.
func (m Model) footer() string {
	if m.confirm != "" {
		return m.deletePrompt()
	}
	if m.asking != "" {
		return m.askingLine()
	}
	err := m.err
	if err == nil {
		err = m.surveyErr
	}
	if err != nil {
		// One line, whatever the error: a joined error is one per line, and a
		// second line in the footer pushes the frame past the terminal.
		return m.theme.Attention.Render(" " + strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	if m.dialog != dialogNone {
		return " " + m.help.ShortHelpView(m.keys.helpForDialog(m.dialog))
	}
	keys := m.keys.helpFor(m.focus)
	if v, ok := m.selected(); ok {
		keys = append(keys, m.keys.targetHelp(m.targetKeysOf(v))...)
	}
	// Last, so a narrow footer cuts the file keys and not the row's own
	// target keys: those change from row to row, and these never do.
	keys = append(keys, m.keys.Edit, m.keys.Delete)
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

// clipTo cuts text to a width, keeping the start: for a name, an activity
// line or a tree row the beginning is what identifies it. It is ANSI-aware,
// so it can be given text that already carries styling.
func clipTo(s string, width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// wrap breaks text into lines no wider than width: at a space or a hyphen
// where there is one, and mid-word where there is not. It trims the padding
// lipgloss adds, so a line is only as wide as its text.
func wrap(s string, width int) []string {
	if width < 1 {
		return nil
	}
	out := strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

// hang puts a value right of an already rendered head and wraps it within
// width, continuing under the value rather than under the head, so a label
// column stays a column.
func hang(head, value string, width int, style lipgloss.Style) string {
	indent := lipgloss.Width(head)
	parts := wrap(value, width-indent)
	for i, p := range parts {
		parts[i] = style.Render(p)
	}
	return head + strings.Join(parts, "\n"+strings.Repeat(" ", indent))
}

// ellipsis cuts text to a width, keeping the start and marking the cut: an
// activity line reads from its first word, and a cut with no mark reads as
// the whole sentence.
func ellipsis(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return clipTo(s, width-1) + "…"
}

// elide cuts a path in the middle: the start says where checkouts live, the
// end says which checkout this is, and a row has room to say both. A third of
// the width goes to the start, because the end is the part that differs.
func elide(s string, width int) string {
	if width < 4 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	head := width / 3
	tail := width - head - 1
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
