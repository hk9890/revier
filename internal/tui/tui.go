// Package tui is the one surface: every project with its agent state, sorted
// so the ones needing attention come first, and one level down, a project's
// targets and attached instances. Enter activates. It is the picker and the
// monitor at once (docs/design/decisions.md D8).
//
// It reads nothing `revier list --json` does not: core.Survey is the only
// source, refreshed on a timer that never overlaps itself, and every action
// goes through the same core paths the CLI commands use.
//
// It is also where claim-on-appear runs, because it is the one long-lived
// process. A window host that reports events (revier.WindowWatcher) is
// subscribed to, and a window that opens shortly after a launch is claimed at
// once; otherwise successive surveys are diffed, and the claim lands within
// two refresh intervals. State is re-read on every refresh, because the
// launch that starts the clock is written by another process.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

type level int

const (
	levelProjects level = iota
	levelTargets
)

// Model is the bubbletea model. Construct it with New.
type Model struct {
	core      *core.Core
	projects  []core.Project
	stateRoot string
	actions   []config.Action
	refresh   time.Duration

	views    []revier.ProjectView // attention first, then config order
	windows  []revier.Instance    // the window host's listing at the last survey
	surveyed bool                 // whether windows holds a listing yet
	attached map[revier.ProjectName][]revier.TargetRef
	err      error // the last failure, shown in the footer
	level    level
	filter   string
	cursor   int                // row at the project level, into rows()
	tcursor  int                // row at the target level
	current  revier.ProjectName // the project drilled into
	width    int
	height   int
}

// New builds the surface over prepared projects. stateRoot is where revier's
// state lives: attached instances are read from it on every refresh and
// claims are written to it.
func New(c *core.Core, projects []core.Project, stateRoot string, actions []config.Action, refresh time.Duration) Model {
	return Model{core: c, projects: projects, stateRoot: stateRoot, actions: actions, refresh: refresh, width: 80, height: 24}
}

type surveyMsg struct {
	report core.Report
	err    error
}

// windowMsg is one event from a watching window host, and the channel it
// came on, so the next wait reads the same subscription.
type windowMsg struct {
	event  revier.WindowEvent
	ok     bool
	events <-chan revier.WindowEvent
}

type tickMsg struct{}

// actedMsg follows a Go, a Focus, or an action; the next survey shows the
// result. launch is set when the Go was a detached launch, so the record is
// written here, on the update loop, and never races the claim paths.
type actedMsg struct {
	err    error
	launch *state.Launch
}

// Init surveys immediately; the timer starts once the first survey answers.
// A window host that can report events is watched from the start.
func (m Model) Init() tea.Cmd {
	if w, ok := m.core.Window.(revier.WindowWatcher); ok {
		events, err := w.Watch(context.Background())
		if err == nil {
			return tea.Batch(m.Survey(), waitEvent(events))
		}
	}
	return m.Survey()
}

// waitEvent delivers the next window event as a message. Watch is called
// once, in Init: each call starts a subscription, so re-arming reads the
// channel that message carries rather than subscribing again.
func waitEvent(events <-chan revier.WindowEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		return windowMsg{event: ev, ok: ok, events: events}
	}
}

// Survey is one refresh: one bulk listing per host, matched locally. It is a
// command so the terminal stays responsive while hosts answer, and it
// schedules nothing itself, so two surveys never run at once.
func (m Model) Survey() tea.Cmd {
	c, projects := m.core, m.projects
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		report, err := c.Survey(ctx, projects)
		return surveyMsg{report: report, err: err}
	}
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case surveyMsg:
		m.err = msg.err
		if msg.err == nil {
			m.claimByPolling(msg.report.Windows)
			m.views = sorted(msg.report.Views)
			m.windows, m.surveyed = msg.report.Windows, true
		}
		m.clamp()
		return m, tick(m.refresh)
	case windowMsg:
		if !msg.ok {
			return m, nil // the watcher ended; polling still claims
		}
		if msg.event.Kind == revier.WindowOpened {
			m.claimByEvent(msg.event.Instance)
		}
		return m, waitEvent(msg.events)
	case tickMsg:
		return m, m.Survey()
	case actedMsg:
		// The timer's next survey shows the result. Starting one here would
		// add a second survey-tick chain that never ends.
		m.err = msg.err
		m.recordLaunch(msg.launch)
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// claimByPolling diffs the window listing against the previous survey's and
// attaches a window that appeared within the claim window after the last
// launch. State is re-read because the launch was written by another process.
// The same pass drops attachments whose windows are gone, so a claim made
// on every detached launch does not accumulate for the life of the machine.
func (m *Model) claimByPolling(windows []revier.Instance) {
	st, err := state.Load(m.stateRoot)
	if err != nil {
		return
	}
	changed := false
	if m.core.Window != nil {
		live := map[string]bool{}
		for _, w := range windows {
			live[state.Key(w.Ref)] = true
		}
		changed = st.Prune(live)
	}
	if st.Launch != nil && m.surveyed {
		now := time.Now()
		if ref, ok := m.core.Claim(m.windows, windows, st.Launch.At, now, m.projects); ok {
			st.Attach(st.Launch.Project, ref)
			st.Launch, changed = nil, true // consumed
		} else if now.Sub(st.Launch.At) > core.ClaimWindow {
			st.Launch, changed = nil, true // expired
		}
	}
	if changed {
		_ = st.Save(m.stateRoot)
	}
	m.attached = st.Attached
}

// claimByEvent is the same decision for a window a watching host reported.
func (m *Model) claimByEvent(inst revier.Instance) {
	st, err := state.Load(m.stateRoot)
	if err != nil || st.Launch == nil {
		return
	}
	if !m.core.ClaimEvent(inst, st.Launch.At, time.Now(), m.projects) {
		return
	}
	st.Attach(st.Launch.Project, inst.Ref)
	st.Launch = nil
	_ = st.Save(m.stateRoot)
	m.attached = st.Attached
}

// recordLaunch starts the claim-on-appear clock for a detached launch.
func (m *Model) recordLaunch(l *state.Launch) {
	if l == nil {
		return
	}
	st, err := state.Load(m.stateRoot)
	if err != nil {
		return
	}
	st.Launch = l
	_ = st.Save(m.stateRoot)
}

// sorted puts projects needing attention first and otherwise keeps config
// order, so rows move only when an agent's state changes.
func sorted(views []revier.ProjectView) []revier.ProjectView {
	out := make([]revier.ProjectView, len(views))
	copy(out, views)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Attention() && !out[j].Attention()
	})
	return out
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.level == levelProjects && msg.String() == "q" && m.filter != "" {
			break // "q" is a letter of a project name while filtering
		}
		return m, tea.Quit
	case "esc":
		switch {
		case m.level == levelTargets:
			m.level = levelProjects
		case m.filter != "":
			m.filter = ""
			m.clamp()
		default:
			return m, tea.Quit
		}
		return m, nil
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "enter":
		return m.enter()
	case "backspace":
		if m.level == levelProjects && m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.clamp()
		}
		return m, nil
	}
	if cmd, ok := m.action(msg); ok {
		return m, cmd
	}
	if m.level == levelProjects && msg.Type == tea.KeyRunes {
		m.filter += string(msg.Runes)
		m.clamp()
	}
	return m, nil
}

func (m *Model) move(delta int) {
	if m.level == levelTargets {
		m.tcursor += delta
	} else {
		m.cursor += delta
	}
	m.clamp()
}

func (m *Model) clamp() {
	if n := len(m.rows()); m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if n := len(m.targetRows()); m.tcursor >= n {
		m.tcursor = n - 1
	}
	if m.tcursor < 0 {
		m.tcursor = 0
	}
}

// rows is the project level after the filter: indices into views.
func (m Model) rows() []int {
	var out []int
	f := strings.ToLower(m.filter)
	for i, v := range m.views {
		if f == "" || strings.Contains(strings.ToLower(string(v.Project.Name)), f) {
			out = append(out, i)
		}
	}
	return out
}

// selected is the project under the cursor at the project level, or the one
// drilled into at the target level.
func (m Model) selected() (revier.ProjectView, bool) {
	if m.level == levelTargets {
		for _, v := range m.views {
			if v.Project.Name == m.current {
				return v, true
			}
		}
		return revier.ProjectView{}, false
	}
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return revier.ProjectView{}, false
	}
	return m.views[rows[m.cursor]], true
}

func (m Model) project(name revier.ProjectName) (core.Project, bool) {
	for _, p := range m.projects {
		if p.Name == name {
			return p, true
		}
	}
	return core.Project{}, false
}

// targetRow is one line at the target level: a declared target, or an
// attached instance, which has a ref and no name.
type targetRow struct {
	target   revier.TargetView
	attached revier.TargetRef
}

func (m Model) targetRows() []targetRow {
	v, ok := m.selected()
	if !ok {
		return nil
	}
	var out []targetRow
	for _, t := range v.Targets {
		out = append(out, targetRow{target: t})
	}
	for _, ref := range m.attached[v.Project.Name] {
		out = append(out, targetRow{attached: ref})
	}
	return out
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	if m.level == levelProjects {
		v, ok := m.selected()
		if !ok {
			return m, nil
		}
		m.current = v.Project.Name
		m.level = levelTargets
		m.tcursor = 0
		return m, nil
	}
	rows := m.targetRows()
	if m.tcursor < 0 || m.tcursor >= len(rows) {
		return m, nil
	}
	row := rows[m.tcursor]
	p, ok := m.project(m.current)
	if !ok {
		return m, nil
	}
	c := m.core
	if !row.attached.IsZero() {
		ref := row.attached
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return actedMsg{err: c.Focus(ctx, ref)}
		}
	}
	name := row.target.Name
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ref, err := c.Go(ctx, p, name)
		msg := actedMsg{err: err}
		if err == nil && ref.IsZero() {
			// A detached launch: the window that appears next may be claimed.
			msg.launch = &state.Launch{Project: p.Name, At: time.Now()}
		}
		return msg
	}
}

// action runs the configured action bound to the key, if any, against the
// selected project. The terminal is handed to the command while it runs, and
// the argv is rendered by the same rules `revier run` uses.
func (m Model) action(msg tea.KeyMsg) (tea.Cmd, bool) {
	pressed := msg.String()
	for _, act := range m.actions {
		if keyName(act.Key) != pressed {
			continue
		}
		v, ok := m.selected()
		if !ok {
			return nil, true
		}
		p, ok := m.project(v.Project.Name)
		if !ok {
			return nil, true
		}
		argv, err := core.RenderArgv(p.Project, act.Run)
		if err != nil || len(argv) == 0 {
			return func() tea.Msg { return actedMsg{err: fmt.Errorf("action %q: %w", act.Name, err)} }, true
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = p.Path
		return tea.ExecProcess(cmd, func(err error) tea.Msg { return actedMsg{err: err} }), true
	}
	return nil, false
}

// keyName maps a configured key ("ctrl-y") to the name bubbletea reports
// ("ctrl+y").
func keyName(k string) string { return strings.ReplaceAll(strings.ToLower(k), "-", "+") }

var (
	styleHeader    = lipgloss.NewStyle().Bold(true)
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleCursor    = lipgloss.NewStyle().Reverse(true)
	styleAttention = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleRunning   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleIdle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")

	var lines []string
	cursor := m.cursor
	if m.level == levelTargets {
		lines = m.targetLines()
		cursor = m.tcursor
	} else {
		lines = m.projectLines()
	}
	// Keep the cursor on screen: the header and footer take three lines.
	visible := m.height - 3
	if visible < 1 {
		visible = 1
	}
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	for i := start; i < end; i++ {
		line := lines[i]
		if i == cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	for i := end - start; i < visible; i++ {
		b.WriteString("\n")
	}
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	running, attention := 0, 0
	for _, v := range m.views {
		if v.Running {
			running++
		}
		if v.Attention() {
			attention++
		}
	}
	title := fmt.Sprintf(" revier  %d projects · %d running · %d need you", len(m.views), running, attention)
	if m.level == levelTargets {
		title = fmt.Sprintf(" revier  %s", m.current)
	} else if m.filter != "" {
		title += "   /" + m.filter
	}
	return styleHeader.Render(title)
}

func (m Model) footer() string {
	var help string
	if m.level == levelTargets {
		help = " enter go · esc back · q quit"
	} else {
		help = " enter targets · type to filter · esc clear/quit"
	}
	for _, act := range m.actions {
		help += " · " + act.Key + " " + act.Name
	}
	if m.err != nil {
		return styleAttention.Render(" " + m.err.Error())
	}
	return styleDim.Render(help)
}

func (m Model) projectLines() []string {
	rows := m.rows()
	nameWidth := 0
	for _, i := range rows {
		if n := lipgloss.Width(string(m.views[i].Project.Name)); n > nameWidth {
			nameWidth = n
		}
	}
	if nameWidth > 32 {
		nameWidth = 32
	}
	lines := make([]string, 0, len(rows))
	for _, i := range rows {
		v := m.views[i]
		mark, state := styleDim.Render("·"), styleDim.Render("-      ")
		if v.Running {
			mark, state = styleRunning.Render("●"), "running"
		}
		if v.Attention() {
			mark = styleAttention.Render("!")
		}
		lines = append(lines, fmt.Sprintf(" %s %s  %s  %s", mark, pad(string(v.Project.Name), nameWidth), state, agentLine(v)))
	}
	return lines
}

// agentLine is the worst agent state in the project and its activity: the
// line that answers "which one needs me" at a glance.
func agentLine(v revier.ProjectView) string {
	if len(v.Agents) == 0 {
		return ""
	}
	worst := revier.AgentState{}
	for _, a := range v.Agents {
		if a.State.Status >= worst.Status {
			worst = a.State
		}
	}
	label := worst.Status.String()
	switch worst.Status {
	case revier.StatusAttention:
		label = styleAttention.Render(label)
	case revier.StatusRunning:
		label = styleRunning.Render(label)
	case revier.StatusIdle:
		label = styleIdle.Render(label)
	default:
		label = styleDim.Render(label)
	}
	if worst.Activity == "" {
		return label
	}
	return label + " " + worst.Activity
}

func (m Model) targetLines() []string {
	rows := m.targetRows()
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		if !r.attached.IsZero() {
			lines = append(lines, fmt.Sprintf("   %s  %s  %s", pad("", 14), pad(r.attached.Title, 24), styleDim.Render("attached · "+r.attached.Host)))
			continue
		}
		t := r.target
		mark := styleDim.Render("·")
		state := styleDim.Render("-")
		switch {
		case !t.Available:
			state = styleDim.Render("unavailable")
		case !t.Ref.IsZero():
			mark, state = styleRunning.Render("●"), "running"
		}
		lines = append(lines, fmt.Sprintf(" %s %s  %s  %s  %s", mark, pad(t.Key, 14), pad(string(t.Name), 24), pad(t.Host, 6), state))
	}
	return lines
}

func pad(s string, width int) string {
	if n := lipgloss.Width(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}
