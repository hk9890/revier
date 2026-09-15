package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The action bar is the top line: what the surface can do that is not about
// the row under the cursor. Adding a project, linking one on another machine
// and opening the configuration are all about the installation, so none of
// them has a row to hang off, and none of them can take a bare letter -
// every printable rune is a filter character.
//
// A button carries its key. The bar is a second way to reach the same thing,
// never the only way: an action with no key would be unreachable from the
// keyboard, which is where this surface is driven from (decisions.md D49).

// barAction is one button: what it says, the key that does the same, and
// what it runs.
type barAction struct {
	label string
	key   string
	run   func(Model) (tea.Model, tea.Cmd)
}

var barActions = []barAction{
	{label: "new", key: "alt+n", run: Model.openNew},
	{label: "remote", key: "alt+r", run: Model.openHosts},
	{label: "sessions", key: sessionsBarKey, run: Model.openSessions},
	{label: "config", key: "alt+c", run: Model.openConfig},
	{label: "help", key: helpBarKey, run: Model.openHelp},
}

// barCell is a button's place on the bar: the text as it is drawn, and the
// columns it covers, counted from the frame's content. Render and the hit
// test both read it, so a button is never drawn somewhere the pointer does
// not find it.
type barCell struct {
	action barAction
	text   string
	x0, x1 int
}

// barLine is the bar's line: the surface's first. What can be
// done to the installation stands above what is on it, and the query line
// then sits directly over the rows it filters.
const barLine = 0

// barSeparator stands between two buttons. It is the header's own separator
// between its counts: the eye already reads that mark on this surface as
// "these are separate things", where a wider gap alone did not - the gap
// inside a button is a space too.
const barSeparator = " · "

// buttons are the bar's buttons on the screen in view: the surface's, or the
// sessions screen's own. Every other screen has none.
func (m Model) buttons() []barAction {
	switch {
	case m.confirm != "":
		return nil
	case m.dialog == dialogNone:
		return barActions
	case m.dialog == dialogSessions:
		return sessionActions
	}
	return nil
}

// barLead is what stands on the bar before the buttons: the name of the
// screen, where a screen has buttons of its own.
func (m Model) barLead() string {
	if m.dialog == dialogSessions {
		return " " + m.theme.Header.Render("Sessions") + "  "
	}
	return ""
}

func (m Model) barCells() []barCell {
	buttons := m.buttons()
	out := make([]barCell, 0, len(buttons))
	// A button's own leading space is the line's gutter, so the first label
	// starts in the column every other line starts in.
	x := lipgloss.Width(m.barLead())
	for _, a := range buttons {
		text := " " + a.label + " " + a.key + " "
		w := lipgloss.Width(text)
		out = append(out, barCell{action: a, text: text, x0: x, x1: x + w})
		x += w + lipgloss.Width(barSeparator)
	}
	return out
}

// bar is the buttons as they are drawn. The button under the pointer takes
// the hover background, a step below the selected row's. A delete question
// blanks the line rather than removing it: the rows below must not jump by
// one while an answer is waited for.
func (m Model) bar() string {
	th := m.theme
	var b strings.Builder
	b.WriteString(m.barLead())
	for i, c := range m.barCells() {
		if i > 0 {
			b.WriteString(th.Path.Render(barSeparator))
		}
		label, key := th.ProjectName, th.Help
		if m.over.is(hoverBar, i) {
			label, key = th.OnHover(label).Bold(true), th.OnHover(key)
		}
		b.WriteString(label.Render(" "+c.action.label+" ") + key.Render(c.action.key+" "))
	}
	return b.String()
}

// barAt is the button under a terminal cell, or -1 where there is none.
func (m Model) barAt(x, y int) int {
	if len(m.buttons()) == 0 {
		return -1
	}
	mr, mc := m.margins()
	if y != mr+barLine {
		return -1
	}
	cx := x - mc
	for i, c := range m.barCells() {
		if cx >= c.x0 && cx < c.x1 {
			return i
		}
	}
	return -1
}

// barKey runs the button a press names, if a button declares it.
func (m Model) barKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	for _, a := range barActions {
		if msg.String() == a.key {
			next, cmd := a.run(m)
			return next, cmd, true
		}
	}
	return m, nil, false
}
