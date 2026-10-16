package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The mirror is the part of a pane that shows an agent: what the panel of the
// agent under the cursor shows, read from its terminal once a second, so the
// user watches the agent work without going to it (decisions.md D111). The
// agent list's pane and the project list's pane draw it alike, each for the
// agent under its own cursor (D125).
//
// It is the terminal's own drawing, laid out for the agent's window, and that
// window is wider than the pane: a line that does not fit continues on the
// next at a word, under its own first word, and a rule is shortened to fit
// instead. The frame the harness draws under the conversation is left out
// (D136). The colours are kept, and nothing else the terminal was told: only
// an SGR sequence is passed on, and every line is closed, so what an agent's
// screen holds cannot repaint the surface that shows it.

// mirrorInterval is how often the mirror reads the panel. It has its own
// timer: a survey lists every host here, and the mirror must not wait for
// one.
const mirrorInterval = time.Second

// mirrorNotch is the lines one notch of the wheel scrolls the mirror by.
const mirrorNotch = 3

// mirror is the screen of one agent as last read.
type mirror struct {
	key      listedKey // the agent it shows
	text     string    // the screen as read
	textDeep bool      // whether text holds the scrollback
	err      error     // why the last read gave none
	read     bool      // a read for key has answered
	deep     bool      // the scrollback is asked for: the user scrolled up
	off      int       // the lines the view is scrolled up from the end
	room     int       // the lines the pane last had for it
	width    int       // the columns the pane last set its lines at
	seq      int       // the number of the last read asked for
	took     int       // the number of the last read taken
	ticking  bool      // whether a tick is out, so a switch starts no second one
	gen      int       // the number of the tick that is out; an older one is dropped

	// The lines text was last set as, and the width they were set at: the
	// pane is drawn again on every message, and a scrollback is long. A read
	// that brings another text drops them; one that brings the same text, as
	// every read of an agent at rest does, keeps them.
	lines  []string
	linesW int
}

// mirroredMsg is one read of an agent's screen.
type mirroredMsg struct {
	key  listedKey
	seq  int
	deep bool
	text string
	err  error
}

// mirrorTickMsg starts the next read. gen is the number of the tick.
type mirrorTickMsg struct{ gen int }

func mirrorTick(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return mirrorTickMsg{gen: gen} })
}

// mirrorEvery is how often the mirror reads the panel: once a second with
// the focus, as often as the surface refreshes without it.
func (m Model) mirrorEvery() time.Duration {
	if m.blurred {
		return blurredRefresh
	}
	return mirrorInterval
}

// restart drops the tick that is out, so the next ask reads at once and
// starts a tick at the interval of now: the focus is back on a mirror that
// read slowly.
func (mi *mirror) restart() {
	mi.gen++
	mi.ticking = false
}

// mirrored is the agent the pane mirrors, and the row that names it: the one
// under the agent list's cursor, or beside the project list the one under the
// pane's cursor. None is a pane off the screen, a screen standing over it, or
// a project with no agent.
func (m Model) mirrored() (listedKey, revier.AgentView, bool) {
	if m.hidden || m.dialog != dialogNone || m.paneCols() == 0 {
		return listedKey{}, revier.AgentView{}, false
	}
	if m.agents.shown {
		it, ok := m.agents.selected()
		return it.key(), it.agent, ok
	}
	v, ok := m.selected()
	rows := m.agentRows()
	if !ok || m.acursor >= len(rows) {
		return listedKey{}, revier.AgentView{}, false
	}
	a := rows[m.acursor].agent
	return listedKey{project: v.Project.Name, agent: keyOf(a)}, a, true
}

// mirroring is mirrored for a pane that had a row for the mirror when it was
// last drawn. A pane with none shows no mirror: nothing is read for it, and
// the wheel over it scrolls the pane. Nothing is read either for an agent the
// project pane has not settled on (chooseAgent).
func (m Model) mirroring() (listedKey, revier.AgentView, bool) {
	key, a, ok := m.mirrored()
	return key, a, ok && m.mirror.room > 0 && (m.agents.shown || !m.apending)
}

// ask sends for the screen of the agent the pane mirrors: at once when
// another agent comes under the cursor, and when the mirror comes back into
// view with a screen read before it left, and on every tick after. It starts
// the tick when the mirror comes into view, and the tick ends itself when it
// leaves.
func (mi *mirror) ask(c *core.Core, key listedKey, a revier.AgentView, msg tea.Msg, every time.Duration) tea.Cmd {
	var cmds []tea.Cmd
	back := !mi.ticking
	if back {
		mi.ticking = true
		cmds = append(cmds, mirrorTick(every, mi.gen))
	}
	t, tick := msg.(mirrorTickMsg)
	tick = tick && t.gen == mi.gen
	if key != mi.key {
		*mi = mirror{key: key, ticking: true, gen: mi.gen, seq: mi.seq, room: mi.room, width: mi.width}
	} else if !tick && !back {
		return tea.Batch(cmds...)
	}
	return tea.Batch(append(cmds, mi.readScreen(c, a))...)
}

// readScreen is one read of the agent's panel, off the update loop.
func (mi *mirror) readScreen(c *core.Core, a revier.AgentView) tea.Cmd {
	mi.seq++
	key, seq, deep := mi.key, mi.seq, mi.deep
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), detailWait)
		defer cancel()
		text, err := c.Mirror(ctx, a, deep)
		return mirroredMsg{key: key, seq: seq, deep: deep, text: text, err: err}
	}
}

// take takes in a read. One for an agent the cursor has since left is
// dropped, and so is one older than the read already taken. A read that
// failed keeps the screen the mirror was showing: one kitty call that timed
// out must not blank it. While the user is scrolled up, the view stays on the
// lines it shows as more are written under them.
func (mi *mirror) take(msg mirroredMsg) {
	if msg.key != mi.key || msg.seq <= mi.took {
		return
	}
	if !errors.Is(msg.err, core.ErrNoScreen) {
		// One line per panel that fails: a key for them all would log again
		// each time the cursor went from a panel that reads to one that does
		// not.
		at := msg.key.agent
		logging.Repeat("mirror\x00"+at.host+"\x00"+at.id+"\x00"+string(at.panel), "mirror", msg.err, "panel", at.panel)
	}
	before := len(mi.screenLines())
	mi.took, mi.read, mi.err = msg.seq, true, msg.err
	if msg.err != nil {
		return
	}
	// The first read with the scrollback adds lines above the view, which a
	// view counted from the end does not move for; a later one adds them
	// under it.
	grew := msg.deep && mi.textDeep
	if msg.text != mi.text {
		mi.text, mi.lines = msg.text, nil
	}
	mi.textDeep = msg.deep
	if mi.off > 0 && grew {
		mi.off += max(len(mi.screenLines())-before, 0)
	}
	mi.boundScroll()
}

// scroll moves the mirror's view of the agent's screen by lines, up for a
// positive count. The first move up asks for the scrollback, which the mirror
// does not read while it follows the end; back at the end it stops reading
// it. Until the scrollback is here there is nothing above the screen to stop
// at, so the moves are kept as they come.
func (mi *mirror) scroll(c *core.Core, a revier.AgentView, by int) tea.Cmd {
	if by > 0 && !mi.deep {
		mi.deep, mi.off = true, by
		return mi.readScreen(c, a)
	}
	mi.off = max(mi.off+by, 0)
	mi.boundScroll()
	return nil
}

// boundScroll keeps the view within the scrollback once it is here, and has
// the mirror follow the end again when the view is at it: a scrollback with
// nothing above the pane leaves no line to hold, and a view that held one
// would stand still while the panel wrote on under it.
func (mi *mirror) boundScroll() {
	if mi.textDeep {
		mi.off = min(mi.off, max(len(mi.screenLines())-mi.room, 0))
	}
	if mi.off == 0 {
		mi.deep = false
	}
}

// screenLines is the mirror's text as the pane's lines, at the width the pane
// last drew it, kept while the text and the width stay.
func (mi *mirror) screenLines() []string {
	if mi.lines == nil || mi.linesW != mi.width {
		mi.lines, mi.linesW = screenLines(mi.text, mi.width), mi.width
	}
	return mi.lines
}

// view is the mirror in rows lines, w wide: the end of the screen of the
// agent key names, or the part the user scrolled to. A mirror that holds
// another agent's screen has not been asked for this one yet and says it is
// reading, so an agent is never drawn over the screen of the one the cursor
// left.
//
// The room and the width are kept: a read and a wheel that come before the
// next draw bound the scroll by what is on the screen.
func (mi *mirror) view(th theme.Theme, key listedKey, w, rows int) string {
	mi.room, mi.width = rows, w
	if rows < 1 {
		return ""
	}
	note := func(s string) string {
		parts := wrap(s, w)
		parts = parts[:min(len(parts), rows)]
		for i := range parts {
			parts[i] = th.Meta.Render(parts[i])
		}
		return strings.Join(parts, "\n")
	}
	if key != mi.key {
		return note("reading...")
	}
	lines := mi.screenLines()
	switch {
	case errors.Is(mi.err, core.ErrNoScreen):
		return note("The screen of this agent cannot be read here.")
	case !mi.read:
		return note("reading...")
	case mi.err != nil && len(lines) == 0:
		return note("The screen of this agent could not be read.")
	}
	end := len(lines) - min(mi.off, max(len(lines)-rows, 0))
	return strings.Join(lines[max(end-rows, 0):end], "\n")
}

// sgrReset closes a line that carries a style, so the style ends with it.
const sgrReset = "\x1b[m"

// screenLines sets a panel's screen as lines no wider than w. A row too long
// continues on the next line at a space, under the row's first word, and
// mid-word only where a word is wider than the line; a rule is shortened to
// fit instead. Each line starts in the style the one before it ended in. Of
// what the terminal was told only the SGR sequences are kept - colour, and
// bold and its kin - and every other escape and control character is
// dropped, so a line is as wide measured as it is drawn and can do nothing
// but be read (decisions.md D111). The blank lines a screen ends in are
// dropped, so the mirror ends on the last thing the agent's terminal shows.
func screenLines(text string, w int) []string {
	if w < 1 || text == "" {
		return nil
	}
	var out []string
	var style sgrState
	var cells []cell
	for _, row := range strings.Split(text, "\n") {
		cells = screenCells(row, cells[:0])
		out = append(out, setRow(cells, w, &style)...)
	}
	for len(out) > 0 && ansi.StringWidth(strings.TrimSpace(ansi.Strip(out[len(out)-1]))) == 0 {
		out = out[:len(out)-1]
	}
	return out
}

// cell is one thing a row of the screen holds: a character as wide as it is
// drawn, or an SGR sequence, which takes no column.
type cell struct {
	s     string
	width int
	sgr   []string // the sequence's parameters; nil for a character
}

func (c cell) space() bool { return c.s == " " }

// visible reports a cell that draws something: a character but a space.
func visible(c cell) bool { return c.width > 0 && !c.space() }

// screenCells is one row of a screen as cells, appended to cells: a screen is
// set a row at a time into one slice. A tab is the spaces it is drawn as;
// every escape but an SGR sequence, and every control character, is dropped.
func screenCells(row string, cells []cell) []cell {
	for i := 0; i < len(row); {
		r, size := utf8.DecodeRuneInString(row[i:])
		switch {
		case r == '\x1b':
			seq, n := escapeAt(row[i:])
			i += n
			if params, ok := sgrParams(seq); ok {
				cells = append(cells, cell{s: seq, sgr: params})
			}
			continue
		case r == '\t':
			for range tabWidth {
				cells = append(cells, cell{s: " ", width: 1})
			}
		case r == utf8.RuneError && size == 1:
			cells = append(cells, cell{s: string(r), width: 1})
		case unicode.IsControl(r):
		case r < utf8.RuneSelf && (i+1 == len(row) || row[i+1] < utf8.RuneSelf):
			cells = append(cells, cell{s: row[i : i+1], width: 1})
		default:
			// A character is measured with what joins it, as the terminal
			// draws it: an emoji and its variation selector are one, two
			// cells wide, where the two measured apart come to one.
			cluster, cols := ansi.FirstGraphemeCluster(row[i:], ansi.GraphemeWidth)
			cells = append(cells, cell{s: cluster, width: cols})
			size = len(cluster)
		}
		i += size
	}
	return cells
}

// setRow sets one row of the screen as lines no wider than w, and leaves
// style as the row ends in.
func setRow(cells []cell, w int, style *sgrState) []string {
	cells = fitRule(cells, w)
	// A line under the indent has room for a wide character, and for more
	// than the indent.
	indent := hangingIndent(cells)
	if indent > w/2 || w-indent < 2 {
		indent = 0
	}
	var out []string
	pad := 0
	for start := 0; ; {
		end, next := breakAt(cells, start, w-pad)
		// A row indented past the line, as a harness centres a word, shows
		// the word and not a line of its indent.
		if next < len(cells) && !slices.ContainsFunc(cells[start:end], visible) {
			end = start
		} else {
			out = append(out, setLine(cells[start:end], pad, style))
		}
		for _, c := range cells[end:next] {
			if c.sgr != nil {
				style.apply(c.sgr)
			}
		}
		if next == len(cells) {
			return out
		}
		start, pad = next, indent
	}
}

// breakAt is where a line that starts at cells[start] with room columns ends,
// and where the next line starts. It ends at the last space after a word, and
// the spaces there are dropped; a word wider than the room is cut. A rest of
// spaces alone is dropped too, so it starts no line of its own.
func breakAt(cells []cell, start, room int) (end, next int) {
	width, space, inWord := 0, -1, false
	for i := start; i < len(cells); i++ {
		c := cells[i]
		switch {
		case c.space():
			if inWord {
				space = i
			}
			inWord = false
		case c.width > 0:
			inWord = true
		}
		if width+c.width <= room {
			width += c.width
			continue
		}
		end = i
		switch {
		case space > start:
			end = space
		case i == start:
			end = i + 1 // one character wider than the whole line
		}
		next = end
		for next < len(cells) && (cells[next].space() || cells[next].sgr != nil) {
			next++
		}
		return end, next
	}
	return len(cells), len(cells)
}

// setLine draws one line: indent columns of plain space, then the cells, in
// the style the line before ended in. A line that carries a style is closed,
// so the style ends with it.
func setLine(cells []cell, indent int, style *sgrState) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", indent))
	seq := style.seq()
	b.WriteString(seq)
	styled := seq != ""
	for _, c := range cells {
		if c.sgr != nil {
			style.apply(c.sgr)
			styled = true
		}
		b.WriteString(c.s)
	}
	if styled {
		b.WriteString(sgrReset)
	}
	return b.String()
}

// hangingIndent is the column a row's continuation lines start at: its first
// word, past the row's indent and past the mark a list item starts with - a
// bullet, a symbol, or a number with a dot or a parenthesis - and the spaces
// after that mark.
func hangingIndent(cells []cell) int {
	const (
		inIndent = iota
		inNumber
		pastMark
		pastSpace
	)
	lead, col, at := 0, 0, inIndent
	for _, c := range cells {
		if c.sgr != nil {
			continue
		}
		digit := len(c.s) == 1 && c.s[0] >= '0' && c.s[0] <= '9'
		switch {
		case at == inIndent && c.space():
			lead++
		case at == inIndent && digit:
			at, col = inNumber, lead+1
		case at == inIndent && listMark(c.s):
			at, col = pastMark, lead+c.width
		case at == inNumber && digit:
			col++
		case at == inNumber && (c.s == "." || c.s == ")"):
			at, col = pastMark, col+1
		case (at == pastMark || at == pastSpace) && c.space():
			at, col = pastSpace, col+1
		case at == pastSpace:
			return col
		default:
			return lead
		}
	}
	if at == pastSpace {
		return col
	}
	return lead
}

// listMark reports a character that marks a list item or a block when a
// space follows it: an ASCII bullet, or any symbol that is no letter or
// digit, such as the dots and corners a harness marks its turns with.
func listMark(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	switch r {
	case '-', '*', '+', '>':
		return true
	}
	return r >= utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// ruleRun is the fewest characters of a rule that still read as one.
const ruleRun = 3

// fitRule shortens a row wider than w that holds a rule - a run of one
// line-drawing character - by what the row overflows, so the rule and any
// label on it stay on one line. A row with no run that long is left to wrap.
func fitRule(cells []cell, w int) []cell {
	over := -w
	for _, c := range cells {
		over += c.width
	}
	if over <= 0 {
		return cells
	}
	at, run := 0, 0
	for i := 0; i < len(cells); {
		j := i
		for j < len(cells) && isRule(cells[j].s) && cells[j].s == cells[i].s {
			j++
		}
		if j-i > run {
			at, run = i, j-i
		}
		i = max(j, i+1)
	}
	if run-over < ruleRun {
		return cells
	}
	return append(slices.Clip(cells[:at+run-over]), cells[at+run:]...)
}

// isRule reports a character a harness draws a horizontal rule with.
func isRule(s string) bool {
	switch s {
	case "─", "━", "═", "╌", "┄":
		return true
	}
	return false
}

// escapeAt is the escape sequence a text starts with, and its length: a CSI
// with its parameters, an OSC up to its terminator, or a two-character
// escape. An escape cut short by the end of the text is the rest of it.
func escapeAt(s string) (string, int) {
	if loc := escapeSeq.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return s[:loc[1]], loc[1]
	}
	return s[:1], 1
}

// sgrParams is the parameters of an SGR sequence, and whether the escape is
// one: a CSI that ends in m and holds digits and their separators alone. A
// CSI that ends in m with a private marker - CSI > 4 ; 2 m sets how the
// terminal reports keys - is no SGR, and is dropped with the rest.
func sgrParams(seq string) ([]string, bool) {
	body, ok := strings.CutPrefix(seq, "\x1b[")
	if !ok || !strings.HasSuffix(body, "m") {
		return nil, false
	}
	body = strings.TrimSuffix(body, "m")
	for _, r := range body {
		if (r < '0' || r > '9') && r != ';' && r != ':' {
			return nil, false
		}
	}
	return strings.Split(body, ";"), true
}

// sgrState is the style a run of the screen is drawn in: what an SGR
// sequence set and none since took back. It is what a line cut in the middle
// of a run starts its second half with.
type sgrState struct {
	attrs  map[string]string // by the attribute's own code: "1" bold, "4" underline
	fg, bg string
	ul     string // the underline's colour
}

// sgrOff is the attributes each resetting code takes back.
var sgrOff = map[string][]string{
	"22": {"1", "2"}, "23": {"3"}, "24": {"4"}, "25": {"5"}, "27": {"7"}, "28": {"8"}, "29": {"9"},
}

// apply takes one SGR sequence's parameters into the state. A colour set with
// semicolons spans several parameters (38;5;n and 38;2;r;g;b); one set with
// colons is a single parameter, and so is a styled underline (4:3).
func (s *sgrState) apply(params []string) {
	for i := 0; i < len(params); i++ {
		p := params[i]
		code, _, _ := strings.Cut(p, ":")
		switch code {
		case "", "0":
			*s = sgrState{}
		case "1", "2", "3", "4", "5", "7", "8", "9":
			if s.attrs == nil {
				s.attrs = map[string]string{}
			}
			s.attrs[code] = p
		case "22", "23", "24", "25", "27", "28", "29":
			for _, attr := range sgrOff[code] {
				delete(s.attrs, attr)
			}
		case "38", "48", "58":
			if code == p {
				n := 0
				if i+1 < len(params) {
					switch params[i+1] {
					case "5":
						n = 2
					case "2":
						n = 4
					}
				}
				end := min(i+1+n, len(params))
				p = strings.Join(params[i:end], ";")
				i = end - 1
			}
			switch code {
			case "38":
				s.fg = p
			case "48":
				s.bg = p
			default:
				s.ul = p
			}
		case "39":
			s.fg = ""
		case "49":
			s.bg = ""
		case "59":
			s.ul = ""
		default:
			switch n, _ := strconv.Atoi(code); {
			case n >= 30 && n <= 37, n >= 90 && n <= 97:
				s.fg = p
			case n >= 40 && n <= 47, n >= 100 && n <= 107:
				s.bg = p
			}
		}
	}
}

// seq is the one SGR sequence that sets the state, empty for the default
// style.
func (s sgrState) seq() string {
	var parts []string
	for _, code := range []string{"1", "2", "3", "4", "5", "7", "8", "9"} {
		if p, ok := s.attrs[code]; ok {
			parts = append(parts, p)
		}
	}
	for _, p := range []string{s.fg, s.bg, s.ul} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}
