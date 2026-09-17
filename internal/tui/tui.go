// Package tui is the one surface: every project with its agent state, sorted
// so the ones needing attention come first, and beside it a pane with the
// project's targets, attached instances and agents, which Tab moves the
// cursor through. Enter activates. It is the picker and the monitor at once
// (docs/design/decisions.md D8, D73).
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
	"io"
	"log/slog"
	"os/exec"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// focus is the section the cursor is in: the project list, or the target or
// agent rows of the pane beside it. The pane is the same content either way;
// focus decides what typing filters and what up, down and Enter act on
// (sections.go).
type focus int

const (
	focusList focus = iota
	focusTargets
	focusAgents
)

// dialog is a screen standing over the surface: the link dialog's three steps
// - which host, which of its projects (decisions.md D45), and the link's name - the
// new-project field, the sessions screen and its name step, or the config screen. dialogNone is the surface itself, which is where it is
// nearly always.
type dialog int

const (
	dialogNone dialog = iota
	dialogHosts
	dialogRemote
	dialogLinkName
	dialogNew
	dialogConfig
	dialogHelp
	dialogSessions
	dialogSessionName
	dialogShutdown
)

// hasRows reports a screen whose body is list rows: what a click selects and
// the wheel moves through. Every other screen stands over the project list
// with text of its own, and a press must not reach a row nobody can see.
func (d dialog) hasRows() bool {
	switch d {
	case dialogNone, dialogHosts, dialogRemote, dialogSessions:
		return true
	}
	return false
}

// hasPane reports a screen with the detail pane beside it. The help and
// config screens are about no project, so they take the whole width.
func (d dialog) hasPane() bool {
	return d != dialogHelp && d != dialogConfig
}

// Model is the bubbletea model. Construct it with New.
type Model struct {
	core      *core.Core
	projects  []core.Project
	stateRoot string
	actions   []config.Action
	shared    []map[string]any // config.toml's shared targets, for a project file read again
	refresh   time.Duration
	theme     theme.Theme
	frame     int  // the spinner frame a working agent shows
	spinning  bool // whether a spin tick is out, so a survey starts no second one

	views    []revier.ProjectView // attention first, then config order
	windows  []revier.Instance    // the window host's listing at the last survey
	surveyed bool                 // whether a survey has answered: windows holds a listing, and the counts are real
	attached map[revier.ProjectName][]revier.TargetRef
	bound    map[revier.ProjectName]core.Bindings // where targets last landed, from state
	pending  *state.Launch                        // the launch still coming up, from state
	// Two failures, because they end differently. err is what a key or an
	// activation ran into, and it stays until the next key: a refusal that
	// the next refresh wiped would be on screen for under a second.
	// surveyErr is the last refresh's, and the next refresh replaces it.
	err       error
	surveyErr error
	focus     focus
	tcursor   int                // the target row the pane's cursor is on
	tfilter   string             // the query over the target rows
	tinput    textinput.Model    // the target query, with its own cursor
	tlines    []int              // the pane line each target row is on, for the cursor and a click
	tfield    int                // the pane line the target query is on
	acursor   int                // the agent row the pane's cursor is on
	afilter   string             // the query over the agent rows
	ainput    textinput.Model    // the agent query, with its own cursor
	alines    []lineSpan         // the pane lines each agent row takes, for the cursor and a click
	afield    int                // the pane line the agent query is on, -1 with no Agents section
	before    revier.ProjectName // the project the cursor was on when the query began, for when it is cleared
	confirm   revier.ProjectName // the project a delete is waiting on an answer for
	dialog    dialog             // the link dialog, while it is up
	host      string             // the host the dialog's second step shows
	asking    string             // the host an ask is out to, while it is
	saving    bool               // whether a session save is out
	restoring string             // the session a restore is walking, while it is
	outcome   sessionOutcome     // what the last save or restore came to
	shut      shutdown           // the shutdown wizard, while it is up
	width     int
	height    int

	// The project list. Cursor, paging and fuzzy filtering are the
	// component's; what a row looks like is the delegate's.
	plist  list.Model
	hlist  list.Model // the hosts, the link dialog's first step
	rlist  list.Model // a host's projects, its second
	slist  list.Model // the saved sessions
	filter string     // the query, held here so a refresh can re-apply it
	keys   keyMap
	help   help.Model
	detail viewport.Model
	shown  revier.ProjectName               // the project the pane holds, so a new one starts at its top
	tkeys  map[core.Chord]revier.TargetName // press to target name, over every project
	start  revier.ProjectName               // the project to open on, from the working directory
	trees  map[string]treeEntry             // cached directory listings, by project path
	input  textinput.Model                  // the filter query, with its own cursor
	path   textinput.Model                  // the directory field of the new-project screen
	lname  textinput.Model                  // the name field of the link dialog's last step
	sname  textinput.Model                  // the name field of a session being saved
	over   hovered                          // what the pointer is on
	cell   *pointerCell                     // where the pointer last was, nil before it moved
	body   viewport.Model                   // the scrolling window over the list
	last   click                            // the last click on a row, for telling a double click
	press  *press                           // where the left button went down, while it is down
	sel    selection                        // the box a drag is selecting
	copied int                              // the characters the last selection copied, shown until the next press
	// proposed is whether the link's name is still the one offered, which
	// the first character typed replaces.
	proposed bool

	// The config screen.
	ui        config.UI       // [ui] as config.toml holds it
	runtime   []string        // [hosts] runtime as config.toml holds it
	runtimes  []string        // the runtime hosts the screen offers besides auto
	pick      RuntimeSelector // probes a runtime choice, as startup does
	switching string          // the runtime choice being probed
	refused   string          // the last runtime choice that did not probe, stepped from next
	crow      int             // the row the screen's cursor is on
	chord     textinput.Model // the trigger key, while it is typed
	aform     actionForm      // the action being added or changed, while its form is up
	targets   []revier.Target // the shared targets, typed, as config.toml holds them
	tform     targetForm      // the shared target being added or changed, while its form is up
	dropping  bool            // whether the target or action under the cursor waits on a y to be deleted
}

// New builds the surface over prepared projects. stateRoot is where revier's
// state lives: attached instances are read from it on every refresh and
// claims are written to it.
func New(c *core.Core, projects []core.Project, stateRoot string, cfg *config.Config, refresh time.Duration, th theme.Theme, start revier.ProjectName) Model {
	actions := cfg.Actions
	keys := newKeyMap(actions)
	m := Model{
		core: c, projects: projects, stateRoot: stateRoot, actions: actions, shared: cfg.Targets,
		refresh: refresh, theme: th, width: 80, height: 24,
		plist: newProjectList(th),
		hlist: newHostList(th), rlist: newRemoteList(th), slist: newSessionList(th),
		keys: keys, help: newHelp(th), detail: newDetail(th),
		tkeys: targetKeys(projects, keys), start: start, input: newPrompt(th, projectPlaceholder),
		tinput: newPrompt(th, targetPlaceholder), ainput: newPrompt(th, agentPlaceholder), afield: -1,
		path: newPathInput(th), lname: newLinkNameInput(th), sname: newSessionNameInput(th),
		body: newBody(),
		ui:   cfg.UI, runtime: cfg.Hosts.Runtime, chord: newChordInput(th),
	}
	// The first survey matches through the bindings too. Left to the survey's
	// own answer to fill in, they reach only the second one, a refresh later.
	if st, err := state.Load(stateRoot); err == nil {
		m.keep(st)
	}
	// The project field has the cursor from the start: the surface filters as
	// you type, so it is where a keystroke lands. Init focuses it again for
	// the blink command; this is what makes it accept keys at all.
	_ = m.input.Focus()
	// config.Load has decoded them already, so this cannot fail.
	m.targets, _ = config.DecodeTargets(cfg.Targets)
	m.layout()
	return m
}

// surveyMsg is one survey's answer, and the state it started from: what it
// may prune (state.Prune).
type surveyMsg struct {
	report core.Report
	before *state.State
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

// spinMsg advances the working spinner. It runs on its own timer, because
// the survey's is seconds long and a spinner that fast does not read as one.
type spinMsg struct{}

const spinInterval = 120 * time.Millisecond

func spin() tea.Cmd {
	return tea.Tick(spinInterval, func(time.Time) tea.Msg { return spinMsg{} })
}

// actedMsg follows a Go, a Focus, or an action; the next survey shows the
// result. Its state changes are applied here, on the update loop, and never
// race the claim paths: launch records a launch still coming up, bind pins
// where a target landed.
type actedMsg struct {
	err    error
	launch *state.Launch
	bind   *binding
}

type binding struct {
	project revier.ProjectName
	target  revier.TargetName
	ref     revier.TargetRef
}

// launchedMsg follows a Go that started a process whose window it cannot name
// yet. The launch reaches state before the wait for that window starts, so a
// second press - here or on a desktop key - finds it and reports the target
// coming up instead of launching a second copy (decisions.md D21).
type launchedMsg struct {
	project core.Project
	launch  state.Launch
	before  []revier.Instance
}

// Init surveys immediately; the timer starts once the first survey answers.
// A window host that can report events is watched from the start.
func (m Model) Init() tea.Cmd {
	blink := m.input.Focus()
	if w, ok := m.core.Window.(revier.WindowWatcher); ok {
		events, err := w.Watch(context.Background())
		if err == nil {
			return tea.Batch(m.Survey(), waitEvent(events), blink)
		}
		slog.Warn("window watch, claiming by polling only", "host", m.core.Window.Name(), "err", err)
	}
	return tea.Batch(m.Survey(), blink)
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
	c, projects, bound, root := m.core, m.projects, m.bound, m.stateRoot
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// A state that cannot be read is nil, which lets nothing be pruned.
		before, _ := loadState(root)
		// No attachments: the surface lists them from state, which a claim
		// updates between surveys, so a claimed window shows at once rather
		// than a refresh later.
		report, err := c.Survey(ctx, projects, bound, nil)
		return surveyMsg{report: report, before: before, err: err}
	}
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// Update handles the message and then rebuilds the detail pane, so the pane
// is a function of the state after the message rather than something every
// branch has to remember to refresh.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	mm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if mm.err != nil && mm.err != m.err && !loggedAlready(msg) {
		slog.Error("tui", "err", mm.err)
	}
	// The pointer moving within one row, or over nothing, changes nothing on
	// the screen, and a terminal reports every cell it crosses.
	if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionMotion && mm.over == m.over {
		return mm, cmd
	}
	// The screen is frozen under a selection: nothing drawn now is shown, and
	// the release that ends it redraws everything.
	if mm.sel.active {
		return mm, cmd
	}
	if _, ok := msg.(spinMsg); ok {
		mm.redrawSpin()
		return mm, cmd
	}
	mm.syncDetail()
	mm.syncBody()
	// A key, a survey or a screen change can move what is under a pointer
	// that stayed where it was, so what it is over is asked again.
	if mm.cell != nil {
		if over := mm.hoverAt(mm.cell.x, mm.cell.y); over != mm.over {
			mm.over = over
			mm.syncDetail()
			mm.syncBody()
		}
	}
	return mm, cmd
}

// loggedAlready reports a message whose error the operation behind it has
// logged: a Go, a bind, a focus or an action. Every other error set on m.err -
// a form refused, a file that did not load, a clone - is logged by Update. A
// failed survey is shown from m.surveyErr and logged by core.Survey.
func loggedAlready(msg tea.Msg) bool {
	switch msg.(type) {
	case actedMsg, restoredMsg, savedMsg, shutdownMsg:
		return true
	}
	return false
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case surveyMsg:
		m.surveyErr = msg.err
		if msg.err == nil {
			m.claimByPolling(msg.report, msg.before)
			m.views = sorted(m.known(m.uncovered(msg.report.Views)))
			m.windows, m.surveyed = msg.report.Windows, true
			m.reload()
		}
		if !m.spinning && m.anyWorking() {
			m.spinning = true
			return m, tea.Batch(tick(m.refresh), spin())
		}
		return m, tick(m.refresh)
	case spinMsg:
		// The spinner stops when nothing works, so an idle surface does not
		// redraw; the next survey that finds a working agent starts it again.
		if !m.anyWorking() {
			m.spinning = false
			return m, nil
		}
		m.frame++
		return m, spin()
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
		m.apply(msg)
		return m, nil
	case launchedMsg:
		m.apply(actedMsg{launch: &msg.launch})
		return m, m.bindLaunch(msg)
	case editedMsg:
		m.err = m.reread(msg)
		return m, nil
	case askedMsg:
		return m.asked(msg)
	case savedMsg:
		return m.saved(msg)
	case restoredMsg:
		return m.restored(msg)
	case plannedMsg:
		return m.planned(msg)
	case shutdownMsg:
		return m.shutDown(msg)
	case ledgerMsg:
		return m.ledgerWritten(msg)
	case runtimeMsg:
		return m.runtimeSwitched(msg)
	case clonedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, m.goTarget(msg.project, msg.home)
	case tea.KeyMsg:
		m.copied = 0
		if m.sel.active {
			return m.selectingKey(msg)
		}
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	// A blink is a field's own timer message; the field it is not for
	// ignores it.
	var cmds [3]tea.Cmd
	m.input, cmds[0] = m.input.Update(msg)
	m.tinput, cmds[1] = m.tinput.Update(msg)
	m.ainput, cmds[2] = m.ainput.Update(msg)
	return m, tea.Batch(cmds[:]...)
}

// claimByPolling diffs the window listing against the previous survey's and
// settles the last launch with a window that appeared since: bound to its
// target, or attached to the project when an action launched. The launch may
// have been written by another process, so the claim is made on the state on
// disk. The same pass drops attachments and bindings whose instances are
// gone, of those the survey started from: one written while it listed is the
// next survey's.
func (m *Model) claimByPolling(report core.Report, before *state.State) {
	m.updateState(func(st *state.State) bool {
		changed := st.Prune(report.Hosts, report.Instances, before)
		l, ok := m.launch(st)
		if !ok || !m.surveyed {
			return changed
		}
		now := time.Now()
		if claimed, ok := m.core.Claim(m.windows, report.Windows, l, now, m.projects); ok {
			claim(st, l, claimed, "poll")
			return true
		}
		if !l.Pending(now) {
			slog.Info("claim: launch expired with no window claimed", "project", l.Project.Name, "target", l.Target, "launched_at", l.At)
			st.Launch = nil
			return true
		}
		return changed
	})
}

// claimByEvent is the same decision for a window a watching host reported.
func (m *Model) claimByEvent(inst revier.Instance) {
	m.updateState(func(st *state.State) bool {
		l, ok := m.launch(st)
		if !ok {
			return false
		}
		claimed, ok := m.core.ClaimEvent(inst, l, time.Now(), m.projects)
		if ok {
			claim(st, l, claimed, "event")
		}
		return ok
	})
}

func claim(st *state.State, l core.Launch, c core.Claimed, by string) {
	slog.Info("claim", "project", l.Project.Name, "target", c.Target, "ref", c.Ref, "by", by)
	st.Claim(c.Target, c.Ref)
}

// loadState reads state for a survey to start from. A state file that cannot
// be read or written costs the next keypress a fallback, not the surface, so
// it is logged and not shown; the surface does both every refresh, so a
// failure that lasts is one line (logging.Repeat).
func loadState(root string) (*state.State, error) {
	st, err := state.Load(root)
	logging.Repeat("state load", "state load", err, "root", root)
	return st, err
}

// updateState is every write of state on the surface: the change is made to
// the state on disk under its lock, and the surface keeps the result.
func (m *Model) updateState(apply func(st *state.State) bool) {
	st, err := state.Update(m.stateRoot, apply)
	logging.Repeat("state update", "state update", err, "root", m.stateRoot)
	if st != nil {
		m.keep(st)
	}
}

// keep holds the parts of state the surface reads between refreshes.
func (m *Model) keep(st *state.State) {
	m.attached, m.bound, m.pending = st.Attached, st.Bound, st.Launch
}

// launch is the pending launch in state as the core sees it.
func (m Model) launch(st *state.State) (core.Launch, bool) {
	if st.Launch == nil {
		return core.Launch{}, false
	}
	p, ok := m.project(st.Launch.Project)
	if !ok {
		return core.Launch{}, false
	}
	return core.Launch{Project: p, Target: st.Launch.Target, At: st.Launch.At}, true
}

// apply writes what an activation learned: a launch still coming up, or the
// ref a target landed on. Either makes its project the current one, as a CLI
// command makes it: a desktop key pressed next on a window no rule names
// falls back to it.
func (m *Model) apply(msg actedMsg) {
	if msg.launch == nil && msg.bind == nil {
		return
	}
	m.updateState(func(st *state.State) bool {
		if l := msg.launch; l != nil {
			st.Launched(l.Project, l.Target, l.At)
		}
		if b := msg.bind; b != nil {
			st.Landed(b.project, b.target, b.ref)
		}
		return true
	})
}

// sorted puts projects needing attention first, then the running ones, and
// otherwise keeps config order, so rows move only when a project starts, stops
// or an agent's state changes. Running above stopped is the picker's order
// (os_list_json.py sorts on it too): with ninety projects the handful that are
// open are what a search is almost always for.
func sorted(views []revier.ProjectView) []revier.ProjectView {
	out := make([]revier.ProjectView, len(views))
	copy(out, views)
	rank := func(v revier.ProjectView) int {
		switch {
		case v.Attention():
			return 0
		case v.Running:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rank(out[i]) < rank(out[j])
	})
	return out
}

// key routes a press. Everything the surface owns is matched here, and only
// what is left over reaches the list - which is why every printable rune is a
// filter character and never a list command: the surface filters as you type,
// the way the picker it replaces does, so no letter can be a shortcut.
func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm != "" {
		return m.confirmDelete(msg)
	}
	switch m.dialog {
	case dialogNew:
		return m.newKey(msg)
	case dialogLinkName:
		return m.linkNameKey(msg)
	case dialogConfig:
		return m.configKey(msg)
	case dialogHelp:
		return m.helpScreenKey(msg)
	case dialogSessions:
		return m.sessionsKey(msg)
	case dialogSessionName:
		return m.sessionNameKey(msg)
	case dialogShutdown:
		return m.shutdownKey(msg)
	}
	if m.dialog != dialogNone {
		return m.dialogKey(msg)
	}
	m.err = nil
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		switch {
		case m.field().Value() != "":
			m.query(m.focus, "")
		case m.focus != focusList:
			m.toList()
		default:
			return m, tea.Quit
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.moveCursor(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1)
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.enter()
	case key.Matches(msg, m.keys.Next):
		return m.step(1)
	case key.Matches(msg, m.keys.Prev):
		return m.step(-1)
	case key.Matches(msg, m.keys.Edit):
		return m.editFile()
	case key.Matches(msg, m.keys.Delete):
		return m.askDelete()
	}
	if next, cmd, ok := m.barKey(msg); ok {
		return next, cmd
	}
	if cmd, ok := m.action(msg); ok {
		return opened(m, cmd)
	}
	// A target key names a target of the highlighted project wherever the
	// cursor is; it does not read the pane's cursor.
	if next, cmd, ok := m.targetKey(msg); ok {
		return next, cmd
	}
	if m.promptKey(msg) {
		return m.edit(msg)
	}
	return m, nil
}

// selected is the project under the cursor.
func (m Model) selected() (revier.ProjectView, bool) {
	it, ok := m.plist.SelectedItem().(projectItem)
	if !ok {
		return revier.ProjectView{}, false
	}
	return it.view, true
}

func (m Model) project(name revier.ProjectName) (core.Project, bool) {
	for _, p := range m.projects {
		if p.Name == name {
			return p, true
		}
	}
	return core.Project{}, false
}

// targetRow is one row of the pane's Targets section: a declared target, or an
// attached instance, which has a ref and no name. matches are the byte
// positions of its label the query matched, for the highlight.
type targetRow struct {
	target   revier.TargetView
	attached revier.TargetRef
	matches  []int
}

// label is what the target query matches: a target's name, or an attached
// instance's title.
func (r targetRow) label() string {
	if !r.attached.IsZero() {
		return r.attached.Title
	}
	return string(r.target.Name)
}

// same reports whether two rows are one target or one attached instance,
// whatever the query matched.
func (r targetRow) same(o targetRow) bool {
	return r.target.Name == o.target.Name && r.attached == o.attached
}

// moveCursor moves the cursor of the section it is in by rows.
func (m *Model) moveCursor(by int) {
	switch m.focus {
	case focusTargets:
		m.tcursor += by
	case focusAgents:
		m.acursor += by
	case focusList:
		if by < 0 {
			m.plist.CursorUp()
		} else {
			m.plist.CursorDown()
		}
	}
}

// targetRows is the pane's Targets section: every target, then every
// attached instance, or, while a target query is typed, the rows it matches
// ranked as the list ranks projects.
func (m Model) targetRows() []targetRow {
	v, ok := m.selected()
	if !ok {
		return nil
	}
	var all []targetRow
	for _, t := range v.Targets {
		all = append(all, targetRow{target: t})
	}
	for _, ref := range m.attached[v.Project.Name] {
		all = append(all, targetRow{attached: ref})
	}
	if m.tfilter == "" {
		return all
	}
	labels := make([]string, len(all))
	for i, r := range all {
		labels[i] = r.label()
	}
	var out []targetRow
	for _, rank := range list.DefaultFilter(m.tfilter, labels) {
		r := all[rank.Index]
		r.matches = rank.MatchedIndexes
		out = append(out, r)
	}
	return out
}

// enter acts on the row under the cursor, by key or by double click.
func (m Model) enter() (Model, tea.Cmd) {
	return opened(m.act())
}

// opened ends the search once a press has something to run: Enter, a double
// click, a click on a pane row, a target key or an action. The query was the
// way to what now opens, so the surface comes back with empty fields
// (decisions.md D73). The search ends also when the run fails later; the
// failure is said in the footer.
func opened(m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	if cmd != nil {
		m.endSearch()
	}
	return m, cmd
}

// act on the list opens the project: its home target, the same
// run-or-raise `revier go home` does. Searching for a project is almost always
// to get to it, so the targets are the detour and get the other key. A
// project with no home target has nothing to open, so Enter moves the cursor
// to its targets instead. In the pane, Enter runs the target under the
// cursor, or brings the agent under it to the front.
//
// A project whose directory is not on this machine is cloned first, when its
// file says from where, as `revier open` does.
func (m Model) act() (Model, tea.Cmd) {
	switch m.focus {
	case focusTargets:
		return m, m.goRow(m.tcursor)
	case focusAgents:
		return m, m.goAgentRow(m.acursor)
	}
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	p, ok := m.project(v.Project.Name)
	if !ok {
		return m, nil
	}
	if home, ok := p.Home(); ok {
		// A remote project's checkout is its host's: the pane opened
		// here runs `revier open` there, which clones (decisions.md D40).
		if p.Remote != nil {
			return m, m.goTarget(p, home.Name)
		}
		if !v.PathExists && p.GitURL != "" {
			return m, m.clone(p, home.Name)
		}
		if !v.PathExists && !v.Running {
			// Nothing to clone from, and nothing to raise: refused as
			// `revier open` refuses it, rather than started in whatever
			// directory the runtime falls back to. The check is made
			// again, as the directory may have appeared since the survey.
			if _, err := checkout.Ensure(p.Project, io.Discard); err != nil {
				m.err = err
				return m, nil
			}
		}
		return m, m.goTarget(p, home.Name)
	}
	return m.focusOn(focusTargets), nil
}

// goRow activates one row of the pane: an attached instance is focused
// through the host that produced it, a target is run-or-raised.
func (m Model) goRow(i int) tea.Cmd {
	rows := m.targetRows()
	if i < 0 || i >= len(rows) {
		return nil
	}
	row := rows[i]
	if ref := row.attached; !ref.IsZero() {
		c := m.core
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			start := time.Now()
			err := c.Focus(ctx, ref)
			logging.Op("focus attached", start, err, "ref", ref)
			return actedMsg{err: err}
		}
	}
	v, _ := m.selected()
	p, ok := m.project(v.Project.Name)
	if !ok {
		return nil
	}
	return m.goTarget(p, row.target.Name)
}

// goTarget is one activation: core.Activate, and settle where it landed.
// Enter on a row of the pane and a target key on the list are the same
// operation, so they are the same command.
func (m Model) goTarget(p core.Project, name revier.TargetName) tea.Cmd {
	c := m.core
	bound := m.bound[p.Name]
	pending := m.pending.Pending(p.Name, name, core.BindWindow)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), core.BindWait)
		defer cancel()
		res, err := c.Activate(ctx, p, name, bound, pending, nil)
		return landed(p, res, err)
	}
}

// landed is what an activation leaves to settle: a launch whose window is
// still coming up, or the ref the target landed on, which is pinned also when
// it comes with an error.
func landed(p core.Project, res core.Result, err error) tea.Msg {
	if res.Launched && res.Ref.IsZero() && err == nil {
		return launchedMsg{
			project: p,
			launch:  state.Launch{Project: p.Name, Target: res.Target, At: time.Now()},
			before:  res.Before,
		}
	}
	if res.Ref.IsZero() {
		return actedMsg{err: err}
	}
	return actedMsg{err: err, bind: &binding{project: p.Name, target: res.Target, ref: res.Ref}}
}

// bindLaunch waits for the window a detached launch produces and binds it. If
// it takes longer than the wait, the launch record lets a later refresh bind
// it. The wait runs in a command, off the update loop, so the surface stays
// live.
func (m Model) bindLaunch(msg launchedMsg) tea.Cmd {
	c, l := m.core, msg.launch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*core.BindWait)
		defer cancel()
		inst, ok, err := c.Bind(ctx, msg.project, l.Target, msg.before, core.BindWait)
		if !ok {
			return actedMsg{err: err}
		}
		return actedMsg{err: err, bind: &binding{project: l.Project, target: l.Target, ref: inst.Ref}}
	}
}

// action runs the configured action bound to the key, if any, against the
// selected project. The terminal is handed to the command while it runs, and
// the command is resolved by the same rules `revier run` uses.
func (m Model) action(msg tea.KeyMsg) (tea.Cmd, bool) {
	c, ok := pressed(msg)
	if !ok {
		return nil, false
	}
	for _, act := range m.actions {
		if actionChord(act) != c {
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
		argv, dir, err := m.core.ActionCommand(p, act.Name, act.Run)
		if err != nil {
			slog.Error("action", "project", p.Name, "action", act.Name, "err", err)
			return func() tea.Msg { return actedMsg{err: err} }, true
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = dir
		project := p.Name
		start := time.Now()
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			logging.Op("action", start, err, "project", project, "action", act.Name, "argv", argv)
			// An action may open anything; the window that appears next is
			// the project's (claim-on-appear).
			return actedMsg{err: err, launch: &state.Launch{Project: project, At: time.Now()}}
		}), true
	}
	return nil, false
}

// anyWorking reports an agent in a turn anywhere on the surface: every row
// that counts one draws the spinner, and so does the pane.
func (m Model) anyWorking() bool {
	for _, v := range m.views {
		if working(v) {
			return true
		}
	}
	return false
}

// working reports an agent of the project in a turn.
func working(v revier.ProjectView) bool {
	for _, a := range v.Agents {
		if a.State.Status == revier.StatusRunning {
			return true
		}
	}
	return false
}

// redrawSpin redraws what a spinner frame changes, and nothing else: the rows while
// one on the screen has a working agent, and the pane while its project has
// one. A frame comes eight times a second, and a working agent behind the
// filter or a dialog would otherwise rebuild the whole surface for no glyph.
func (m *Model) redrawSpin() {
	if m.dialog.hasRows() {
		for _, item := range m.bodyList().VisibleItems() {
			if row, ok := item.(tableRow); ok && working(row.rowView()) {
				m.syncBody()
				break
			}
		}
	}
	if v, ok := m.selected(); ok && m.dialog == dialogNone && working(v) {
		m.syncDetail()
	}
}

// spun is the theme with the working glyph at the current spinner frame, for
// the rows and the pane.
func (m Model) spun() theme.Theme {
	th := m.theme
	if len(th.Spinner) > 0 {
		th.Glyphs.Working = th.Spinner[m.frame%len(th.Spinner)]
	}
	return th
}
