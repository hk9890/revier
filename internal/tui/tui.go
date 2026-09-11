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
	"errors"
	"fmt"
	"io"
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
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
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
	theme     theme.Theme

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
	level     level
	current   revier.ProjectName // the project drilled into
	confirm   revier.ProjectName // the project a delete is waiting on an answer for
	width     int
	height    int

	// The two levels are two lists. Cursor, paging and fuzzy filtering are
	// the component's; what a row looks like is the delegate's.
	plist  list.Model
	tlist  list.Model
	filter string // typed at the project level, held here so a refresh can re-apply it
	keys   keyMap
	help   help.Model
	detail viewport.Model
	shown  revier.ProjectName               // the project the pane holds, so a new one starts at its top
	tkeys  map[core.Chord]revier.TargetName // press to target name, over every project
	start  revier.ProjectName               // the project to open on, from the working directory
	trees  map[string]treeEntry             // cached directory listings, by project path
	input  textinput.Model                  // the filter query, with its own cursor
	body   viewport.Model                   // the scrolling window over the level in view
	last   click                            // the last click on a row, for telling a double click
}

// New builds the surface over prepared projects. stateRoot is where revier's
// state lives: attached instances are read from it on every refresh and
// claims are written to it.
func New(c *core.Core, projects []core.Project, stateRoot string, actions []config.Action, refresh time.Duration, th theme.Theme, start revier.ProjectName) Model {
	keys := newKeyMap(actions)
	m := Model{
		core: c, projects: projects, stateRoot: stateRoot, actions: actions,
		refresh: refresh, theme: th, width: 80, height: 24,
		plist: newProjectList(th), tlist: newTargetList(th),
		keys: keys, help: newHelp(th), detail: newDetail(th),
		tkeys: targetKeys(projects, keys), start: start, input: newPrompt(th),
		body: newBody(),
	}
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
		before, _ := state.Load(root)
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
	mm.syncDetail()
	mm.syncBody()
	return mm, cmd
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
			m.views = sorted(m.known(msg.report.Views))
			m.windows, m.surveyed = msg.report.Windows, true
			m.reload()
		}
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
		m.apply(msg)
		return m, nil
	case launchedMsg:
		m.apply(actedMsg{launch: &msg.launch})
		return m, m.bindLaunch(msg)
	case editedMsg:
		m.err = m.reread(msg)
		return m, nil
	case clonedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, m.goTarget(msg.project, msg.home)
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	// A blink is the input's own timer message; nothing else reads it.
	in, cmd := m.input.Update(msg)
	m.input = in
	return m, cmd
}

// claimByPolling diffs the window listing against the previous survey's and
// settles the last launch with a window that appeared since: bound to its
// target, or attached to the project when an action launched. State is
// re-read because the launch was written by another process. The same pass
// drops attachments and bindings whose instances are gone, of those the
// survey started from: one written while it listed is the next survey's.
func (m *Model) claimByPolling(report core.Report, before *state.State) {
	st, err := state.Load(m.stateRoot)
	if err != nil {
		return
	}
	changed := st.Prune(report.Hosts, report.Instances, before)
	if l, ok := m.launch(st); ok && m.surveyed {
		now := time.Now()
		if claimed, ok := m.core.Claim(m.windows, report.Windows, l, now, m.projects); ok {
			settle(st, claimed)
			changed = true
		} else if !l.Pending(now) {
			st.Launch, changed = nil, true // expired
		}
	}
	if changed {
		_ = st.Save(m.stateRoot)
	}
	m.keep(st)
}

// claimByEvent is the same decision for a window a watching host reported.
func (m *Model) claimByEvent(inst revier.Instance) {
	st, err := state.Load(m.stateRoot)
	if err != nil {
		return
	}
	l, ok := m.launch(st)
	if !ok {
		return
	}
	claimed, ok := m.core.ClaimEvent(inst, l, time.Now(), m.projects)
	if !ok {
		return
	}
	settle(st, claimed)
	_ = st.Save(m.stateRoot)
	m.keep(st)
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

// settle writes a claim into state and consumes the launch.
func settle(st *state.State, c core.Claimed) {
	if c.Target != "" {
		st.Bind(st.Launch.Project, c.Target, c.Ref)
	} else {
		st.Attach(st.Launch.Project, c.Ref)
	}
	st.Launch = nil
}

// apply writes what an activation learned: a launch still coming up, or the
// ref a target landed on.
func (m *Model) apply(msg actedMsg) {
	if msg.launch == nil && msg.bind == nil {
		return
	}
	st, err := state.Load(m.stateRoot)
	if err != nil {
		return
	}
	// The project acted on becomes the current one, as a CLI command makes
	// it: a desktop key pressed next on a window no rule names falls back to
	// it.
	if msg.launch != nil {
		st.Launch = msg.launch
		st.Current = msg.launch.Project
	}
	if b := msg.bind; b != nil {
		st.Current = b.project
		st.Bind(b.project, b.target, b.ref)
		if st.Launch != nil && st.Launch.Project == b.project && st.Launch.Target == b.target {
			st.Launch = nil
		}
	}
	_ = st.Save(m.stateRoot)
	m.keep(st)
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
	m.err = nil
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		switch {
		case m.level == levelTargets:
			m.level = levelProjects
		case m.filter != "":
			m.setFilter("")
		default:
			return m, tea.Quit
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.list().CursorUp()
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.list().CursorDown()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.enter()
	case key.Matches(msg, m.keys.Targets) && m.level == levelProjects:
		return m.drill()
	case key.Matches(msg, m.keys.Edit) && m.level == levelProjects:
		return m.editFile()
	case key.Matches(msg, m.keys.Delete) && m.level == levelProjects:
		return m.askDelete()
	}
	if cmd, ok := m.action(msg); ok {
		return m, cmd
	}
	if m.level == levelProjects {
		if next, cmd, ok := m.targetKey(msg); ok {
			return next, cmd
		}
		if m.promptKey(msg) {
			return m.edit(msg)
		}
	}
	return m, nil
}

// list is the list of the level in view. It is a pointer because the cursor
// moves on it.
func (m *Model) list() *list.Model {
	if m.level == levelTargets {
		return &m.tlist
	}
	return &m.plist
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

// enter at the project level opens the project: its home target, the same
// run-or-raise `revier go home` does. Searching for a project is almost always
// to get to it, so the target list is the detour and gets the other key. A
// project with no home target has nothing to open, so it gets the list.
//
// A project whose directory is not on this machine is cloned first, when its
// file says from where, as `revier open` does.
func (m Model) enter() (tea.Model, tea.Cmd) {
	if m.level == levelProjects {
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
			if p.Host != "" {
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
		return m.drill()
	}
	it, ok := m.tlist.SelectedItem().(targetItem)
	if !ok {
		return m, nil
	}
	row := it.row
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
	return m, m.goTarget(p, row.target.Name)
}

// drill opens the target list of the project under the cursor.
func (m Model) drill() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.current = v.Project.Name
	m.level = levelTargets
	m.reloadTargets()
	m.tlist.Select(0)
	return m, nil
}

// goTarget is one activation: run-or-raise the target, and settle where it
// landed. Enter at the target level and a target key at the project level are
// the same operation, so they are the same command.
//
// A target still coming up from an earlier press, here or from a desktop key,
// is not launched again: the first press is waiting for its window
// (decisions.md D21). A window that has appeared by then is raised like any
// other.
func (m Model) goTarget(p core.Project, name revier.TargetName) tea.Cmd {
	c := m.core
	bound := m.bound[p.Name]
	pending := m.launchPending(p.Name, name)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), bindWait)
		defer cancel()
		if pending {
			if up, err := c.Running(ctx, p, name, bound); err != nil || !up {
				return actedMsg{err: err}
			}
		}
		res, err := c.Go(ctx, p, name, bound)
		if err != nil {
			return actedMsg{err: err}
		}
		if res.Launched && res.Ref.IsZero() {
			return launchedMsg{
				project: p,
				launch:  state.Launch{Project: p.Name, Target: res.Target, At: time.Now()},
				before:  res.Before,
			}
		}
		return actedMsg{bind: &binding{project: p.Name, target: res.Target, ref: res.Ref}}
	}
}

// launchPending reports whether a launch of the target is on record and still
// inside the time its window may take to appear.
func (m Model) launchPending(p revier.ProjectName, name revier.TargetName) bool {
	l := m.pending
	return l != nil && l.Project == p && l.Target == name && time.Since(l.At) < core.BindWindow
}

// bindLaunch waits for the window a detached launch produces and binds it. If
// it takes longer than the wait, the launch record lets a later refresh bind
// it.
func (m Model) bindLaunch(msg launchedMsg) tea.Cmd {
	c, l := m.core, msg.launch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*bindWait)
		defer cancel()
		inst, ok, err := c.Bind(ctx, msg.project, l.Target, msg.before, bindWait)
		if err != nil || !ok {
			return actedMsg{err: err}
		}
		return actedMsg{bind: &binding{project: l.Project, target: l.Target, ref: inst.Ref}}
	}
}

// bindWait is how long an activation waits for a launched window. The wait
// runs in a command, off the update loop, so the surface stays live.
const bindWait = 30 * time.Second

// action runs the configured action bound to the key, if any, against the
// selected project. The terminal is handed to the command while it runs, and
// the argv is rendered by the same rules `revier run` uses.
// actionArgv is what runs for an action on a project: the action rendered
// against the project, or, for a project on another machine, the ssh that
// runs the action there (decisions.md D40).
func (m Model) actionArgv(p core.Project, act config.Action) ([]string, error) {
	r, err := m.core.RemoteOf(p)
	if err != nil {
		return nil, err
	}
	if r != nil {
		return r.RunCommand(p.Name, act.Name), nil
	}
	argv, err := core.RenderArgv(p.Project, act.Run)
	if err == nil && len(argv) == 0 {
		err = errors.New("it runs nothing")
	}
	return argv, err
}

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
		argv, err := m.actionArgv(p, act)
		if err != nil {
			return func() tea.Msg { return actedMsg{err: fmt.Errorf("action %q: %w", act.Name, err)} }, true
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		if p.Host == "" {
			cmd.Dir = p.Path // a remote project's path is on its host, where the action runs
		}
		project := p.Name
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			// An action may open anything; the window that appears next is
			// the project's (claim-on-appear).
			return actedMsg{err: err, launch: &state.Launch{Project: project, At: time.Now()}}
		}), true
	}
	return nil, false
}
