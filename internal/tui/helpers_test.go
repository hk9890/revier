// The world, the keys and the screen readers the tests of this package share.
package tui_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// world is a runtime, a window host, and n projects; the last project's
// workspace is running with an agent that wants the human.
func world(t *testing.T, n int) (*hosttest.FakeRuntime, *hosttest.Fake, *core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention, Activity: "needs a decision"},
	}}}
	var raw []revier.Project
	for i := 0; i < n; i++ {
		name := revier.ProjectName(fmt.Sprintf("project-%02d", i))
		raw = append(raw, revier.Project{Name: name, Path: "/p/" + string(name), Targets: []revier.Target{
			{Name: "home", Home: true, Key: "ctrl-shift-u", Runtime: &revier.Realization{
				Name: "session:" + string(name), Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + string(name) + "$"}}},
			{Name: "editor", Key: "ctrl-shift-o", Window: &revier.Realization{
				Launch: []string{"code"}, Match: revier.Match{Class: "^code-" + string(name) + "$"}}},
		}})
	}
	last := raw[n-1].Name
	rt.Add("session:"+string(last), "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	projects := core.Prepare(raw)
	return rt, wm, c, projects
}

// stateWith writes a state file holding the given attachments and returns
// its root.
func stateWith(t *testing.T, attached map[revier.ProjectName][]revier.TargetRef) string {
	t.Helper()
	root := t.TempDir()
	st := &state.State{Attached: attached}
	if err := st.Save(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// refreshed builds the model over a state root and applies one survey, as the
// timer does.
func refreshed(t *testing.T, c *core.Core, projects []core.Project, root string, actions []config.Action) tui.Model {
	t.Helper()
	m := tui.New(c, projects, root, &config.Config{Actions: actions}, time.Second, theme.Default(), "").StaticCursors()
	return survey(m)
}

// clocked gives the model a clock a test advances by hand, and returns the
// hand: two clicks are as far apart as the test says, not as the machine ran.
func clocked(m tui.Model) (tui.Model, *time.Time) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return m.WithClock(func() time.Time { return now }), &now
}

// survey applies one whole refresh: the survey of this machine, then what
// the linked hosts say.
func survey(m tui.Model) tui.Model {
	next, _ := m.Update(m.Survey()())
	next, _ = next.Update(next.(tui.Model).AskRemotes()())
	return next.(tui.Model)
}

func press(m tui.Model, key string) (tui.Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "home":
		msg = tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		msg = tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	case "delete":
		msg = tea.KeyMsg{Type: tea.KeyDelete}
	case "alt+delete":
		msg = tea.KeyMsg{Type: tea.KeyDelete, Alt: true}
	default:
		letter, alt := strings.CutPrefix(key, "alt+")
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(letter), Alt: alt}
	}
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// lines is the surface's content, with the margin taken off: its blank rows
// dropped and each line right-trimmed. Tests assert on what the surface says,
// not on where it sits in the terminal.
//
// The left margin stays on the line. Every line of the surface carries a
// gutter space of its own, so a test that cared where a line starts would
// have to count either way, and margins reads the margin off the render.
func lines(m tui.Model) []string {
	out := strings.Split(m.View(), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// The chrome lines, in the order View writes them.
func barLine(m tui.Model) string { return lines(m)[0] }

func query(m tui.Model) string { return lines(m)[2] }

func ruleLine(m tui.Model) string { return lines(m)[3] }

// footer is the last content line: the key legend, or the last failure.
func footer(m tui.Model) string {
	l := lines(m)
	return l[len(l)-1]
}

// rows are the project rows, without the header, the query line and the rule.
func rows(m tui.Model) []string {
	l := lines(m)
	if len(l) < chromeLines {
		return nil
	}
	return l[chromeLines:]
}

// The action bar, the line under it, the query line and the rule sit above
// the list.
const chromeLines = 4

// column is the screen column text sits in, which is not its byte offset: a
// rule is drawn out of three-byte dashes.
func column(line, text string) int {
	at := strings.Index(line, text)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(line[:at])
}

// paneCursor is the pane row carrying the cursor bar, or nothing when the
// cursor is on the list.
func paneCursor(m tui.Model) string {
	for _, line := range strings.Split(pane(m), "\n") {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return line
		}
	}
	return ""
}

// paneCell is the terminal cell where the text starts on the pane line that
// carries it. The column is the rendered width of what stands before the
// text, not its byte offset: a glyph is three bytes for one cell, and a
// colour profile puts escape sequences in the line.
func paneCell(t *testing.T, m tui.Model, text string) (x, y int) {
	t.Helper()
	for y, raw := range strings.Split(m.View(), "\n") {
		// The list, the pane's border, the pane.
		if parts := strings.Split(raw, "│"); len(parts) > 1 {
			if at := strings.Index(parts[1], text); at >= 0 {
				return paneBorder(t, m) + 1 + lipgloss.Width(parts[1][:at]), y
			}
		}
	}
	t.Fatalf("no pane line carries %q:\n%s", text, m.View())
	return 0, 0
}

// selectedRow is the row the cursor is on, found by the cursor glyph the
// delegate renders into it.
func selectedRow(t *testing.T, m tui.Model) string {
	t.Helper()
	for _, line := range lines(m) {
		if strings.Contains(line, theme.Default().Glyphs.Cursor) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("no row is selected:\n%s", m.View())
	return ""
}

// resize is the size message a terminal sends. The default model is 80
// columns, which is too narrow to split, so a test that wants the detail pane
// has to ask for the room.
func resize(m tui.Model, w, h int) tui.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(tui.Model)
}

// pane is the detail pane: whatever is right of the border column on each
// line. The two panes are joined horizontally, so this is how a test reads
// one without the other.
func pane(m tui.Model) string {
	var out []string
	for _, line := range lines(m) {
		if _, right, ok := strings.Cut(line, "│"); ok {
			out = append(out, strings.TrimSpace(right))
		}
	}
	return strings.Join(out, "\n")
}

// send is one key as bubbletea's input reader delivers it.
func send(m tui.Model, msg tea.KeyMsg) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

// longActivity is an agent's activity line longer than any pane.
const longActivity = "Reading internal/tui/detail.go and working out why the activity line ends in an ellipsis where fzf wraps it"

// longWorld is one running project at path whose agent reports longActivity,
// and a second project after it.
func longWorld(t *testing.T, path string) (*core.Core, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: longActivity},
	}}}
	home := func(name string) revier.Target {
		return revier.Target{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}}
	}
	projects := core.Prepare([]revier.Project{
		{Name: "long", Path: path, Targets: []revier.Target{home("long")}},
		{Name: "short", Path: "/p/short", Targets: []revier.Target{home("short")}},
	})
	rt.Add("session:long", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	return c, projects
}

// wheel is one notch of the mouse wheel at a column.
func wheel(m tui.Model, x int, b tea.MouseButton) tui.Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: 5, Button: b, Action: tea.MouseActionPress})
	return next.(tui.Model)
}

// paneBorder is the terminal column of the border between the list and the
// pane, read off the rendered surface: the rendered width of what stands
// before it, so a colour profile's escape sequences do not count.
func paneBorder(t *testing.T, m tui.Model) int {
	t.Helper()
	for _, raw := range strings.Split(m.View(), "\n") {
		if strings.Count(raw, "│") == 1 {
			return lipgloss.Width(raw[:strings.Index(raw, "│")])
		}
	}
	t.Fatalf("no line with a pane:\n%s", m.View())
	return 0
}

// each runs a command and hands what it answers to do. A batch answers one
// message per command, in order, as the program delivers them; a nil command
// answers nothing.
func each(cmd tea.Cmd, do func(tea.Msg)) {
	if cmd == nil {
		return
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		do(msg)
		return
	}
	for _, c := range batch {
		each(c, do)
	}
}

// deliver runs a command and feeds what it answers back, and returns the
// commands the model answers with: a close that needs no confirm plans, then
// runs. It is the one way a test delivers a command; run, step and runAll are
// deliver for a caller that wants less back.
func deliver(m tui.Model, cmd tea.Cmd) (tui.Model, tea.Cmd) {
	var outs []tea.Cmd
	each(cmd, func(msg tea.Msg) {
		next, out := m.Update(msg)
		m = next.(tui.Model)
		outs = append(outs, out)
	})
	return m, tea.Batch(outs...)
}

// run is deliver without the commands the model answers with.
func run(m tui.Model, cmd tea.Cmd) tui.Model {
	m, _ = deliver(m, cmd)
	return m
}

// step presses a key and delivers the command it returns.
func step(m tui.Model, key string) tui.Model {
	return run(press(m, key))
}

// runAll runs a command for what it does to the world; its messages are
// dropped.
func runAll(cmd tea.Cmd) {
	each(cmd, func(tea.Msg) {})
}

// clickCell is one press and release of the left button on a terminal cell,
// with the command either returned.
func clickCell(m tui.Model, x, y int) (tui.Model, tea.Cmd) {
	next, pressed := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	next, released := next.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	return next.(tui.Model), tea.Batch(pressed, released)
}
