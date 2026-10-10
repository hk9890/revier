// Package tui is the one surface: every project with its agent state, sorted
// so the ones needing attention come first, and beside it a pane with the
// project's targets, attached instances and agents, and what one of its agents
// said last. Tab moves the cursor between the projects and the agents, and
// alt+t to the targets. Enter activates. It is the picker and the monitor at
// once (decisions.md D8, D105, D107). The key that opens it puts a second list
// in the first one's place: every agent of every project, with the terminal of
// the one under the cursor mirrored beside it (D110, D111).
//
// Every project, target and agent it draws comes from core.SurveyLocal,
// refreshed on a timer that never overlaps itself, with what core.AskRemotes
// last brought from the linked hosts laid over it (D114), and every action
// goes through the same core paths the CLI commands use. The two reads
// outside that path are core.Details, what an agent said last, which the
// surface asks for on its own and no survey carries (D106), and core.Screen,
// the mirror's.
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

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/ledger"
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

// dialog is a screen standing over the surface: the link dialog (decisions.md
// D45), the new-project field, the sessions screen, the config screen, the
// help screen, the shutdown wizard, or the project screen. A screen's step is
// the screen's own. dialogNone is the surface itself, which is where it is
// nearly always.
type dialog int

const (
	dialogNone dialog = iota
	dialogLink
	dialogNew
	dialogConfig
	dialogHelp
	dialogSessions
	dialogShutdown
	dialogProject
)

// hasRows reports a screen whose body is list rows: what a click selects and
// the wheel moves through. Every other screen stands over the project list
// with text of its own, and a press must not reach a row nobody can see.
func (m Model) hasRows() bool {
	switch m.dialog {
	case dialogNone:
		return true
	case dialogSessions:
		return !m.sessions.naming
	case dialogLink:
		return m.link.list() != nil
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
	usable    []map[string]any // shared without the ones config.toml's rules refuse, as a project file gets them
	refresh   time.Duration
	now       func() time.Time // the clock the double-click window is measured on; a test sets it
	theme     theme.Theme
	frame     int  // the spinner frame a working agent shows
	spinning  bool // whether a spin tick is out, so a survey starts no second one

	views    []revier.ProjectView // attention first, then config order
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
	tlines    []int              // the pane line each target row is on, for the cursor and a click
	acursor   int                // the agent row the pane's cursor is on
	afilter   string             // the query over the agent rows
	ainput    textinput.Model    // the agent query, with its own cursor
	alines    []lineSpan         // the pane lines each agent row takes, for the cursor and a click
	afield    int                // the pane line the agent query is on, -1 with no Agents section
	before    revier.ProjectName // the project the cursor was on when the query began, for when it is cleared
	confirm   revier.ProjectName // the project a delete is waiting on an answer for
	ctarget   revier.TargetName  // the target of confirm whose entry the delete removes, empty for the file
	dialog    dialog             // the screen standing over the surface
	shut      shutdown           // the shutdown wizard, while it is up
	width     int
	height    int

	// The project list. Cursor, paging and filtering are the component's;
	// the order of the rows a query leaves is ranked's, and what a row looks
	// like is the delegate's.
	plist    list.Model
	filter   string // the query, held here so a refresh can re-apply it
	keys     keyMap
	help     help.Model
	detail   viewport.Model
	shown    revier.ProjectName               // the project the pane holds, so a new one starts at its top
	tkeys    map[core.Chord]revier.TargetName // press to target name, over every project
	start    revier.ProjectName               // the project to open on, from the working directory
	placing  bool                             // the cursor was put on the starting project; the next body sync places the list around it
	lookup   StartLookup                      // the project to open on when start is none, asked once the surface shows
	popup    bool                             // the surface is the popup: Esc hides it, and the next press raises it
	hidden   bool                             // the popup is off the screen; nothing surveys until it is raised
	idle     bool                             // the survey chain ended while hidden; the raise starts it again
	local    core.Report                      // the last survey of this machine, with no host's answer laid over
	answers  core.RemoteAnswers               // what the linked hosts said last
	polling  []string                         // the linked hosts that are being asked
	onTop    bool                             // the cursor is on the top row by nobody's choice, and stays on it until every linked host has answered
	input    textinput.Model                  // the filter query, with its own cursor
	over     hovered                          // what the pointer is on
	cell     *pointerCell                     // where the pointer last was, nil before it moved
	body     viewport.Model                   // the scrolling window over the list
	last     click                            // the last click on a row, for telling a double click
	pclick   paneClick                        // the last click on a pane row, the same for the pane's targets and agents
	aasked   revier.ProjectName               // the project whose agents' details are out, empty with none
	aseq     int                              // the number of the last ask for details
	atook    int                              // the number of the ask whose answer the pane holds
	adetails map[agentKey]revier.AgentDetail  // what each agent of the project shown said last
	achosen  agentKey                         // the agent the user last put the cursor on, zero to let the pane choose
	amessage setMessage                       // the message the pane last set, as it set it
	barMore  bool                             // the bar shows the buttons a narrow terminal has no room for beside the first ones
	press    *press                           // where the left button went down, while it is down
	sel      selection                        // the box a drag is selecting
	copied   int                              // the characters the last selection copied, shown until the next press
	// tdeclared is every chord a target of any project declares as itself.
	// It comes from targetKeys with the vocabulary, because the footer asks
	// it for every target of every row it draws.
	tdeclared map[core.Chord]bool

	targets []revier.Target // the shared targets, typed, as config.toml holds them

	// The screens. Each is a struct of its own (screen.go): the root routes a
	// press to the one that is up and applies the result it hands back.
	link     linkScreen     // the link dialog (link.go)
	create   createScreen   // the new-project screen (newproject.go)
	sessions sessionsScreen // the sessions screen, and its save or restore (sessions.go)
	config   configScreen   // the config screen (configscreen.go)
	proj     projectScreen  // the project screen (projectscreen.go)
	agents   agentList      // the agent list, the surface's second list (agentlist.go)
}

// New builds the surface over prepared projects. stateRoot is where revier's
// state lives: the saved sessions are under it, and a core handed in with no
// ledger keeps its state there.
func New(c *core.Core, projects []core.Project, stateRoot string, cfg *config.Config, refresh time.Duration, th theme.Theme, start revier.ProjectName) Model {
	if c.Ledger == nil {
		c.Ledger = ledger.File{Root: stateRoot}
	}
	actions := cfg.Actions
	keys := newKeyMap(actions)
	m := Model{
		core: c, stateRoot: stateRoot, actions: actions,
		refresh: refresh, now: time.Now, theme: th, width: 80, height: 24,
		plist: newProjectList(th),
		link:  newLinkScreen(th), sessions: newSessionsScreen(th),
		keys: keys, help: newHelp(th), detail: newDetail(th),
		start: start, input: newPrompt(th, projectPlaceholder),
		ainput: newPrompt(th, agentPlaceholder), afield: -1,
		agents: newAgentScreen(th),
		create: newCreateScreen(th),
		body:   newBody(),
		config: newConfigScreen(th, cfg),
		proj:   newProjectScreen(th),
	}
	// The files go in through the one function that reads them, so what is
	// derived from them is derived once and the first frame holds the same
	// thing a reload leaves behind.
	m.setFiles(cfg.Targets, projects)
	// The first survey matches through the bindings too. Left to the survey's
	// own answer to fill in, they reach only the second one, a refresh later.
	m.takeState()
	// The project field has the cursor from the start: the surface filters as
	// you type, so it is where a keystroke lands. Init focuses it again for
	// the blink command; this is what makes it accept keys at all.
	_ = m.input.Focus()
	m.layout()
	// The first frame lists every project from its file, before any host
	// has answered: the survey lists every host here, and the names are
	// what the surface is opened for. The first survey lays
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
//
// The two shared lists are both derived here, once. The config screen edits
// config.toml as it is and needs every entry; a project file gets only the
// entries config.toml's rules do not refuse, and deriving those costs a TOML
// round trip per target, which is not a thing to do while drawing a frame.
func (m *Model) setFiles(shared []map[string]any, projects []core.Project) {
	m.shared = shared
	m.usable = config.Usable(shared)
	m.targets = config.ListTargets(shared)
	m.projects = projects
	m.setKeys()
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
	m.views = m.sorted(append(m.known(m.views), m.core.Unsurveyed(added)...))
	m.reload()
}

// surveyMsg is one survey's answer.
type surveyMsg struct {
	report core.Report
	err    error
}

// remotesMsg is what the hosts asked said, each about its projects.
type remotesMsg struct {
	hosts   []string
	answers core.RemoteAnswers
}

type tickMsg struct{}

// spinMsg advances the working spinner. It runs on its own timer, because
// the survey's is seconds long and a spinner that fast does not read as one.
type spinMsg struct{}

const spinInterval = 120 * time.Millisecond

func spin() tea.Cmd {
	return tea.Tick(spinInterval, func(time.Time) tea.Msg { return spinMsg{} })
}

// actedMsg follows a press, a focus, or an action; the next survey shows the
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

// localHostWait bounds one round of host calls - a survey, one focus, or the
// probe of a runtime host - so a hung wctl or kitty socket costs one refresh
// and not the surface.
const localHostWait = 10 * time.Second

// Survey is one refresh of what this machine answers by itself: one bulk
// listing per host here, matched locally. The linked hosts are AskRemotes's,
// so a host that is slow or gone holds no row here up (decisions.md D114). It
// is a command so the terminal stays responsive while hosts answer, and it
// schedules nothing itself, so two surveys never run at once.
func (m Model) Survey() tea.Cmd {
	c, projects := m.core, m.projects
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		defer cancel()
		report, err := c.SurveyLocal(ctx, projects)
		if err == nil {
			// The agents here. A link's are recorded when its host answers.
			events.Sessions(report.Views)
		}
		return surveyMsg{report: report, err: err}
	}
}

// AskRemotes asks the hosts named about their projects, or every linked host
// when none is named. The surface asks each host by itself, one round at a
// time: the survey that answers while a host has no round running starts its
// next, so a host is asked as often as it answers, never faster than the
// refresh, and a host that is slow holds no other host's rows up.
func (m Model) AskRemotes(hosts ...string) tea.Cmd {
	if len(hosts) == 0 {
		hosts = m.linkedHosts()
	}
	var links []core.Project
	for _, p := range m.projects {
		if p.Remote != nil && slices.Contains(hosts, p.Remote.Host) {
			links = append(links, p)
		}
	}
	c, local := m.core, m.local
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		defer cancel()
		answers := c.AskRemotes(ctx, links)
		events.Sessions(c.Lay(local, answers).Views)
		return remotesMsg{hosts: hosts, answers: answers}
	}
}

// linkedHosts is every host a project lives on, each once, in the projects'
// order.
func (m Model) linkedHosts() []string {
	var hosts []string
	for _, p := range m.projects {
		if p.Remote != nil && !slices.Contains(hosts, p.Remote.Host) {
			hosts = append(hosts, p.Remote.Host)
		}
	}
	return hosts
}

// answered reports a view that says all a survey can say of its project: a
// link's is the files' alone in what its host owns - the checkout and the
// agents - until that host has been asked.
func (m Model) answered(v revier.ProjectView) bool {
	return v.Project.Remote == nil || m.answers.Has(v.Project)
}

// show puts the last local survey on the screen with the hosts' last answers
// laid over it. Either half arrives by itself, and the rows are always both.
func (m *Model) show() {
	views := m.core.Lay(m.local, m.answers).Views
	m.views = m.sorted(m.known(m.uncovered(m.core.Shown(views))))
	m.reload()
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
	// The mirror is asked for before the pane is drawn: an agent that came
	// under the cursor is drawn with its own screen pending, and not over the
	// screen of the agent the cursor left.
	var read tea.Cmd
	if mm.mirroring() {
		read = mm.agents.askMirror(mm.core, msg)
	}
	mm.syncDetail()
	mm.syncBody()
	cmd = tea.Batch(cmd, mm.askDetails(msg), read)
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
// logged: a press, a bind, a focus or an action. Every other error set on m.err -
// a form refused, a file that did not load, a clone - is logged by Update. A
// failed survey is shown from m.surveyErr and logged by core.SurveyLocal.
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
	case detailsMsg:
		m.took(msg)
		return m, nil
	case mirroredMsg:
		m.agents.tookScreen(msg, m.paneCols()-paneChrome)
		return m, nil
	case mirrorTickMsg:
		// The tick ends with the mirror off the screen, and askMirror starts
		// it again when the mirror comes back; the read it is for is
		// askMirror's too.
		if !m.mirroring() {
			m.agents.mirror.ticking = false
			return m, nil
		}
		return m, mirrorTick()
	case surveyMsg:
		m.surveyErr = msg.err
		if msg.err == nil {
			// A host that could not list degraded the survey rather than
			// failing it: the view stands, and the footer says which host.
			m.surveyErr = msg.report.HostErr()
			// What the survey settles in state - prune, claim, expire - is
			// the core's, on the state on disk: the launch may have been
			// written by another process.
			m.keep(m.core.Settle(msg.report, m.projects, time.Now()))
			m.local, m.surveyed = msg.report, true
			m.show()
		}
		// Hidden, nobody reads the answer, and the chain ends here, and with
		// it the rounds of the linked hosts that a survey starts: a round
		// trip to each of them a second is for nothing.
		if m.hidden {
			m.idle = true
			return m, nil
		}
		var cmds []tea.Cmd
		for _, host := range m.linkedHosts() {
			if !slices.Contains(m.polling, host) {
				// A new slice: the rounds that run hold the old one.
				m.polling = append(slices.Clone(m.polling), host)
				cmds = append(cmds, m.AskRemotes(host))
			}
		}
		if !m.spinning && m.anyWorking() {
			m.spinning = true
			cmds = append(cmds, spin())
		}
		return m, tea.Batch(append(cmds, tick(m.refresh))...)
	case remotesMsg:
		m.polling = slices.DeleteFunc(slices.Clone(m.polling), func(host string) bool { return slices.Contains(msg.hosts, host) })
		m.answers = m.answers.With(msg.hosts, msg.answers)
		if !m.surveyed {
			return m, nil
		}
		m.show()
		if !m.spinning && !m.hidden && m.anyWorking() {
			m.spinning = true
			return m, spin()
		}
		return m, nil
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
			m.start, m.onTop = msg.name, false
			m.placing = m.selectName(msg.name)
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
		// The surface opens on the projects, a raise included: the press that
		// raises it is the one that opened it (decisions.md D110).
		m.hidden, m.agents.shown = false, false
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
	case assistedMsg:
		m.err = msg.err
		return m, reloadFiles
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
	case closePlannedMsg:
		return m.closePlanned(msg)
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
	m.ainput, cmds[1] = m.ainput.Update(msg)
	m.agents.query, cmds[2] = m.agents.query.Update(msg)
	return m, tea.Batch(cmds[:]...)
}

// keep holds the parts of state the surface reads between refreshes. The
// surface writes no state itself: the core and internal/app write it through
// the ledger. A state file that cannot be read is the state the ledger read
// last, and one that cannot be written costs the next keypress a fallback,
// not the surface, so the ledger logs it and nothing is shown.
func (m *Model) keep(st *state.State) {
	m.attached, m.bound, m.pending = st.Attached, st.Bound, st.Launch
}

// takeState takes the ledger's state into the surface: what an activation
// wrote, before the next survey would.
func (m *Model) takeState() {
	m.keep(m.core.Ledger.State())
}

// sorted puts projects needing attention first, then the running ones, and
// otherwise keeps config order, so rows move only when a project starts, stops
// or an agent's state changes. Running above stopped is the picker's order
// (os_list_json.py sorts on it too): with ninety projects the handful that are
// open are what a search is almost always for.
func (m Model) sorted(views []revier.ProjectView) []revier.ProjectView {
	out := make([]revier.ProjectView, len(views))
	copy(out, views)
	sort.SliceStable(out, func(i, j int) bool {
		return m.need(out[i]) < m.need(out[j])
	})
	return out
}

// need is the group sorted puts a project in, the one that needs the user
// most first. A query keeps its rows in these groups too (ranked).
func (m Model) need(v revier.ProjectView) int {
	switch {
	case v.Attention():
		return 0
	case m.heldHere(v):
		return 1
	default:
		return 2
	}
}

// key routes a press. Everything the surface owns is matched here, and only
// what is left over reaches the list - which is why every printable rune is a
// filter character and never a list command: the surface filters as you type,
// so no letter can be a shortcut.
func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm != "" {
		return m.confirmDelete(msg)
	}
	switch m.dialog {
	case dialogNew:
		return m.createKey(msg)
	case dialogLink:
		return m.linkKey(msg)
	case dialogConfig:
		return m.configKey(msg)
	case dialogProject:
		return m.projectKey(msg)
	case dialogHelp:
		return m.helpScreenKey(msg)
	case dialogSessions:
		return m.sessionsKey(msg)
	case dialogShutdown:
		return m.shutdownKey(msg)
	}
	if m.agents.shown {
		return m.agentsKey(msg)
	}
	m.err = nil
	if by, ok := m.keys.move(msg, m.page); ok {
		m.moveCursor(by)
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case switches(msg):
		return m.switchList()
	case key.Matches(msg, m.keys.Back):
		switch {
		case m.field() != nil && m.field().Value() != "":
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
	case key.Matches(msg, m.keys.Close) && atEnd(m.field()):
		return m.askClose()
	case key.Matches(msg, m.keys.Delete):
		return m.askDelete()
	case key.Matches(msg, m.keys.Targets):
		if len(m.targetRows()) > 0 {
			return m.focusOn(focusTargets), nil
		}
		return m, nil
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
	// The targets have no query: a letter typed there filters nothing.
	if m.focus != focusTargets && promptKey(msg) {
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
	return projectNamed(m.projects, name)
}

// targetRow is one row of the pane's Targets section: a declared target, or an
// attached instance, which has a ref and no name.
type targetRow struct {
	target   revier.TargetView
	attached revier.TargetRef
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
		m.pickAgent(m.acursor + by)
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

// heldHere reports anything on this machine holding the project, as every surface
// over this machine's projects draws it (decisions.md D104). It is
// ProjectView.Held plus the instances attached by hand: the survey is asked
// with no attachments, because the surface reads those from state, where a
// claim writes one between surveys so a claimed window shows at once rather
// than a refresh later.
func (m Model) heldHere(v revier.ProjectView) bool {
	return heldHere(m.attached, v)
}

func heldHere(attached map[revier.ProjectName][]revier.TargetRef, v revier.ProjectView) bool {
	return v.Held() || len(attached[v.Project.Name]) > 0
}

// targetRows is the pane's Targets section: every target, then every
// attached instance.
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
	return all
}

// enter acts on the row under the cursor, by key or by double click.
func (m Model) enter() (Model, tea.Cmd) {
	if m.agents.shown {
		cmd := m.agents.goSelected(m.surface())
		return m, cmd
	}
	return opened(m.act())
}

// opened ends the search once a press has something to run: Enter, a double
// click, a click on a pane row, a target key or an action. The query was the
// way to what now opens, so the surface comes back with empty fields
// (decisions.md D105). The search ends also when the run fails later; the
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
			ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
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
	c := m.core
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*core.BindWait)
		defer cancel()
		_, _, err := c.ActivateWaiting(ctx, p, name, nil)
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
		// The plan writes the action's launch, which makes its project the
		// current one, as a command makes it: a desktop key pressed next on a
		// window no rule names falls back to it.
		run, err := app.PlanAction(m.core, p, act.Name, act.Run)
		if err != nil {
			slog.Error("action", "project", p.Name, "action", act.Name, "err", err)
			return func() tea.Msg { return actedMsg{err: err} }, true
		}
		cmd := exec.Command(run.Argv[0], run.Argv[1:]...)
		cmd.Dir = run.Dir
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			run.Done(err)
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
	// Every working agent on the agent list is a row with the spinner, and
	// the pane's head carries it for the one under the cursor.
	if m.agents.shown && m.dialog == dialogNone {
		for _, item := range m.agents.list.VisibleItems() {
			if it, ok := item.(agentItem); ok && it.agent.State.Status == revier.StatusRunning {
				m.syncBody()
				break
			}
		}
		if it, ok := m.agents.selected(); ok && it.agent.State.Status == revier.StatusRunning {
			m.syncDetail()
		}
		return
	}
	if m.hasRows() {
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
