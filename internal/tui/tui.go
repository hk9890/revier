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
// process: successive surveys are diffed, and a window that opens shortly
// after a launch is claimed within two refresh intervals. State is re-read on
// every refresh, because the launch that starts the clock is written by
// another process.
package tui

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"slices"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

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
// - which host, which of its projects (decisions.md D45), and the link's name -
// the new-project field, the sessions screen and its name step, the config
// screen, or the project screen. dialogNone is the surface itself, which is
// where it is nearly always.
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
	dialogProject
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
// config screens are about no project, and the project screen is the
// project's own, so they take the whole width.
func (d dialog) hasPane() bool {
	return d != dialogHelp && d != dialogConfig && d != dialogProject
}

// Model is the bubbletea model. Construct it with New.
type Model struct {
	core      *core.Core
	projects  []core.Project
	stateRoot string
	actions   []config.Action
	shared    []map[string]any // every [[target]] of config.toml, refused ones included, as the config screen edits them
	refresh   time.Duration
	now       func() time.Time // the clock the double-click window is measured on; a test sets it
	theme     theme.Theme
	frame     int  // the spinner frame a working agent shows
	spinning  bool // whether a spin tick is out, so a survey starts no second one

	views    []revier.ProjectView // attention first, then config order
	windows  []revier.Instance    // the window host's listing at the last survey it answered
	listed   bool                 // whether the window host has answered once: windows is a listing, and a diff against it means something
	surveyed bool                 // whether a survey has answered: the counts are real
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
	rfilter   string             // the query over the host's projects
	rbefore   revier.ProjectName // the host's project the cursor was on when its query began
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
	lookup StartLookup                      // the project to open on when start is none, asked once the surface shows
	popup  bool                             // the surface is the popup: Esc hides it, and the next press raises it
	hidden bool                             // the popup is off the screen; nothing surveys until it is raised
	idle   bool                             // the survey chain ended while hidden; the raise starts it again
	trees  map[string]treeEntry             // cached directory listings, by project path
	input  textinput.Model                  // the filter query, with its own cursor
	path   textinput.Model                  // the directory field of the new-project screen
	nstep  newStep                          // the new-project screen's step
	nrows  []string                         // what the new-project screen lists under the field
	nrow   int                              // the chosen one of nrows, -1 for none
	ndir   string                           // the folder the new-project screen asks to create
	lname  textinput.Model                  // the name field of the link dialog's last step
	rinput textinput.Model                  // the query over the link dialog's second step
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

	// The project screen. Its target form is tform.
	proj  revier.ProjectName // the project the screen edits
	ptext config.ProjectText // its file as written
	prow  int                // the row the screen's cursor is on
	pedit textinput.Model    // a field's value, while it is typed
}

// New builds the surface over prepared projects. stateRoot is where revier's
// state lives: attached instances are read from it on every refresh and
// claims are written to it.
func New(c *core.Core, projects []core.Project, stateRoot string, cfg *config.Config, refresh time.Duration, th theme.Theme, start revier.ProjectName) Model {
	actions := cfg.Actions
	keys := newKeyMap(actions)
	m := Model{
		core: c, projects: projects, stateRoot: stateRoot, actions: actions, shared: cfg.Targets,
		refresh: refresh, now: time.Now, theme: th, width: 80, height: 24,
		plist: newProjectList(th),
		hlist: newHostList(th), rlist: newRemoteList(th), slist: newSessionList(th),
		keys: keys, help: newHelp(th), detail: newDetail(th),
		tkeys: targetKeys(projects, keys), start: start, input: newPrompt(th, projectPlaceholder),
		tinput: newPrompt(th, targetPlaceholder), ainput: newPrompt(th, agentPlaceholder), afield: -1,
		path: newPathInput(th), lname: newLinkNameInput(th), sname: newSessionNameInput(th),
		rinput: newPrompt(th, ""),
		body:   newBody(),
		ui:     cfg.UI, runtime: cfg.Hosts.Runtime, chord: newChordInput(th),
		pedit: newFieldInput(th),
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
	m.targets = config.ListTargets(cfg.Targets)
	m.layout()
	// The first frame lists every project from its file, before any host
	// has answered: the survey is a round trip to every linked host, and
	// the names are what the surface is opened for. The first survey lays
	// the marks and the counts over the same rows.
	m.views = c.Unsurveyed(projects)
	m.reload()
	return m
}

// StartLookup finds the project to open on when the working directory is in
// none: the one of the focused window, else the one remembered. It lists
// every host, so it runs after the first frame, not before it.
type StartLookup func(ctx context.Context) (revier.ProjectName, bool)

// WithStart sets the lookup Init runs. Its answer moves the cursor only
// while the user has not: a cursor that jumps under a keystroke is worse
// than one that starts on the top row.
func (m Model) WithStart(lookup StartLookup) Model {
	m.lookup = lookup
	return m
}

// WithPopup makes the surface the popup: Esc on the list hides its window
// instead of exiting, nothing surveys while it is hidden, and the raise
// surveys once and resumes (decisions.md D86).
func (m Model) WithPopup() Model {
	m.popup = true
	return m
}

// startMsg is the lookup's answer.
type startMsg struct {
	name revier.ProjectName
	ok   bool
}

// hiddenMsg follows the hide of the popup's window.
type hiddenMsg struct{ err error }

// reloadedMsg is the project files read again, on the raise of the popup:
// what `revier new`, a link or an edit changed while it was hidden.
type reloadedMsg struct {
	shared   []map[string]any
	actions  []config.Action
	projects []core.Project
	err      error
}

// reloadFiles reads the configuration again. Every press used to load it,
// since the popup exited on Esc; a popup that hides must read it on the
// raise, or a project added from a terminal is missing until it quits.
func reloadFiles() tea.Msg {
	var msg reloadedMsg
	msg.err = withConfigRoot(func(root string) error {
		cfg, projects, err := config.Load(root)
		if err != nil {
			return err
		}
		msg.shared, msg.actions, msg.projects = cfg.Targets, cfg.Actions, projects
		return nil
	})
	return msg
}

// setFiles takes the project files as read again: the shared targets for the
// config screen, and every project loaded with them, for the surface.
func (m *Model) setFiles(shared []map[string]any, projects []core.Project) {
	m.shared = shared
	m.targets = config.ListTargets(shared)
	m.projects = projects
	m.tkeys = targetKeys(m.projects, m.keys)
}

// reloadViews puts the projects as read again on the screen before the
// survey answers, as the first frame does: a project removed goes, one
// added gets the row its file alone gives, and the cursor stays on its
// project by name. A row that stayed keeps what the last survey found until
// the next one answers.
func (m *Model) reloadViews() {
	have := make(map[revier.ProjectName]bool, len(m.views))
	for _, v := range m.views {
		have[v.Project.Name] = true
	}
	var added []core.Project
	for _, p := range m.projects {
		if !have[p.Name] {
			added = append(added, p)
		}
	}
	m.views = sorted(append(m.known(m.views), m.core.Unsurveyed(added)...))
	m.reload()
}

// surveyMsg is one survey's answer, and the state it started from: what it
// may prune (state.Prune).
type surveyMsg struct {
	report core.Report
	before *state.State
	err    error
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
// result. An activation wrote what it learned through the ledger, and the
// surface takes state in on the update loop.
type actedMsg struct{ err error }

// Init surveys immediately; the timer starts once the first survey answers.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.Survey(), m.input.Focus(), m.lookupStart())
}

// lookupStart asks the lookup where to open, when the working directory did
// not say. Nil without a lookup, or with a start already.
func (m Model) lookupStart() tea.Cmd {
	lookup := m.lookup
	if lookup == nil || m.start != "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		name, ok := lookup(ctx)
		return startMsg{name: name, ok: ok}
	}
}

// hide takes the popup's window off the screen.
func (m Model) hide() tea.Cmd {
	c := m.core
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return hiddenMsg{err: c.HidePopup(ctx)}
	}
}

// leave is Esc on the list with no query: the popup hides, any other
// surface quits.
func (m Model) leave() tea.Cmd {
	if m.popup {
		return m.hide()
	}
	return tea.Quit
}

// leaveWord names what leave does, for the help.
func (m Model) leaveWord() string {
	if m.popup {
		return "hide the popup"
	}
	return "quit"
}

// hostTimeout bounds one round of host calls - a survey, one focus, or the
// probe of a runtime host - so a hung wctl or kitty socket costs one refresh
// and not the surface.
const hostTimeout = 10 * time.Second

// Survey is one refresh: one bulk listing per host, matched locally. It is a
// command so the terminal stays responsive while hosts answer, and it
// schedules nothing itself, so two surveys never run at once.
func (m Model) Survey() tea.Cmd {
	c, projects, bound, root := m.core, m.projects, m.bound, m.stateRoot
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), hostTimeout)
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
			// A host that could not list degraded the survey rather than
			// failing it: the view stands, and the footer says which host.
			m.surveyErr = msg.report.HostErr()
			m.claimByPolling(msg.report, msg.before)
			m.views = sorted(m.known(m.uncovered(msg.report.Views)))
			m.surveyed = true
			// The window listing stands only when the window host answered:
			// one it is missing from is not an empty one, and the next diff
			// would take every window as new and claim one of them.
			if m.core.Window == nil || slices.Contains(msg.report.Hosts, m.core.Window.Name()) {
				m.windows, m.listed = msg.report.Windows, true
			}
			m.reload()
		}
		// Hidden, nobody reads the answer, and the chain ends here: a
		// survey a second is a round trip to every linked host for nothing.
		if m.hidden {
			m.idle = true
			return m, nil
		}
		if !m.spinning && m.anyWorking() {
			m.spinning = true
			return m, tea.Batch(tick(m.refresh), spin())
		}
		return m, tick(m.refresh)
	case spinMsg:
		// The spinner stops when nothing works, so an idle surface does not
		// redraw, and while hidden, where nobody sees it; the next survey
		// that finds a working agent starts it again.
		if m.hidden || !m.anyWorking() {
			m.spinning = false
			return m, nil
		}
		m.frame++
		return m, spin()
	case tickMsg:
		if m.hidden {
			m.idle = true
			return m, nil
		}
		return m, m.Survey()
	case startMsg:
		// The user's own cursor wins: a keystroke, a query or a dialog in
		// the time the lookup took means they are already somewhere.
		if msg.ok && m.start == "" && m.filter == "" && m.dialog == dialogNone && m.focus == focusList && m.plist.Index() == 0 {
			m.start = msg.name
			m.selectName(msg.name)
		}
		return m, nil
	case hiddenMsg:
		if msg.err != nil {
			// The popup cannot leave the screen, so it leaves the way it
			// did before it could hide.
			slog.Warn("popup hide, quitting instead", "err", msg.err)
			return m, tea.Quit
		}
		m.hidden = true
		return m, nil
	case tea.FocusMsg:
		// The raise: the files again, then one survey, then the chain as
		// before. A chain still running while hidden goes on by itself.
		if !m.hidden {
			return m, nil
		}
		m.hidden = false
		return m, reloadFiles
	case reloadedMsg:
		if msg.err != nil {
			// The old list stands: a file broken while hidden must not
			// empty the popup.
			slog.Warn("reload on raise", "err", msg.err)
		} else {
			m.setFiles(msg.shared, msg.projects)
			m.setActions(msg.actions)
			m.reloadViews()
		}
		if m.idle {
			m.idle = false
			return m, m.Survey()
		}
		return m, nil
	case actedMsg:
		// The timer's next survey shows the result. Starting one here would
		// add a second survey-tick chain that never ends. An activation wrote
		// where it landed through the ledger, so the surface takes state in.
		m.err = msg.err
		m.takeState()
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
	c, prev, listed, projects := m.core, m.windows, m.listed, m.projects
	m.updateState(func(st *state.State) bool {
		return c.Settle(st, before, report, prev, listed, projects, time.Now())
	})
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

// apply writes an action's launch, which makes its project the current one,
// as a CLI command makes it: a desktop key pressed next on a window no rule
// names falls back to it. It is written on the update loop, so it never
// races the claim path.
func (m *Model) apply(l state.Launch) {
	m.updateState(func(st *state.State) bool {
		st.Launched(l.Project, l.Target, l.At)
		return true
	})
}

// takeState takes the state on disk into the surface: what an activation
// wrote through the ledger, before the next survey would.
func (m *Model) takeState() {
	if st, err := loadState(m.stateRoot); err == nil && st != nil {
		m.keep(st)
	}
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
	case dialogProject:
		return m.projectKey(msg)
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
	if by, ok := m.keys.move(msg, m.page); ok {
		m.moveCursor(by)
		return m, nil
	}
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
			return m, m.leave()
		}
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.enter()
	case key.Matches(msg, m.keys.Next):
		return m.step(1)
	case key.Matches(msg, m.keys.Prev):
		return m.step(-1)
	case key.Matches(msg, m.keys.Edit):
		return m.openProject()
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

// allRows is a move past either end of any section, which the cursor stops
// at: what home and end move by.
const allRows = 1 << 30

// move is how many rows a movement key moves a cursor. page is how many rows
// of the section are on the screen from the cursor's, down for 1 and up for
// -1, and is asked only for a page key.
func (k keyMap) move(msg tea.KeyMsg, page func(dir int) int) (int, bool) {
	switch {
	case key.Matches(msg, k.Up):
		return -1, true
	case key.Matches(msg, k.Down):
		return 1, true
	case key.Matches(msg, k.PageUp):
		return -page(-1), true
	case key.Matches(msg, k.PageDown):
		return page(1), true
	case key.Matches(msg, k.Home):
		return -allRows, true
	case key.Matches(msg, k.End):
		return allRows, true
	}
	return 0, false
}

// moveCursor moves the cursor of the section it is in by rows.
func (m *Model) moveCursor(by int) {
	switch m.focus {
	case focusTargets:
		m.tcursor = clampRow(m.tcursor+by, len(m.targetRows()))
	case focusAgents:
		m.acursor = clampRow(m.acursor+by, len(m.agentRows()))
	case focusList:
		moveRow(&m.plist, by)
	}
}

// moveRow moves a list's cursor by rows.
func moveRow(l *list.Model, by int) {
	l.Select(clampRow(l.Index()+by, len(l.VisibleItems())))
}

// clampRow keeps a cursor on one of n rows, stopping at the first and the
// last.
func clampRow(i, n int) int {
	return min(max(i, 0), max(n-1, 0))
}

// page is how many rows of the section the cursor is in are on the screen
// at once, down for 1 and up for -1: what page up and page down move by.
func (m Model) page(dir int) int {
	switch m.focus {
	case focusTargets:
		return max(m.detail.Height, 1) // a target is one line
	case focusAgents:
		return m.agentsOnPage(dir)
	}
	return m.listPage()
}

// listPage is how many rows of the list in view the body shows at once.
func (m Model) listPage() int {
	return max(m.body.Height/m.itemHeight(), 1)
}

// agentsOnPage is how many agent rows past the cursor fit on the pane, below
// it for 1 and above it for -1. An agent's row is as tall as its activity
// needs, so rows are not lines.
func (m Model) agentsOnPage(dir int) int {
	n, room := 0, m.detail.Height
	for i := m.acursor + dir; i >= 0 && i < len(m.alines); i += dir {
		room -= m.alines[i].end - m.alines[i].start
		if room < 0 {
			break
		}
		n++
	}
	return max(n, 1)
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
	// What Enter does is the core's decision, the one `revier open` makes
	// (decisions.md D90); the surface renders it. A project with no home has
	// nothing to open, so the cursor goes to what it does have, with the
	// reason in the footer.
	home, _ := p.Home()
	open, err := core.Open(p, func() (bool, error) {
		// The last survey's word. A home its host could not list is
		// neither running nor stopped, and the refusal says why.
		for _, tv := range v.Targets {
			if !tv.Attached && tv.Name == home.Name && tv.Unknown != "" {
				return false, errors.New(tv.Unknown)
			}
		}
		return v.Running, nil
	})
	if err != nil {
		m.err = err
		if errors.Is(err, core.ErrNoHome) {
			return m.focusOn(focusTargets), nil
		}
		return m, nil
	}
	if open == core.OpenClone {
		return m, m.clone(p, home.Name)
	}
	return m, m.goTarget(p, home.Name)
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
			ctx, cancel := context.WithTimeout(context.Background(), hostTimeout)
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

// goTarget is one activation, the whole run-or-raise as core.ActivateWaiting
// walks it against the state on disk: the launch is written before the wait
// for its window, so a second press - here or on a desktop key - finds it
// and does not launch again (decisions.md D21), and the ref it lands on is
// written after. The wait runs in the command, off the update loop, so the
// surface stays live. Enter on a row of the pane and a target key on the
// list are the same operation, so they are the same command.
func (m Model) goTarget(p core.Project, name revier.TargetName) tea.Cmd {
	c, root := m.core, m.stateRoot
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*core.BindWait)
		defer cancel()
		_, _, err := c.ActivateWaiting(ctx, p, name, nil, core.StateLedger{Root: root})
		return actedMsg{err: err}
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
		if act.Refused != nil || actionChord(act) != c {
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
		// An action may open anything; the window that appears next is the
		// project's (claim-on-appear). The launch is recorded now, as the
		// CLI records it, so core.ClaimWindow runs from the action's start
		// and not from its exit, which for an editor is hours later. A
		// remote project's action runs on its host and opens no window here.
		if p.Remote == nil {
			m.apply(state.Launch{Project: project, At: start})
		}
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			logging.Op("action", start, err, "project", project, "action", act.Name, "argv", argv)
			return actedMsg{err: err}
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
