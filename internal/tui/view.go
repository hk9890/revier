package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
)

// The chrome above and below the list: the action bar, a line under it, the
// query line, the rule, and the footer. The margin around all of
// it costs two more rows and four more columns.
//
// There is no border. It drew a box around a surface that already fills the
// terminal, which has its own edges, and charged two rows and four columns
// for saying again where they are. The two rules inside do the separating a
// box was doing (decisions.md D51).
const (
	chromeHeight = 5
	queryRow     = 2 // the terminal row of the query line, below the margin
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
// terminal (decisions.md D39): the rows are a grid, and the pane takes the
// width the list does not.
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
	// The query stands in the list's column beside a pane, and scrolls
	// within it rather than running past the pane's border to be cut.
	query := w
	if m.paneWidth() > 0 {
		query = m.listWidth()
	}
	m.input.Width = query - lipgloss.Width(promptMark) - 2
	m.rinput.Width = m.input.Width
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
	if m.sel.active {
		return m.sel.view()
	}
	w, _ := m.inner()
	var b strings.Builder
	// A line under the top one: what can be done to the installation is not
	// what is being looked for, and the two should not read as one block.
	b.WriteString(clipTo(m.top(), w))
	b.WriteString("\n")
	b.WriteString(m.thinRule(w))
	b.WriteString("\n")

	// The query sits on the rule over the rows it filters, as each of the
	// pane's sections has its own over its rows. Beside the list the pane
	// starts level with the query, so the query and the rule stand in the
	// list's column and read as the list's, not the whole screen's
	// (decisions.md D73). A terminal too narrow for both shows the pane in
	// the list's place, under a query as wide as the screen.
	// Beside the pane the rule stops a column short of its border, as every
	// row does, rather than running into it.
	list, rule := m.listWidth(), m.listWidth()-1
	if m.paneWidth() == 0 {
		list, rule = w, w
	}
	head := clipTo(m.subtitle(), list) + "\n" + m.rule(rule)
	switch {
	case m.paneWidth() > 0:
		left := lipgloss.NewStyle().Width(list).Render(head) + "\n" + m.body.View()
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, m.detail.View()))
	case m.paneCols() > 0:
		b.WriteString(head + "\n" + m.detail.View())
	default:
		b.WriteString(head + "\n" + m.body.View())
	}
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
	case dialogProject:
		name = "Project " + string(m.proj)
	case dialogHelp:
		name = "Keyboard shortcuts"
	case dialogSessionName:
		name = "Save the projects open now"
	case dialogShutdown:
		name = "Shutdown"
	default:
		return m.bar()
	}
	return " " + m.theme.Header.Render(name)
}

// thinRule closes the top line off. It starts where every other line starts,
// so it and the rule under the query are one pair rather than two edges.
// Beside a pane it meets the pane's border, which starts under it.
func (m Model) thinRule(width int) string {
	if width < 2 {
		return ""
	}
	line := []rune(strings.Repeat("\u2500", width-1))
	if at := m.listWidth() - 1; m.paneWidth() > 0 && at >= 0 && at < len(line) {
		line[at] = '\u252c'
	}
	return " " + m.theme.Border.Render(string(line))
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
		return " " + m.rinput.View()
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
	case dialogProject:
		file := ""
		if p, ok := m.project(m.proj); ok {
			file = contractHome(p.File)
		}
		return " " + m.theme.Meta.Render("written to "+file+" as it changes")
	case dialogHelp:
		return " " + m.theme.Meta.Render("every key revier answers to")
	case dialogSessions:
		return " " + m.theme.Meta.Render("saved in "+contractHome(session.Dir(m.stateRoot))+", newest first")
	case dialogSessionName:
		return " " + m.sname.View()
	case dialogShutdown:
		return " " + m.theme.Meta.Render(m.shutdownTitle())
	}
	return " " + m.fieldView(m.input, focusList)
}

// rule separates the query from the rows it filters, and says how many of
// them survive it out of how many there are, the way the picker says it.
func (m Model) rule(width int) string {
	head := clipTo(pad0(m.ruleCount()), width)
	line := max(width-lipgloss.Width(head), 0)
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
	case m.dialog == dialogRemote && m.rfilter != "":
		return th.NameDim.Render(fmt.Sprintf("%d/%d projects", len(m.rlist.VisibleItems()), len(m.rlist.Items())))
	case m.dialog == dialogRemote:
		return th.NameDim.Render(fmt.Sprintf("%d projects", len(m.rlist.Items())))
	case m.dialog == dialogSessions:
		return th.NameDim.Render(core.Count(len(m.slist.Items()), "session"))
	case m.dialog == dialogNew, m.dialog == dialogLinkName, m.dialog == dialogConfig, m.dialog == dialogProject, m.dialog == dialogHelp, m.dialog == dialogSessionName, m.dialog == dialogShutdown:
		return ""
	case !m.ready():
		return th.NameDim.Render("surveying")
	}
	return th.NameDim.Render(fmt.Sprintf("%d/%d", len(m.plist.VisibleItems()), len(m.views)))
}

// ready reports whether the survey's numbers can be shown. bubbletea paints
// before the first survey answers, and on those frames the rows are the
// files' alone: the rule says the survey is pending rather than counting
// what is not yet known. With no project configured there is nothing to
// wait for.
func (m Model) ready() bool {
	return m.surveyed || len(m.projects) == 0
}

// empty is what the list shows in place of rows, in revier's words
// rather than the list component's "No items.": where to add a project when
// none is configured, and that the filter is why the list is empty when it
// is. The rows before the first survey are the files' alone, so a query that
// matches none of them is why the list is empty then too.
//
// It wraps rather than clips: the directory is the part worth reading, and a
// scratch REVIER_CONFIG_HOME is longer than the list is wide.
func (m Model) empty() string {
	th := m.theme
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(m.listWidth()).Render(text)
	}
	switch {
	case m.dialog == dialogSessions:
		return say(th.NameDim, "No saved sessions. "+sessionsBarKey+" saves the projects open now.")
	case m.dialog == dialogRemote && m.rfilter != "":
		return say(th.NameDim, fmt.Sprintf("No project on %s matches %q.", m.host, m.rfilter))
	case m.dialog != dialogNone:
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
	if m.dialog == dialogProject && m.dropping {
		return m.dropProjectPrompt()
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
	if m.restoring != "" || m.saving {
		return m.progressLine()
	}
	if m.shut.running && m.shut.saves() {
		return m.theme.Meta.Render(" saving the session when it changed, and closing…")
	}
	if m.shut.running {
		return m.theme.Meta.Render(" closing…")
	}
	if m.copied > 0 {
		unit := "characters"
		if m.copied == 1 {
			unit = "character"
		}
		return m.theme.NameDim.Render(fmt.Sprintf(" Copied %d %s.", m.copied, unit))
	}
	if m.dialog == dialogConfig {
		return " " + m.help.ShortHelpView(m.keys.helpForConfig(m.configHelp()))
	}
	if m.dialog == dialogProject {
		return " " + m.help.ShortHelpView(m.projectHelp())
	}
	if m.dialog == dialogNew {
		return " " + m.help.ShortHelpView(m.newHelp())
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
	keys = append(keys, m.keys.Close, m.keys.Edit, m.keys.Delete)
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
