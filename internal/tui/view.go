package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// The chrome above and below the list: the action bar, a line under it, the
// query line, a blank line, the rule, and the footer. The margin around all of
// it costs two more rows and four more columns.
//
// There is no border. It drew a box around a surface that already fills the
// terminal, which has its own edges, and charged two rows and four columns
// for saying again where they are. The two rules inside do the separating a
// box was doing (decisions.md D51).
const (
	chromeHeight = 6
	marginRows   = 1
	marginCols   = 2
)

// margins are what surrounds the surface: nothing on a small terminal, where
// two rows and four columns of empty space cost a project row, and one row
// and two columns otherwise. The rows go first, because rows are what a
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

// inner is the size available inside the margin. The surface takes the whole
// terminal (decisions.md D38): the rows are a grid, so a wide row is a long
// activity line and not a state a screen away from its name.
func (m Model) inner() (w, h int) {
	mr, mc := m.margins()
	w = m.width - 2*mc
	h = m.height - 2*mr - chromeHeight
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
	// One column less than the content: the footer is rendered with a
	// leading space. help truncates on its own width and marks the cut with an
	// ellipsis; sized to the terminal instead, it never cut, and View's own
	// clip took the end of a word with no mark.
	m.help.Width = w - 1
}

func (m Model) View() string {
	w, _ := m.inner()
	var b strings.Builder
	// A line under the top one: what can be done to the installation is not
	// what is being looked for, and the two should not read as one block.
	b.WriteString(clipTo(m.top(), w))
	b.WriteString("\n")
	b.WriteString(m.thinRule(w))
	b.WriteString("\n")
	// A blank line under the query: the field stands on its own, and the
	// rule reads as the head of the list rather than the query's underline.
	b.WriteString(clipTo(m.subtitle(), w))
	b.WriteString("\n\n")
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
	return m.theme.Frame.Margin(mr, mc).Width(w).Render(b.String())
}

// top is the surface's first line: the action bar on the surface, and the name
// of a dialog standing over it. A dialog takes the bar's place rather than a
// line of its own, because none of the bar's buttons acts while one is up.
func (m Model) top() string {
	name := ""
	switch m.dialog {
	case dialogHosts:
		name = "Link a project on another machine"
	case dialogRemote:
		name = m.host
	case dialogLinkName:
		name = "Name the link"
		if it, ok := m.rlist.SelectedItem().(remoteItem); ok {
			name += " to " + string(it.view.Project.Name) + " on " + m.host
		}
	case dialogNew:
		name = "Add a project on this machine"
	case dialogConfig:
		name = "Configuration"
	case dialogHelp:
		name = "Keyboard shortcuts"
	default:
		return m.bar()
	}
	return " " + m.theme.Header.Render(name)
}

// thinRule closes the top line off. It starts where every other line starts,
// so it and the rule under the query are one pair rather than two edges.
func (m Model) thinRule(width int) string {
	if width < 2 {
		return ""
	}
	return " " + m.theme.Border.Render(strings.Repeat("\u2500", width-1))
}

// subtitle is the line over the rule: the query, where typing filters, or
// what the dialog's rows are. The hosts step says nothing there - the title
// over it already says what the rows are, and the rule under it counts them -
// but it keeps the line, so the rows do not move as the step changes.
func (m Model) subtitle() string {
	switch m.dialog {
	case dialogHosts:
		return ""
	case dialogRemote:
		return " " + m.theme.Meta.Render("the projects on "+m.host)
	case dialogLinkName:
		return m.linkNameView()
	case dialogNew:
		return " " + m.path.View()
	case dialogConfig:
		where := "config.toml"
		if root, err := config.Root(); err == nil {
			where = contractHome(config.File(root))
		}
		return " " + m.theme.Meta.Render("written to "+where+" as it changes")
	case dialogHelp:
		return " " + m.theme.Meta.Render("every key revier answers to")
	}
	return m.promptView()
}

// rule separates the chrome from the list and carries every number on the
// screen: how many rows survive the filter out of how many there are, the way
// the picker says it, and beside that how much of the list is doing
// something. The two counts had a line of their own and did not earn it -
// they are three words that never move - so they sit on the line that was
// already mostly empty.
func (m Model) rule(width int) string {
	head := pad0(m.ruleHead())
	// A list too narrow for all of it keeps the ratio and drops the counts:
	// the ratio is the number that changes as you type.
	if lipgloss.Width(head) > width {
		head = pad0(m.ruleCount())
	}
	head = clipTo(head, width)
	line := width - lipgloss.Width(head)
	if line < 0 {
		line = 0
	}
	return head + m.theme.Border.Render(strings.Repeat("─", line))
}

// pad0 puts a space on each side of a rule's head, so its text does not touch
// the margin or the line. An empty head stays empty.
func pad0(head string) string {
	if head == "" {
		return ""
	}
	return " " + head + " "
}

// ruleCount is how many rows are under the rule: how many the filter left out
// of how many there are, the way the picker says it, or what the step of a
// dialog is listing.
func (m Model) ruleCount() string {
	th := m.theme
	switch {
	case m.dialog == dialogHosts:
		return th.NameDim.Render(fmt.Sprintf("%d hosts", len(m.hlist.Items())))
	case m.dialog == dialogRemote:
		return th.NameDim.Render(fmt.Sprintf("%d projects", len(m.rlist.Items())))
	case m.dialog == dialogNew, m.dialog == dialogLinkName, m.dialog == dialogConfig, m.dialog == dialogHelp:
		return ""
	case !m.ready():
		return th.NameDim.Render("surveying")
	}
	return th.NameDim.Render(fmt.Sprintf("%d/%d", len(m.plist.VisibleItems()), len(m.views)))
}

// ruleHead is what the rule says before its line: the count, and on the
// surface how much of the list is doing something.
//
// A count that is zero is grey. "0 need you" in bold red read as an alarm on
// every screen where nothing was wrong.
func (m Model) ruleHead() string {
	th := m.theme
	if m.dialog != dialogNone || !m.ready() {
		return m.ruleCount()
	}
	blockers, working, idle := m.agentCounts()
	// Each count carries the glyph its rows carry, so the rule is also the
	// key to the list.
	count := func(glyph string, n int, text string, s lipgloss.Style) string {
		if n == 0 {
			s = th.Count
		}
		return s.Render(fmt.Sprintf("%s %d %s", glyph, n, text))
	}
	blocked := "blockers"
	if blockers == 1 {
		blocked = "blocker"
	}
	sep := th.Path.Render(" · ")
	return m.ruleCount() +
		sep + count(th.Glyphs.NeedsYou, blockers, blocked, th.Attention) +
		sep + count(th.Glyphs.Working, working, "working", th.Running) +
		sep + count(th.Glyphs.Idle, idle, "idle", th.Idle)
}

// agentCounts is the list by what its agents are doing: a project counts
// once, under its worst agent, which is the state its row shows. A project
// with no agent - a workspace that is only a shell - counts in none of them.
func (m Model) agentCounts() (blockers, working, idle int) {
	for _, v := range m.views {
		worst, ok := core.Worst(v.Agents)
		if !ok {
			continue
		}
		switch worst.Status {
		case revier.StatusAttention:
			blockers++
		case revier.StatusRunning:
			working++
		case revier.StatusIdle:
			idle++
		}
	}
	return blockers, working, idle
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
	if m.dialog == dialogConfig && m.dropping {
		return m.dropPrompt()
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
	if m.dialog == dialogConfig {
		return " " + m.help.ShortHelpView(m.keys.helpForConfig(m.configHelp()))
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

// fill pads a rendered row to the width of the list, so the highlight on a
// selected or hovered row spans the row instead of ending at the last
// character. style is the row's own: whatever background its segments carry,
// the padding carries too.
func fill(row string, width int, style func(lipgloss.Style) lipgloss.Style) string {
	gap := width - lipgloss.Width(row)
	if gap <= 0 {
		return row
	}
	return row + style(lipgloss.NewStyle()).Render(strings.Repeat(" ", gap))
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
