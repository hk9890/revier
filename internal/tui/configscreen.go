package tui

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The config screen, alt+c: one row per key of config.toml it can change.
// A change is written the moment it is made, with the file's comments kept
// (config.Set), and applied the moment it is written: the theme and the
// glyphs repaint the surface, and a runtime host is swapped in for the next
// survey. The window host is shown and not offered: which one a machine has
// is its desktop's, not a choice.

// configRow is a row of the screen the cursor can be on.
type configRow int

const (
	rowTheme configRow = iota
	rowGlyphs
	rowTrigger
	rowRuntime
	configRows
)

// autoRuntime is the runtime choice that writes no host: startup searches the
// compiled-in hosts in their default order.
const autoRuntime = "auto"

// RuntimeSelector picks the runtime host for a configured preference list,
// probing as startup does. An empty list is the default search.
type RuntimeSelector func(ctx context.Context, want []string) (revier.Runtime, error)

// runtimeTimeout bounds one probe of a runtime host.
const runtimeTimeout = 10 * time.Second

// runtimeMsg is a runtime choice probed: the host it selected, or why none.
type runtimeMsg struct {
	want    []string
	runtime revier.Runtime
	err     error
}

// WithRuntimes gives the config screen the runtime hosts it offers and the
// selector that probes them. The adapters are wired in cmd/revier alone, so
// the surface is handed both rather than naming a host itself.
func (m Model) WithRuntimes(choices []string, pick RuntimeSelector) Model {
	m.runtimes, m.pick = choices, pick
	return m
}

func newChordInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	styleField(&in, th)
	in.Prompt = ""
	in.Placeholder = config.DefaultTriggerKey
	in.CharLimit = 64
	return in
}

// openConfig is the "config" button and alt+c.
func (m Model) openConfig() (tea.Model, tea.Cmd) {
	m.err = nil
	m.leavePane()
	m.crow = int(rowTheme)
	m.refused = ""
	m.aform = actionForm{}
	m.tform = targetForm{}
	m.dropping = false
	m.body.SetYOffset(0)
	m.dialog = dialogConfig
	return m, nil
}

// configKey is every press on the config screen. Left and right change the
// row's value; Enter does the same, or opens the trigger key for typing.
func (m Model) configKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.chord.Focused():
		return m.chordKey(msg)
	case m.aform.open:
		return m.actionFormKey(msg)
	case m.tform.open:
		return m.targetFormKey(msg)
	case m.dropping:
		if _, onTarget := m.targetRow(); onTarget {
			return m.confirmDropTarget(msg)
		}
		return m.confirmDropAction(msg)
	}
	m.err = nil
	_, onAction := m.actionRow()
	_, onTarget := m.targetRow()
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.dialog = dialogNone
		// The screen took the pane's width; the list gets its own back.
		m.layout()
	case key.Matches(msg, m.keys.Up):
		m.crow = max(m.crow-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.crow = min(m.crow+1, m.addRow())
	case (onAction || onTarget) && key.Matches(msg, m.keys.Delete):
		m.dropping = true
	case onTarget && key.Matches(msg, m.keys.Enter), m.crow == m.addTargetRow() && key.Matches(msg, m.keys.Enter):
		return m.openTargetForm(m.crow - int(configRows))
	case onAction && key.Matches(msg, m.keys.Enter), m.crow == m.addRow() && key.Matches(msg, m.keys.Enter):
		return m.openActionForm(m.crow - m.actionBase())
	case configRow(m.crow) == rowTrigger && key.Matches(msg, m.keys.Enter):
		m.chord.SetValue(m.ui.TriggerKey)
		m.chord.CursorEnd()
		return m, m.chord.Focus()
	case msg.Type == tea.KeyLeft:
		return m.change(-1)
	case msg.Type == tea.KeyRight, key.Matches(msg, m.keys.Enter):
		return m.change(+1)
	}
	return m, nil
}

// chordKey is a press while the trigger key is typed. Enter writes it, Esc
// leaves it as it was.
func (m Model) chordKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.chord.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		raw := strings.TrimSpace(m.chord.Value())
		if _, err := core.ParseChord(raw); err != nil {
			m.err = err
			return m, nil
		}
		if err := writeConfig("ui", "trigger_key", raw); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.ui.TriggerKey = raw
		m.chord.Blur()
		return m, nil
	case altRune(msg):
		return m, nil
	}
	in, cmd := m.chord.Update(msg)
	m.chord = in
	return m, cmd
}

// change steps the value of the row under the cursor.
func (m Model) change(step int) (tea.Model, tea.Cmd) {
	ui := m.ui
	switch configRow(m.crow) {
	case rowTheme:
		ui.Theme = cycle(theme.Themes(), orDefault(ui.Theme, theme.DefaultTheme), step)
		return m.setUI(ui, "theme", ui.Theme), nil
	case rowGlyphs:
		ui.Glyphs = cycle(theme.GlyphSets(), orDefault(ui.Glyphs, theme.DefaultGlyphs), step)
		return m.setUI(ui, "glyphs", ui.Glyphs), nil
	case rowRuntime:
		return m.switchRuntime(step)
	}
	return m, nil
}

// setUI writes one key of [ui] and repaints the surface in the result.
func (m Model) setUI(ui config.UI, key, value string) Model {
	th, err := theme.Lookup(ui.Theme, ui.Glyphs)
	if err != nil {
		m.err = err
		return m
	}
	if err := writeConfig("ui", key, value); err != nil {
		m.err = err
		return m
	}
	m.ui = ui
	m.applyTheme(th)
	return m
}

// switchRuntime probes the next runtime choice, off the update loop: a probe
// runs the host's own tool. The choice is written only once a host answers,
// because a configured host that probes badly is a startup error, and the
// next start must not be refused over a choice made here.
func (m Model) switchRuntime(step int) (tea.Model, tea.Cmd) {
	if m.pick == nil {
		m.err = errors.New("this surface has no runtime hosts to choose from")
		return m, nil
	}
	if m.switching != "" {
		return m, nil
	}
	// A choice that did not probe is stepped from, not the configured one:
	// stepping from the configured one would offer the refused choice again,
	// and a direction with a broken host in it would go nowhere.
	from := m.runtimeChoice()
	if m.refused != "" {
		from = m.refused
	}
	next := cycle(append([]string{autoRuntime}, m.runtimes...), from, step)
	want := []string{}
	if next != autoRuntime {
		want = []string{next}
	}
	m.switching = next
	pick := m.pick
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeTimeout)
		defer cancel()
		rt, err := pick(ctx, want)
		return runtimeMsg{want: want, runtime: rt, err: err}
	}
}

// runtimeSwitched takes a probed choice: written, and the core swapped for
// one over the host it selected. The next survey reads the new host.
func (m Model) runtimeSwitched(msg runtimeMsg) (tea.Model, tea.Cmd) {
	tried := m.switching
	m.switching = ""
	if msg.err != nil {
		m.err = msg.err
		m.refused = tried
		return m, nil
	}
	m.refused = ""
	if err := writeConfig("hosts", "runtime", msg.want); err != nil {
		m.err = err
		return m, nil
	}
	m.runtime = msg.want
	m.core = m.core.WithRuntime(msg.runtime)
	slog.Info("runtime switched", "want", msg.want, "runtime", hostName(msg.runtime))
	return m, nil
}

// runtimeChoice is the configured runtime preference as the screen names it:
// auto for none, the host for one it offers, and the list as written for
// anything else.
func (m Model) runtimeChoice() string {
	switch {
	case len(m.runtime) == 0:
		return autoRuntime
	case len(m.runtime) == 1 && slices.Contains(m.runtimes, m.runtime[0]):
		return m.runtime[0]
	}
	return strings.Join(m.runtime, ", ")
}

// applyTheme repaints every part of the surface that was built with a theme.
// The lists keep their items and cursors; only their styles change.
func (m *Model) applyTheme(th theme.Theme) {
	m.theme = th
	m.hlist.SetDelegate(hostDelegate{theme: th})
	m.rlist.SetDelegate(projectDelegate{theme: th, hover: -1})
	m.help = newHelp(th)
	m.detail.Style = newDetail(th).Style
	for _, in := range []*textinput.Model{&m.input, &m.path, &m.lname, &m.chord} {
		styleField(in, th)
	}
	m.layout()
}

func writeConfig(table, key string, value any) error {
	return withConfigRoot(func(root string) error { return config.Set(root, table, key, value) })
}

func withConfigRoot(write func(root string) error) error {
	root, err := config.Root()
	if err != nil {
		return err
	}
	return write(root)
}

// cycle is the value step places after current, wrapping at both ends. A
// current value not among them steps onto the first, or the last.
func cycle(values []string, current string, step int) string {
	i := slices.Index(values, current)
	switch {
	case i >= 0:
		return values[(i+step+len(values))%len(values)]
	case step < 0:
		return values[len(values)-1]
	}
	return values[0]
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// configLabelWidth is the column the values start in.
const configLabelWidth = 14

// configScreen stands in the list's place while the screen is up, and the
// line the cursor is on, which the body keeps in view.
func (m Model) configScreen() (string, int) {
	th := m.theme
	w := m.listWidth()
	var b strings.Builder
	at := 0
	row := func(r int, label, value, note string) {
		// While a form is open its own cursor is the one on screen.
		sel := m.crow == r && !m.tform.open
		if sel {
			at = strings.Count(b.String(), "\n")
		}
		style := func(s lipgloss.Style) lipgloss.Style {
			if sel {
				return th.OnSelection(s)
			}
			return s
		}
		line := cursor(th, sel) + style(th.Meta).Render(pad(label, configLabelWidth)) + style(th.ProjectName).Render(value)
		if note != "" {
			line += style(th.Path).Render("  " + note)
		}
		b.WriteString(fill(clipTo(line, w), w, style) + "\n")
	}
	info := func(label, value, note string) {
		line := th.Path.Render("  ") + th.Meta.Render(pad(label, configLabelWidth)) + th.NameDim.Render(value) + th.Path.Render("  "+note)
		b.WriteString(clipTo(line, w) + "\n")
	}

	b.WriteString(m.heading("Appearance", w))
	row(int(rowTheme), "theme", "‹ "+orDefault(m.ui.Theme, theme.DefaultTheme)+" ›", "")
	row(int(rowGlyphs), "glyphs", "‹ "+orDefault(m.ui.Glyphs, theme.DefaultGlyphs)+" ›", "")
	trigger := orDefault(m.ui.TriggerKey, config.DefaultTriggerKey)
	if m.chord.Focused() {
		trigger = m.chord.View()
	}
	row(int(rowTrigger), "trigger key", trigger, "the desktop key that opens revier")

	b.WriteString(m.heading("Hosts", w))
	note := "in use: " + hostName(m.core.Runtime)
	if m.switching != "" {
		note = "checking " + m.switching + "…"
	}
	row(int(rowRuntime), "runtime", "‹ "+m.runtimeChoice()+" ›", note)
	info("window", hostName(m.core.Window), "detected at start")

	b.WriteString(m.heading("Targets", w))
	tform := func() {
		lines, line := m.targetFormLines(w)
		at = strings.Count(b.String(), "\n") + line
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, t := range m.targets {
		row(int(configRows)+i, keyLabel(t.Key), string(t.Name), describeTarget(t))
		if m.tform.open && m.tform.index == i {
			tform()
		}
	}
	row(m.addTargetRow(), "+", "add a target", "every project has it")
	if m.tform.open && m.tform.index == len(m.targets) {
		tform()
	}

	b.WriteString(m.heading("Actions", w))
	form := func() {
		lines, field := m.actionFormLines(w)
		at = strings.Count(b.String(), "\n") + field
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, act := range m.actions {
		if m.aform.open && m.aform.index == i {
			form()
			continue
		}
		row(m.actionBase()+i, keyLabel(act.Key), act.Name, joinCommand(act.Run))
	}
	if m.aform.open && m.aform.index == len(m.actions) {
		form()
	} else {
		row(m.addRow(), "+", "add an action", "run in the selected project, bound to a key")
	}
	return b.String(), at
}

// hostName is a host's name, or "none" for no host.
func hostName(h revier.Host) string {
	if h == nil {
		return "none"
	}
	return h.Name()
}
