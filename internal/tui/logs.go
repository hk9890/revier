package tui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/theme"
)

// The logs screen: the WARN and ERROR lines of this machine's log, over the
// days the log keeps, newest first. Enter on a line shows every field of it.
// It opens from every screen on alt+l and a click on the footer's error, from
// the surface on its button on the top bar too, and Esc goes back to the
// screen it came from.
// A link's operations log on its host, so this machine's file is all it reads
// (decisions.md D157). Every field is drawn as text (D108).

// logsKey opens the screen from any other, and closes it.
const logsKey = "alt+l"

// maxLogMsg and maxLogProject are the widest the message and the project
// columns grow; a longer one is cut.
const (
	maxLogMsg     = 28
	maxLogProject = 20
)

// logItem is one row of the screen.
type logItem struct{ line logging.Line }

func (i logItem) FilterValue() string { return i.line.Msg }

// logDelegate draws a line as its time, its message, its project and the
// first line of its error, each column as wide as the widest on the screen.
// An ERROR's message is in the colour of an error, a WARN's in a warning's.
type logDelegate struct {
	theme     theme.Theme
	today     time.Time // the day a line's time is given without its date
	timeWidth int
	msgWidth  int
	projWidth int
}

func (d logDelegate) Height() int                         { return 1 }
func (d logDelegate) Spacing() int                        { return 0 }
func (d logDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d logDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(logItem)
	if !ok {
		return
	}
	th, l := d.theme, it.line
	sel := index == m.Index()
	style := func(st lipgloss.Style) lipgloss.Style {
		if sel {
			return th.OnSelection(st)
		}
		return st
	}
	cell := func(text string, width int, st lipgloss.Style) string {
		return style(st).Render(pad(ellipsis(text, width), width+2))
	}
	level := th.Warning
	if l.Level == "ERROR" {
		level = th.Attention
	}
	row := cursor(th, sel) + cell(logTime(l.Time, d.today), d.timeWidth, th.Path) +
		cell(firstLine(l.Msg), d.msgWidth, level)
	if d.projWidth > 0 {
		row += cell(firstLine(l.Project), d.projWidth, th.ProjectName)
	}
	row += style(th.NameDim).Render(firstLine(l.Err))
	_, _ = fmt.Fprint(w, fill(clipTo(row, m.Width()), m.Width(), style))
}

// logTime is a line's time as a row gives it: the clock alone on the day it
// is read, and the date with it before.
func logTime(t, today time.Time) string {
	t = t.In(today.Location())
	if y, m, d := t.Date(); y == today.Year() && m == today.Month() && d == today.Day() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 02 15:04")
}

// firstLine is the first line of a field, as text (decisions.md D108).
func firstLine(s string) string {
	first, _, _ := strings.Cut(plainText(s), "\n")
	return first
}

// logsScreen is the screen's rows, and whether one line is read in full.
type logsScreen struct {
	list    list.Model
	skipped int    // the lines of the log that did not decode
	reading bool   // the line under the cursor is shown in full
	from    dialog // the screen it was opened from, which Esc goes back to
	offset  int    // how far that screen was scrolled, which Esc gives back
}

func newLogsScreen(th theme.Theme) logsScreen {
	return logsScreen{list: plainList(logDelegate{theme: th})}
}

// logsResult is what a press on the screen leaves for the surface.
type logsResult struct {
	closed bool // back to the screen it came from
}

// openLogs is alt+l, a click on the footer's error and a click on its button
// on the top bar. When the footer shows an error, the cursor starts on
// the newest line this process wrote by the time it was set (open). The
// screen it opens over keeps its focus, the pane's cursor and its scroll
// too, for Esc to go back to.
func (m Model) openLogs() (tea.Model, tea.Cmd) {
	at, _ := m.footerErr()
	m.logs.from, m.logs.offset = m.dialog, m.body.YOffset()
	m.err = m.logs.open(m.surface(), m.now(), at)
	m.body.SetYOffset(0)
	m.dialog = dialogLogs
	m.layout()
	return m, nil
}

// logsKey is every press on the screen. Reading one line, the movement keys
// scroll it, as they scroll the help screen.
func (m Model) logsKey(msg keyPress) (tea.Model, tea.Cmd) {
	if m.logs.reading {
		if by, ok := m.keys.move(msg, func(int) int { return m.body.Height() }); ok {
			if by < 0 {
				m.body.ScrollUp(-by)
			} else {
				m.body.ScrollDown(by)
			}
			return m, nil
		}
	}
	was := m.logs.reading
	res, cmd := m.logs.key(m.surface(), msg)
	m.err = nil
	if m.logs.reading && !was {
		m.body.SetYOffset(0)
	}
	if res.closed {
		// The screen's own text first: a viewport holds no offset past
		// the end of what it shows.
		m.dialog = m.logs.from
		m.layout()
		m.syncBody()
		m.body.SetYOffset(m.logs.offset)
	}
	return m, cmd
}

// open reads the log into the rows. The cursor starts on the newest line
// this process wrote at or before at, when at is a time: the line of the
// error the footer showed. Nothing ties an error to its line but the time
// it was set, so a warning this process wrote between the operation's line
// and the footer would take the cursor (decisions.md D157).
func (sc *logsScreen) open(sf surface, now, at time.Time) error {
	sc.reading = false
	lines, skipped, err := logging.Read(sf.stateRoot, now)
	sc.skipped = skipped
	items := make([]list.Item, 0, len(lines))
	d := logDelegate{theme: sf.theme, today: now}
	cur, pid := -1, os.Getpid()
	for i, l := range lines {
		items = append(items, logItem{line: l})
		d.timeWidth = max(d.timeWidth, lipgloss.Width(logTime(l.Time, now)))
		d.msgWidth = max(d.msgWidth, lipgloss.Width(firstLine(l.Msg)))
		d.projWidth = max(d.projWidth, lipgloss.Width(firstLine(l.Project)))
		if cur < 0 && !at.IsZero() && l.Pid == pid && !l.Time.After(at) {
			cur = i
		}
	}
	d.msgWidth = min(d.msgWidth, maxLogMsg)
	d.projWidth = min(d.projWidth, maxLogProject)
	sc.list.SetDelegate(d)
	_ = sc.list.SetItems(items)
	sc.list.Select(max(cur, 0))
	return err
}

// key is every press on the screen: the movement keys walk the lines, Enter
// shows the one under the cursor in full, Esc goes back from it to the
// lines and from the lines to the screen before, and the key that opened
// the screen closes it.
func (sc *logsScreen) key(sf surface, msg keyPress) (logsResult, tea.Cmd) {
	var res logsResult
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Logs):
		res.closed = true
	case key.Matches(msg, sf.keys.Back):
		if sc.reading {
			sc.reading = false
			return res, nil
		}
		res.closed = true
	case sc.reading:
		// The root scrolls the line; nothing else acts on it.
	case key.Matches(msg, sf.keys.Enter):
		_, sc.reading = sc.list.SelectedItem().(logItem)
	default:
		if by, ok := sf.keys.move(msg, func(int) int { return sf.page }); ok {
			moveRow(&sc.list, by)
		}
	}
	return res, nil
}

// full is the line under the cursor, every field of it as it was written,
// in the order it was written.
func (sc *logsScreen) full(sf surface) string {
	it, ok := sc.list.SelectedItem().(logItem)
	if !ok {
		return ""
	}
	th, w := sf.theme, sf.list
	fields := it.line.Fields()
	label := 0
	for _, f := range fields {
		label = max(label, lipgloss.Width(plainText(f.Key)))
	}
	level := th.Warning
	if it.line.Level == "ERROR" {
		level = th.Attention
	}
	var b strings.Builder
	b.WriteString(" " + level.Render(clipTo(it.line.Level+" "+firstLine(it.line.Msg), w-1)) + "\n\n")
	for _, f := range fields {
		head := " " + th.Meta.Render(pad(plainText(f.Key), label+2))
		b.WriteString(hang(head, plainText(f.Value), w, th.ProjectName) + "\n")
	}
	return b.String()
}

// help is the footer of the screen.
func (sc *logsScreen) help(k keyMap) []key.Binding {
	if sc.reading {
		return []key.Binding{helpKey("↑↓", "scroll"), helpKey("esc", "back"), helpKey(logsKey, "close"), k.Quit}
	}
	return []key.Binding{helpKey("↑↓", "move"), helpKey("enter", "open"), helpKey("esc", "back"), helpKey(logsKey, "close"), k.Quit}
}

// subtitle is the line over the rule: what the rows are, and where they are
// read from.
func (sc *logsScreen) subtitle(sf surface) string {
	return " " + sf.theme.Meta.Render("warnings and errors of this machine, newest first, from "+config.ContractHome(logging.Dir(sf.stateRoot)))
}

// count is the rule's: the lines listed, and the lines of the log that could
// not be read. It is on the rule, which a narrow terminal does not cut, and
// not after the path.
func (sc *logsScreen) count() string {
	text := core.Count(len(sc.list.Items()), "line")
	if sc.skipped > 0 {
		text += ", " + strconv.Itoa(sc.skipped) + " unreadable"
	}
	return text
}
