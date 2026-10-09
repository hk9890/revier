package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The mirror is the right side of the agent list: what the panel of the agent
// under the cursor shows, read from its terminal once a second, so the user
// watches the agent work without going to it (decisions.md D111).
//
// It is the terminal's own drawing, laid out for the agent's window, and that
// window is wider than the pane: a line that does not fit continues on the
// next. The colours are kept, and nothing else the terminal was told: only an
// SGR sequence is passed on, and every line is closed, so what an agent's
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
	seq      int       // the number of the last read asked for
	took     int       // the number of the last read taken
	ticking  bool      // whether a tick is out, so a switch starts no second one

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

type mirrorTickMsg struct{}

func mirrorTick() tea.Cmd {
	return tea.Tick(mirrorInterval, func(time.Time) tea.Msg { return mirrorTickMsg{} })
}

// mirroring reports the mirror on the screen: the agent list in view, with a
// pane beside it and nothing standing over it.
func (m Model) mirroring() bool {
	return m.agents.shown && !m.hidden && m.dialog == dialogNone && m.paneCols() > 0
}

// askMirror sends for the screen of the agent under the cursor, while the
// mirror is on the screen: at once when
// another agent comes under it, and when the mirror comes back into view with
// a screen read before it left, and on every tick after. It starts the tick
// when the mirror comes into view, and the tick ends itself when it leaves.
func (al *agentList) askMirror(c *core.Core, msg tea.Msg) tea.Cmd {
	it, ok := al.selected()
	if !ok {
		al.mirror = mirror{ticking: al.mirror.ticking, seq: al.mirror.seq}
		return nil
	}
	var cmds []tea.Cmd
	back := !al.mirror.ticking
	if back {
		al.mirror.ticking = true
		cmds = append(cmds, mirrorTick())
	}
	_, tick := msg.(mirrorTickMsg)
	if key := it.key(); key != al.mirror.key {
		al.mirror = mirror{key: key, ticking: true, seq: al.mirror.seq}
	} else if !tick && !back {
		return tea.Batch(cmds...)
	}
	return tea.Batch(append(cmds, al.readScreen(c, it.agent))...)
}

// readScreen is one read of the agent's panel, off the update loop.
func (al *agentList) readScreen(c *core.Core, a revier.AgentView) tea.Cmd {
	al.mirror.seq++
	key, seq, deep := al.mirror.key, al.mirror.seq, al.mirror.deep
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), detailWait)
		defer cancel()
		text, err := c.Screen(ctx, a, deep)
		return mirroredMsg{key: key, seq: seq, deep: deep, text: text, err: err}
	}
}

// tookScreen takes in a read. One for an agent the cursor has since left is
// dropped, and so is one older than the read already taken. A read that
// failed keeps the screen the mirror was showing: one kitty call that timed
// out must not blank it. While the user is scrolled up, the view stays on the
// lines it shows as more are written under them.
//
// w is the width the pane's text has.
func (al *agentList) tookScreen(msg mirroredMsg, w int) {
	if msg.key != al.mirror.key || msg.seq <= al.mirror.took {
		return
	}
	if !errors.Is(msg.err, core.ErrNoScreen) {
		// One line per panel that fails: a key for them all would log again
		// each time the cursor went from a panel that reads to one that does
		// not.
		at := msg.key.agent
		logging.Repeat("mirror\x00"+at.host+"\x00"+at.id+"\x00"+string(at.panel), "mirror", msg.err, "panel", at.panel)
	}
	before := len(al.screenLines(w))
	al.mirror.took, al.mirror.read, al.mirror.err = msg.seq, true, msg.err
	if msg.err != nil {
		return
	}
	// The first read with the scrollback adds lines above the view, which a
	// view counted from the end does not move for; a later one adds them
	// under it.
	grew := msg.deep && al.mirror.textDeep
	if msg.text != al.mirror.text {
		al.mirror.text, al.mirror.lines = msg.text, nil
	}
	al.mirror.textDeep = msg.deep
	if al.mirror.off > 0 && grew {
		al.mirror.off += max(len(al.screenLines(w))-before, 0)
	}
	al.boundScroll(w)
}

// scrollMirror moves the mirror's view by lines, up for a positive count.
// The first move up asks for the scrollback, which the mirror does not read
// while it follows the end; back at the end it stops reading it. Until the
// scrollback is here there is nothing above the screen to stop at, so the
// moves are kept as they come.
func (al *agentList) scrollMirror(c *core.Core, by, w int) tea.Cmd {
	it, ok := al.selected()
	if !ok {
		return nil
	}
	if by > 0 && !al.mirror.deep {
		al.mirror.deep, al.mirror.off = true, by
		return al.readScreen(c, it.agent)
	}
	al.mirror.off = max(al.mirror.off+by, 0)
	al.boundScroll(w)
	return nil
}

// boundScroll keeps the view within the scrollback once it is here, and has
// the mirror follow the end again when the view is at it: a scrollback with
// nothing above the pane leaves no line to hold, and a view that held one
// would stand still while the panel wrote on under it.
func (al *agentList) boundScroll(w int) {
	if al.mirror.textDeep {
		al.mirror.off = min(al.mirror.off, max(len(al.screenLines(w))-al.mirror.room, 0))
	}
	if al.mirror.off == 0 {
		al.mirror.deep = false
	}
}

// screenLines is the mirror's text as the pane's lines, at the pane's width,
// kept while the text and the width stay.
func (al *agentList) screenLines(w int) []string {
	if al.mirror.lines == nil || al.mirror.linesW != w {
		al.mirror.lines, al.mirror.linesW = screenLines(al.mirror.text, w), w
	}
	return al.mirror.lines
}

// mirrorView is the mirror in rows lines, w wide: the end of the screen, or
// the part the user scrolled to.
func (al *agentList) mirrorView(th theme.Theme, w, rows int) string {
	al.mirror.room = rows
	if rows < 1 {
		return ""
	}
	note := func(s string) string {
		parts := wrap(s, w)
		for i := range parts {
			parts[i] = th.Meta.Render(parts[i])
		}
		return strings.Join(parts, "\n")
	}
	lines := al.screenLines(w)
	switch {
	case errors.Is(al.mirror.err, core.ErrNoScreen):
		return note("The screen of this agent cannot be read here.")
	case !al.mirror.read:
		return note("reading...")
	case al.mirror.err != nil && len(lines) == 0:
		return note("The screen of this agent could not be read.")
	}
	end := len(lines) - min(al.mirror.off, max(len(lines)-rows, 0))
	return strings.Join(lines[max(end-rows, 0):end], "\n")
}

// sgrReset closes a line that carries a style, so the style ends with it.
const sgrReset = "\x1b[m"

// screenLines sets a panel's screen as lines no wider than w. A line too long
// continues on the next, in the style it was cut in. Of what the terminal was
// told only the SGR sequences are kept - colour, and bold and its kin - and
// every other escape and control character is dropped, so a line is as wide
// measured as it is drawn and can do nothing but be read (decisions.md D111).
// The blank lines a screen ends in are dropped, so the mirror ends on the
// last thing the agent's terminal shows.
func screenLines(text string, w int) []string {
	if w < 1 || text == "" {
		return nil
	}
	var out []string
	var line strings.Builder
	var style sgrState
	width, styled := 0, false
	start := func() {
		line.Reset()
		width, styled = 0, false
		if seq := style.seq(); seq != "" {
			line.WriteString(seq)
			styled = true
		}
	}
	flush := func() {
		if styled {
			line.WriteString(sgrReset)
		}
		out = append(out, line.String())
		start()
	}
	put := func(s string, cells int) {
		if width+cells > w {
			flush()
		}
		line.WriteString(s)
		width += cells
	}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == '\x1b':
			seq, n := escapeAt(text[i:])
			i += n
			if params, ok := sgrParams(seq); ok {
				style.apply(params)
				line.WriteString(seq)
				styled = true
			}
			continue
		case r == '\n':
			flush()
		case r == '\t':
			for range tabWidth {
				put(" ", 1)
			}
		case r == utf8.RuneError && size == 1:
			put(string(r), 1)
		case unicode.IsControl(r):
		case r < utf8.RuneSelf && (i+1 == len(text) || text[i+1] < utf8.RuneSelf):
			put(text[i:i+1], 1)
		default:
			// A character is measured with what joins it, as the terminal
			// draws it: an emoji and its variation selector are one, two
			// cells wide, where the two measured apart come to one.
			cluster, cells := ansi.FirstGraphemeCluster(text[i:], ansi.GraphemeWidth)
			put(cluster, cells)
			size = len(cluster)
		}
		i += size
	}
	if width > 0 {
		flush()
	}
	for len(out) > 0 && ansi.StringWidth(strings.TrimSpace(ansi.Strip(out[len(out)-1]))) == 0 {
		out = out[:len(out)-1]
	}
	return out
}

// escapeAt is the escape sequence a text starts with, and its length: a CSI
// with its parameters, an OSC up to its terminator, or a two-character
// escape. An escape cut short by the end of the text is the rest of it.
func escapeAt(s string) (string, int) {
	if loc := mdEscape.FindStringIndex(s); loc != nil && loc[0] == 0 {
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
