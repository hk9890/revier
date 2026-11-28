package tui

import (
	"cmp"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/theme"
)

// The curtain covers one monitor while a full shutdown or a restore from the
// popup runs, and shows the walk's progress alone. The
// surface that runs the walk sends its state over the curtain's socket; the
// curtain draws the latest one and sends back what the user pressed. The
// main monitor's curtain lists every step; another monitor's shows the
// title and the bar. A curtain ends when its socket does.

// The bars' widths, and the columns right of a bar that its count takes.
const (
	curtainBar          = 62
	curtainSecondaryBar = 40
	curtainCount        = 10
)

// curtainButtonLines is how many lines the button adds under the block: two
// blank, the button, one blank and the hint.
const curtainButtonLines = 5

// curtainFrame is how often the spinner of the step that runs turns.
const curtainFrame = 100 * time.Millisecond

// curtainModel is one curtain.
type curtainModel struct {
	client     *curtain.Client
	main       bool
	th         theme.Theme
	state      curtain.State
	width      int
	height     int
	frame      int
	compact    bool
	cancelling bool // the user cancelled here; the state says so once the surface took it
}

// curtainStateMsg is a state the surface sent; ok is false once the socket
// ended.
type curtainStateMsg struct {
	state curtain.State
	ok    bool
}

// curtainTickMsg turns the spinner.
type curtainTickMsg struct{}

// NewCurtain is the curtain of one monitor, on the socket client reached.
// main is the primary monitor's curtain, which lists the steps.
func NewCurtain(client *curtain.Client, main bool, th theme.Theme) tea.Model {
	return curtainModel{client: client, main: main, th: th}
}

func (m curtainModel) Init() tea.Cmd {
	return tea.Batch(m.waitState(), curtainTick())
}

// waitState delivers the next state of the socket to the update loop.
func (m curtainModel) waitState() tea.Cmd {
	states := m.client.States()
	return func() tea.Msg {
		st, ok := <-states
		return curtainStateMsg{state: st, ok: ok}
	}
}

func curtainTick() tea.Cmd {
	return tea.Tick(curtainFrame, func(time.Time) tea.Msg { return curtainTickMsg{} })
}

// send tells the surface what the user pressed. A send that fails means the
// socket ended, and its end quits the curtain.
func (m curtainModel) send(cmd curtain.Command) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		_ = client.Send(cmd)
		return nil
	}
}

func (m curtainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case curtainStateMsg:
		if !msg.ok {
			return m, tea.Quit
		}
		before := m.shape()
		m.state = msg.state
		if m.shape() != before {
			return m, tea.Batch(tea.ClearScreen, m.waitState())
		}
		return m, m.waitState()
	case curtainTickMsg:
		m.frame++
		return m, curtainTick()
	case tea.KeyPressMsg:
		return m.key(msg.String())
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		if b := m.layout().button; b.row >= 0 && mouse.Y == b.row && mouse.X >= b.from && mouse.X < b.to {
			return m.press()
		}
	}
	return m, nil
}

// key is a key press on the curtain.
func (m curtainModel) key(k string) (tea.Model, tea.Cmd) {
	if k == "tab" && m.main {
		m.compact = !m.compact
		return m, tea.ClearScreen
	}
	if end := m.state.End; end != nil {
		if !end.OK && (k == "enter" || k == "esc" || k == "ctrl+c") {
			return m, m.send(curtain.Close)
		}
		return m, nil
	}
	switch k {
	case "ctrl+c":
		if m.isCancelling() {
			return m, m.send(curtain.Quit)
		}
		m.cancelling = true
		return m, m.send(curtain.Cancel)
	case "esc":
		return m, m.send(curtain.Hide)
	}
	return m, nil
}

// press is a click on the button: Cancel while the walk runs, Close on a
// summary.
func (m curtainModel) press() (tea.Model, tea.Cmd) {
	switch {
	case m.state.End != nil:
		return m, m.send(curtain.Close)
	case m.isCancelling():
		return m, nil
	}
	m.cancelling = true
	return m, m.send(curtain.Cancel)
}

func (m curtainModel) isCancelling() bool { return m.cancelling || m.state.Cancelling }

func (m curtainModel) View() tea.View {
	v := tea.NewView(m.layout().text)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// curtainButton is where the button is on the screen; row is -1 when the
// screen has none.
type curtainButton struct{ row, from, to int }

// curtainScreen is the screen as text, and where its button is.
type curtainScreen struct {
	text   string
	button curtainButton
}

// layout draws the screen: a block of lines in the middle of the monitor.
func (m curtainModel) layout() curtainScreen {
	lines, label, hint := m.lines()
	buttonAt := -1
	var button string
	if label != "" {
		lines = append(lines, "", "")
		buttonAt = len(lines)
		button = m.buttonStyle().Render("  " + label + "  ")
		lines = append(lines, button, "", m.th.Help.Render(hint))
	}
	blockWidth := m.walkWidth()
	if m.state.End != nil {
		blockWidth = 0
		for _, l := range lines {
			blockWidth = max(blockWidth, lipgloss.Width(l))
		}
	}
	top, left := max(0, (m.height-len(lines))/2), max(0, (m.width-blockWidth)/2)
	pad := strings.Repeat(" ", left)
	var b strings.Builder
	b.WriteString(strings.Repeat("\n", top))
	for _, l := range lines {
		b.WriteString(pad + m.clip(l, m.width-left) + "\n")
	}
	screen := curtainScreen{text: b.String(), button: curtainButton{row: -1}}
	if buttonAt >= 0 {
		screen.button = curtainButton{row: top + buttonAt, from: left, to: left + lipgloss.Width(button)}
	}
	return screen
}

// walkWidth is the width the block is centred on while the walk runs. No
// note and no line that says what runs counts: they come and go with the
// steps, and the block would move with them.
func (m curtainModel) walkWidth() int {
	width := m.barWidth() + curtainCount
	if m.main && !m.compact {
		for _, r := range m.projectRows() {
			width = max(width, lipgloss.Width(r.steps))
		}
	}
	return width
}

// curtainShape is how many lines the block has, and its button.
type curtainShape struct {
	lines int
	label string
}

// shape is the block's form. A state that changes it is drawn on a cleared
// screen: in one restore on GNOME, kitty kept the old button and hint beside
// the new ones where the renderer moved lines to draw the summary.
func (m curtainModel) shape() curtainShape {
	lines, label, _ := m.lines()
	return curtainShape{lines: len(lines), label: label}
}

// lines is the block above the button, the button's label, and the hint
// under it. A screen with no button has no label.
func (m curtainModel) lines() (lines []string, label, hint string) {
	st, th := m.state, m.th
	end := st.End
	lines = append(lines, th.Header.Render(m.title()), "", m.bar())
	if !m.main {
		if end == nil {
			lines = append(lines, "", th.Help.Render("details on the main monitor"))
		} else if end.Summary != "" {
			lines = append(lines, "", th.Meta.Render(end.Summary))
		}
		return lines, "", ""
	}
	lines = append(lines, "")
	switch {
	case end != nil && end.OK:
		if end.Summary != "" {
			lines = append(lines, th.Running.Render(end.Summary))
		}
		return lines, "", ""
	case m.compact && end != nil:
		lines = append(lines, th.Meta.Render(end.Summary))
		var failed []string
		for _, r := range st.Rows {
			if r.State == curtain.RowFailed {
				failed = append(failed, th.Attention.Render(m.icon(r.State)+" "+r.Project+" "+r.Label+": "+oneLine(r.Note)))
			}
		}
		lines = append(lines, m.fit(failed, nil, m.room(len(lines)))...)
	case m.compact:
		lines = append(lines, m.runs())
	default:
		var rows []string
		var first []bool
		for _, r := range m.projectRows() {
			row := r.steps
			if r.note != "" {
				row += "   " + r.note
			}
			rows, first = append(rows, row), append(first, r.first)
		}
		tail := []string{"", m.runs()}
		if end != nil {
			tail[1] = th.Meta.Render(end.Summary)
		}
		lines = append(lines, m.fit(rows, first, m.room(len(lines)+len(tail)))...)
		lines = append(lines, tail...)
	}
	switch {
	case end != nil:
		return lines, "Close", "enter or esc close"
	case m.isCancelling():
		return lines, "Cancelling…", "stops after the step that runs · ctrl+c again quits now"
	}
	return lines, "Cancel", "ctrl+c cancel · esc hide · tab layout"
}

// runs is the line that says what runs now. It is drawn before the first
// report too, so that the block keeps its height.
func (m curtainModel) runs() string {
	return m.spinner() + " " + m.th.Meta.Render(m.state.Line+" …")
}

// room is how many rows fit between the fixed lines above them and the
// button under them; on a monitor not yet sized, any number.
func (m curtainModel) room(fixed int) int {
	if m.height <= 0 {
		return math.MaxInt
	}
	return max(0, m.height-fixed-curtainButtonLines)
}

// fit is as many rows as room holds, in their order, and a last line that
// counts the rest. A row that first marks goes in before any other, so a
// step that runs or failed stays in sight.
func (m curtainModel) fit(rows []string, first []bool, room int) []string {
	if len(rows) <= room {
		return rows
	}
	if room == 0 {
		return nil
	}
	shown := make([]bool, len(rows))
	n := 0
	for _, firstOnly := range []bool{true, false} {
		for i := range rows {
			if n < room-1 && !shown[i] && (!firstOnly || first != nil && first[i]) {
				shown[i] = true
				n++
			}
		}
	}
	var out []string
	for i, r := range rows {
		if shown[i] {
			out = append(out, r)
		}
	}
	return append(out, m.th.Help.Render(fmt.Sprintf("+%d more", len(rows)-n)))
}

// clip cuts a line to width, so that it stays one line. A monitor not yet
// sized cuts none.
func (m curtainModel) clip(line string, width int) string {
	if m.width <= 0 {
		return line
	}
	return ellipsis(line, width)
}

// oneLine is a note on one line: the lines of an error that joins several
// are set apart by "; ".
func oneLine(note string) string {
	return strings.Join(strings.FieldsFunc(note, unicode.IsControl), "; ")
}

// title names the walk, or how it ended.
func (m curtainModel) title() string {
	if m.state.End != nil {
		return m.state.End.Title
	}
	title := "Restoring your session"
	if m.state.Mode == "shutdown" {
		title = "Shutting down"
	}
	if m.isCancelling() {
		title += " · cancelling"
	}
	return title
}

// bar is the progress bar and the count of steps done.
func (m curtainModel) bar() string {
	width := m.barWidth()
	n, of := m.state.N, m.state.Of
	fill := 0
	if of > 0 {
		fill = width * min(n, of) / of
	}
	return m.th.Accent.Render(strings.Repeat("━", fill)) +
		m.th.Border.Render(strings.Repeat("━", width-fill)) +
		"  " + m.th.Meta.Render(fmt.Sprintf("%d / %d", n, of))
}

func (m curtainModel) barWidth() int {
	if m.main {
		return curtainBar
	}
	return curtainSecondaryBar
}

func (m curtainModel) buttonStyle() lipgloss.Style {
	if m.state.End == nil && m.isCancelling() {
		return m.th.OnHover(m.th.Help)
	}
	return m.th.Cursor
}

func (m curtainModel) spinner() string {
	frames := m.th.Spinner
	if len(frames) == 0 {
		return m.th.Meta.Render(m.th.Glyphs.Working)
	}
	return m.th.Meta.Render(frames[m.frame%len(frames)])
}

// icon is the glyph of a step in a state.
func (m curtainModel) icon(s curtain.RowState) string {
	switch s {
	case curtain.RowDone:
		return m.th.Running.Render(m.th.Glyphs.Keep)
	case curtain.RowFailed:
		return m.th.Attention.Render(m.th.Glyphs.Skip)
	case curtain.RowRunning:
		return m.spinner()
	case curtain.RowSkipped:
		return m.th.Help.Render("-")
	}
	return m.th.Help.Render(m.th.Glyphs.Stopped)
}

// curtainRow is one project's line: its state, its name and each step's
// state and label; and a note on the step that failed or was skipped. first
// marks a project whose step runs or failed.
type curtainRow struct {
	steps, note string
	first       bool
}

// projectRows is one row per project, in the order of the walk.
func (m curtainModel) projectRows() []curtainRow {
	var order []string
	steps := map[string][]curtain.Row{}
	nameWidth := 0
	for _, r := range m.state.Rows {
		if _, seen := steps[r.Project]; !seen {
			order = append(order, r.Project)
			nameWidth = max(nameWidth, lipgloss.Width(r.Project))
		}
		steps[r.Project] = append(steps[r.Project], r)
	}
	th := m.th
	var rows []curtainRow
	for _, project := range order {
		var parts []string
		var note string
		for _, r := range steps[project] {
			parts = append(parts, m.icon(r.State)+" "+th.Meta.Render(r.Label))
			switch {
			case r.State == curtain.RowFailed && note == "":
				note = th.Attention.Render(r.Label + ": " + oneLine(r.Note))
			case r.State == curtain.RowSkipped && note == "":
				note = th.Help.Render(cmp.Or(oneLine(r.Note), "not reached"))
			}
		}
		state := projectState(steps[project])
		name := th.ProjectName
		if state == curtain.RowWaiting || state == curtain.RowSkipped {
			name = th.NameDim
		}
		rows = append(rows, curtainRow{
			steps: m.icon(state) + "  " + name.Render(project+strings.Repeat(" ", nameWidth-lipgloss.Width(project))) +
				"   " + strings.Join(parts, "  "),
			note:  note,
			first: state == curtain.RowRunning || state == curtain.RowFailed,
		})
	}
	return rows
}

// projectState is the state a project's line shows: the step that runs,
// else one that failed, was skipped or waits, else done.
func projectState(rows []curtain.Row) curtain.RowState {
	for _, s := range []curtain.RowState{curtain.RowRunning, curtain.RowFailed, curtain.RowSkipped, curtain.RowWaiting} {
		for _, r := range rows {
			if r.State == s {
				return s
			}
		}
	}
	return curtain.RowDone
}
