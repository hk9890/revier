package tui

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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

// runtimeMsg is a runtime choice probed: the host it selected, or why none.
type runtimeMsg struct {
	want    []string
	runtime revier.Runtime
	err     error
}

// configScreen is the config screen: where its cursor is, the forms it has
// open, and the keys of config.toml it changes itself.
type configScreen struct {
	ui        config.UI       // [ui] as config.toml holds it
	runtime   []string        // [hosts] runtime as config.toml holds it
	runtimes  []string        // the runtime hosts the screen offers besides auto
	pick      RuntimeSelector // probes a runtime choice, as startup does
	switching string          // the runtime choice being probed
	refused   string          // the last runtime choice that did not probe, stepped from next
	row       int             // the row the cursor is on
	chord     textinput.Model // the trigger key, while it is typed
	aform     actionForm      // the action being added or changed, while its form is up
	tform     targetForm      // the shared target being added or changed, while its form is up
	dropping  bool            // whether the target or action under the cursor waits on a y to be deleted
}

func newConfigScreen(th theme.Theme, cfg *config.Config) configScreen {
	return configScreen{ui: cfg.UI, runtime: cfg.Hosts.Runtime, chord: newChordInput(th)}
}

// configResult is what a press on the screen leaves for the surface. The
// screen has written config.toml; the surface takes what the file now holds.
type configResult struct {
	err     error                  // the footer's
	closed  bool                   // Esc on the rows: back to the surface
	theme   *theme.Theme           // [ui] changed: the theme to repaint in
	actions *[]config.Action       // the actions changed: the ones to bind
	targets *config.TargetsWritten // the shared targets changed: they, and the projects loaded with them
}

// WithRuntimes gives the config screen the runtime hosts it offers and the
// selector that probes them. The adapters are wired in cmd/revier alone, so
// the surface is handed both rather than naming a host itself.
func (m Model) WithRuntimes(choices []string, pick RuntimeSelector) Model {
	m.config.runtimes, m.config.pick = choices, pick
	return m
}

func newChordInput(th theme.Theme) textinput.Model {
	in := newFieldInput(th)
	in.Placeholder = config.DefaultTriggerKey
	in.CharLimit = 64
	return in
}

// openConfig is the "config" button and alt+c.
func (m Model) openConfig() (tea.Model, tea.Cmd) {
	m.err = nil
	m.toList()
	m.config.open()
	m.body.SetYOffset(0)
	m.dialog = dialogConfig
	return m, nil
}

// configKey is every press on the config screen, and what it left for the
// surface: a change is applied the moment it is written.
func (m Model) configKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res, cmd := m.config.key(m.surface(), msg)
	m.err = res.err
	switch {
	case res.theme != nil:
		m.applyTheme(*res.theme)
	case res.actions != nil:
		m.setActions(*res.actions)
	case res.targets != nil:
		m.setFiles(res.targets.Shared, res.targets.Projects)
	case res.closed:
		m.dialog = dialogNone
		// The screen took the pane's width; the list gets its own back.
		m.layout()
	}
	return m, cmd
}

// runtimeSwitched takes a probed choice: the core is swapped for one over
// the host it selected. The next survey reads the new host.
func (m Model) runtimeSwitched(msg runtimeMsg) (tea.Model, tea.Cmd) {
	if err := m.config.switched(msg); err != nil {
		m.err = err
		return m, nil
	}
	m.core = m.core.WithRuntime(msg.runtime)
	slog.Info("runtime switched", "want", msg.want, "runtime", hostName(msg.runtime))
	return m, nil
}

// applyTheme repaints every part of the surface that was built with a theme.
// The lists keep their items and cursors; only their styles change.
func (m *Model) applyTheme(th theme.Theme) {
	m.theme = th
	m.amessage = setMessage{} // set in the old theme's colours
	m.link.restyle(th)
	m.help = newHelp(th)
	m.detail.Style = newDetail(th).Style
	for _, in := range []*textinput.Model{&m.input, &m.ainput, &m.create.path, &m.config.chord, &m.proj.edit} {
		styleField(in, th)
	}
	m.layout()
}

// open is a new visit: the cursor on the first row, and no form.
func (cs *configScreen) open() {
	cs.row = int(rowTheme)
	cs.refused = ""
	cs.aform = actionForm{}
	cs.tform = targetForm{}
	cs.dropping = false
}

// key is every press on the config screen. Left and right change the row's
// value; Enter does the same, or opens the trigger key for typing.
func (cs *configScreen) key(sf surface, msg tea.KeyMsg) (configResult, tea.Cmd) {
	switch {
	case cs.chord.Focused():
		return cs.chordKey(sf, msg)
	case cs.aform.open:
		return cs.actionFormKey(sf, msg)
	case cs.tform.open:
		form, cmd := cs.tform.key(sf, msg)
		if form.save != nil {
			return cs.saveTarget(sf, *form.save), cmd
		}
		return configResult{err: form.err}, cmd
	case cs.dropping:
		if _, onTarget := cs.targetRow(sf); onTarget {
			return cs.dropTarget(sf, msg), nil
		}
		return cs.dropAction(sf, msg), nil
	}
	var res configResult
	_, onAction := cs.actionRow(sf)
	_, onTarget := cs.targetRow(sf)
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.closed = true
	case key.Matches(msg, sf.keys.Up):
		cs.row = max(cs.row-1, 0)
	case key.Matches(msg, sf.keys.Down):
		cs.row = min(cs.row+1, cs.addRow(sf))
	case (onAction || onTarget) && key.Matches(msg, sf.keys.Delete):
		cs.dropping = true
	case onTarget && key.Matches(msg, sf.keys.Enter), cs.row == cs.addTargetRow(sf) && key.Matches(msg, sf.keys.Enter):
		return res, cs.openTargetForm(sf, cs.row-int(configRows))
	case onAction && key.Matches(msg, sf.keys.Enter), cs.row == cs.addRow(sf) && key.Matches(msg, sf.keys.Enter):
		return res, cs.openActionForm(sf, cs.row-cs.actionBase(sf))
	case configRow(cs.row) == rowTrigger && key.Matches(msg, sf.keys.Enter):
		cs.chord.SetValue(cs.ui.TriggerKey)
		cs.chord.CursorEnd()
		return res, cs.chord.Focus()
	case msg.Type == tea.KeyLeft:
		return cs.change(-1)
	case msg.Type == tea.KeyRight, key.Matches(msg, sf.keys.Enter):
		return cs.change(+1)
	}
	return res, nil
}

// chordKey is a press while the trigger key is typed. Enter writes it, Esc
// leaves it as it was.
func (cs *configScreen) chordKey(sf surface, msg tea.KeyMsg) (configResult, tea.Cmd) {
	res := configResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		cs.chord.Blur()
		return res, nil
	case key.Matches(msg, sf.keys.Enter):
		raw := strings.TrimSpace(cs.chord.Value())
		if _, res.err = core.ParseChord(raw); res.err != nil {
			return res, nil
		}
		if res.err = writeConfig("ui", "trigger_key", raw); res.err != nil {
			return res, nil
		}
		cs.ui.TriggerKey = raw
		cs.chord.Blur()
		return res, nil
	case altRune(msg):
		return res, nil
	}
	in, cmd := cs.chord.Update(msg)
	cs.chord = in
	return res, cmd
}

// change steps the value of the row under the cursor.
func (cs *configScreen) change(step int) (configResult, tea.Cmd) {
	ui := cs.ui
	switch configRow(cs.row) {
	case rowTheme:
		ui.Theme = cycle(theme.Themes(), orDefault(ui.Theme, theme.DefaultTheme), step)
		return cs.setUI(ui, "theme", ui.Theme), nil
	case rowGlyphs:
		ui.Glyphs = cycle(theme.GlyphSets(), orDefault(ui.Glyphs, theme.DefaultGlyphs), step)
		return cs.setUI(ui, "glyphs", ui.Glyphs), nil
	case rowRuntime:
		cmd, err := cs.switchRuntime(step)
		return configResult{err: err}, cmd
	}
	return configResult{}, nil
}

// setUI writes one key of [ui], and hands back the theme the surface is
// repainted in.
func (cs *configScreen) setUI(ui config.UI, key, value string) configResult {
	th, err := theme.Lookup(ui.Theme, ui.Glyphs)
	if err != nil {
		return configResult{err: err}
	}
	if err := writeConfig("ui", key, value); err != nil {
		return configResult{err: err}
	}
	cs.ui = ui
	return configResult{theme: &th}
}

// switchRuntime probes the next runtime choice, off the update loop: a probe
// runs the host's own tool. The choice is written only once a host answers,
// because a configured host that probes badly is a startup error, and the
// next start must not be refused over a choice made here.
func (cs *configScreen) switchRuntime(step int) (tea.Cmd, error) {
	if cs.pick == nil {
		return nil, errors.New("this surface has no runtime hosts to choose from")
	}
	if cs.switching != "" {
		return nil, nil
	}
	// A choice that did not probe is stepped from, not the configured one:
	// stepping from the configured one would offer the refused choice again,
	// and a direction with a broken host in it would go nowhere.
	from := cs.runtimeChoice()
	if cs.refused != "" {
		from = cs.refused
	}
	next := cycle(append([]string{autoRuntime}, cs.runtimes...), from, step)
	want := []string{}
	if next != autoRuntime {
		want = []string{next}
	}
	cs.switching = next
	pick := cs.pick
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		defer cancel()
		rt, err := pick(ctx, want)
		return runtimeMsg{want: want, runtime: rt, err: err}
	}, nil
}

// switched takes a probed choice, and writes it when a host answered.
func (cs *configScreen) switched(msg runtimeMsg) error {
	tried := cs.switching
	cs.switching = ""
	if msg.err != nil {
		cs.refused = tried
		return msg.err
	}
	cs.refused = ""
	if err := writeConfig("hosts", "runtime", msg.want); err != nil {
		return err
	}
	cs.runtime = msg.want
	return nil
}

// runtimeChoice is the configured runtime preference as the screen names it:
// auto for none, the host for one it offers, and the list as written for
// anything else.
func (cs *configScreen) runtimeChoice() string {
	switch {
	case len(cs.runtime) == 0:
		return autoRuntime
	case len(cs.runtime) == 1 && slices.Contains(cs.runtimes, cs.runtime[0]):
		return cs.runtime[0]
	}
	return strings.Join(cs.runtime, ", ")
}

// helpKind is what the cursor is on, which decides the footer.
func (cs *configScreen) helpKind(sf surface) configHelp {
	_, onAction := cs.actionRow(sf)
	_, onTarget := cs.targetRow(sf)
	switch {
	case cs.chord.Focused():
		return configTyping
	case cs.aform.open:
		return configInForm
	case cs.tform.open:
		return cs.tform.helpKind()
	case onAction, onTarget:
		return configOnAction
	case cs.row == cs.addRow(sf), cs.row == cs.addTargetRow(sf):
		return configOnAdd
	}
	return configOnSetting
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

// screen stands in the list's place while the screen is up, and the line the
// cursor is on, which the body keeps in view.
func (cs *configScreen) screen(sf surface) (string, int) {
	th := sf.theme
	w := sf.list
	var b strings.Builder
	at := 0
	row := func(r int, label, value, note string) {
		// While a form is open its own cursor is the one on screen.
		sel := cs.row == r && !cs.tform.open
		if sel {
			at = strings.Count(b.String(), "\n")
		}
		b.WriteString(settingLine(th, sel, label, value, note, w) + "\n")
	}
	info := func(label, value, note string) {
		line := th.Path.Render("  ") + th.Meta.Render(pad(label, configLabelWidth)) + th.NameDim.Render(value) + th.Path.Render("  "+note)
		b.WriteString(clipTo(line, w) + "\n")
	}

	b.WriteString(heading(th, "Appearance", w))
	row(int(rowTheme), "theme", "‹ "+orDefault(cs.ui.Theme, theme.DefaultTheme)+" ›", "")
	row(int(rowGlyphs), "glyphs", "‹ "+orDefault(cs.ui.Glyphs, theme.DefaultGlyphs)+" ›", "")
	trigger := orDefault(cs.ui.TriggerKey, config.DefaultTriggerKey)
	if cs.chord.Focused() {
		trigger = cs.chord.View()
	}
	row(int(rowTrigger), "trigger key", trigger, "the desktop key that opens revier")

	b.WriteString(heading(th, "Hosts", w))
	note := "in use: " + hostName(sf.core.Runtime)
	if cs.switching != "" {
		note = "checking " + cs.switching + "…"
	}
	row(int(rowRuntime), "runtime", "‹ "+cs.runtimeChoice()+" ›", note)
	info("window", hostName(sf.core.Window), "detected at start")

	b.WriteString(heading(th, "Targets", w))
	tform := func() {
		lines, line := cs.tform.lines(th, w)
		at = strings.Count(b.String(), "\n") + line
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, t := range sf.targets {
		row(int(configRows)+i, keyLabel(t.Key), string(t.Name), describeTarget(t))
		if cs.tform.open && cs.tform.index == i {
			tform()
		}
	}
	row(cs.addTargetRow(sf), "+", "add a target", "every project has it")
	if cs.tform.open && cs.tform.index == len(sf.targets) {
		tform()
	}

	b.WriteString(heading(th, "Actions", w))
	form := func() {
		lines, field := cs.aform.lines(th, w)
		at = strings.Count(b.String(), "\n") + field
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	for i, act := range sf.actions {
		if cs.aform.open && cs.aform.index == i {
			form()
			continue
		}
		row(cs.actionBase(sf)+i, keyLabel(act.Key), act.Name, joinCommand(act.Run))
	}
	if cs.aform.open && cs.aform.index == len(sf.actions) {
		form()
	} else {
		row(cs.addRow(sf), "+", "add an action", "run in the selected project, bound to a key")
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
