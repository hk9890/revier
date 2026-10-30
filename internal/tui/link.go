package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/sshconfig"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The remote screen (decisions.md D154): alt+r lists the machines config.toml
// declares, each with what its revier answered when the screen opened. Enter
// on one asks the revier there for its projects, Enter on a project asks what
// to name the link, and Enter on the name writes it here, which is then a row
// like any other. Three steps in the list's own place, with the pane beside
// them. The bar's "new" adds a machine from the ssh configuration, a step of
// its own, and "remove" takes the one under the cursor off the list.

// askWait bounds one ask of a host. Long enough for a cold ssh; short
// enough that a host that is down is a message, not a wait.
const askWait = 15 * time.Second

// addMachineKey is the remote screen's "new".
const addMachineKey = "alt+n"

// remoteActions are the remote screen's own buttons.
var remoteActions = []barAction{
	{label: "new", key: addMachineKey, run: Model.addMachine},
	{label: "remove", key: "alt+d", run: Model.removeMachine},
}

// hostItem is one row of the machines, or of the ssh hosts "new" offers.
// check is what the machine answered, nil on a row of the ssh hosts, which
// is not asked.
type hostItem struct {
	host  string
	check *machineCheck
}

func (i hostItem) FilterValue() string { return i.host }

// machineCheck is a machine's answer to the screen's ask for its revier's
// version, or why it gave none. Unanswered, it is an ask still out. ask
// numbers the ask, so an answer to an earlier one is not taken for it.
type machineCheck struct {
	ask      int
	answered bool
	version  string
	err      error
}

// state is what the machine's row says of it.
func (c machineCheck) state(th theme.Theme) (string, lipgloss.Style) {
	switch {
	case !c.answered:
		return "checking…", th.NameDim
	case errors.Is(c.err, revier.ErrNoRevier):
		return "no revier", th.Attention
	case c.err != nil:
		return "does not answer", th.Attention
	}
	return "revier " + c.version, th.Running
}

// checkedMsg is a machine's answer to the screen's ask for its version.
type checkedMsg struct {
	host    string
	ask     int
	version string
	err     error
}

// remoteItem is one row of the dialog's second step: a project on the host,
// and the link here that already points at it, if any. It is drawn by the
// project table, so the host's list reads as the one it is added to.
type remoteItem struct {
	view   revier.ProjectView
	linked revier.ProjectName
}

func (i remoteItem) FilterValue() string         { return string(i.view.Project.Name) }
func (i remoteItem) rowView() revier.ProjectView { return i.view }
func (i remoteItem) rowPath() string             { return remoteHome(i.view.Project.Path) }
func (i remoteItem) rowUnsurveyed() bool         { return false }
func (i remoteItem) rowHeld() bool               { return i.view.Held() }
func (i remoteItem) rowBuiltin() bool            { return false }
func (i remoteItem) rowLinked() bool             { return i.linked != "" }

// remoteOrder puts the projects a link here names already first, and then
// sorts as the main list does: an agent that wants the user, then the open
// projects, and otherwise the host's order.
func remoteOrder(a, b remoteItem) int {
	return cmp.Or(cmp.Compare(a.rank(), b.rank()), cmp.Compare(a.need(), b.need()))
}

func (i remoteItem) rank() int {
	if i.linked != "" {
		return 0
	}
	return 1
}

func (i remoteItem) need() int {
	switch {
	case i.view.Attention():
		return 0
	case i.view.Held():
		return 1
	default:
		return 2
	}
}

// remoteHome writes a path on the host the way config.ContractHome writes one here,
// with its home directory as ~. The host's home is not known here, so it is
// taken to be the directory under /home or /Users the path starts in.
func remoteHome(p string) string {
	for _, base := range []string{"/home/", "/Users/"} {
		rest, ok := strings.CutPrefix(p, base)
		if !ok {
			continue
		}
		if _, tail, ok := strings.Cut(rest, "/"); ok {
			return "~/" + tail
		}
	}
	return p
}

// askedMsg is a host's answer to the dialog's ask, or why it gave none.
type askedMsg struct {
	host  string
	views []revier.ProjectView
	err   error
}

func newHostList(th theme.Theme) list.Model { return plainList(hostDelegate{theme: th}) }

// newRemoteList is a host's projects, drawn and filtered as the projects here
// are: a host can have as many.
func newRemoteList(th theme.Theme) list.Model { return newProjectList(th) }

// plainList is a list with nothing of its own on screen and no filter: its
// rows are few and each of them is a choice.
func plainList(d list.ItemDelegate) list.Model {
	l := list.New(nil, d, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return l
}

type hostDelegate struct{ theme theme.Theme }

func (d hostDelegate) Height() int                         { return 1 }
func (d hostDelegate) Spacing() int                        { return 0 }
func (d hostDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d hostDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(hostItem)
	if !ok {
		return
	}
	th := d.theme
	sel := index == m.Index()
	style := func(s lipgloss.Style) lipgloss.Style {
		if sel {
			return th.OnSelection(s)
		}
		return s
	}
	row := cursor(th, sel) + th.Remote.Render(th.Glyphs.Remote) + " " + style(th.ProjectName).Render(it.host)
	if it.check != nil {
		// The states stand in one column, after the widest host.
		width := 0
		for _, other := range m.Items() {
			if o, ok := other.(hostItem); ok {
				width = max(width, lipgloss.Width(o.host))
			}
		}
		text, st := it.check.state(th)
		row += style(th.Path).Render(strings.Repeat(" ", width-lipgloss.Width(it.host)+2)) + style(st).Render(text)
	}
	_, _ = fmt.Fprint(w, fill(clipTo(row, m.Width()), m.Width(), style))
}

// cursor is the bar down the left of a row, lit on the selected one.
func cursor(th theme.Theme, sel bool) string {
	if sel {
		return th.Cursor.Render(th.Glyphs.Cursor) + th.OnSelection(th.Path).Render(" ")
	}
	return th.Path.Render("  ")
}

// linkStep is the step of the dialog in view.
type linkStep int

const (
	linkHosts  linkStep = iota // which machine
	linkAdding                 // which ssh host to add as a machine
	linkRemote                 // which of its projects
	linkNaming                 // what to name the link
)

// linkScreen is the remote screen while it is up.
type linkScreen struct {
	step     linkStep
	hosts    list.Model              // the machines, the first step
	checks   map[string]machineCheck // what each machine answered this visit
	asks     int                     // the version asks made so far, which numbers each one
	adding   list.Model              // the ssh hosts "new" offers
	removing string                  // the machine a remove asks about, while it does
	links    int                     // how many links name that machine
	remote   list.Model              // a host's projects, the second
	host     string                  // the host the second step shows
	filter   string                  // the query over the host's projects
	before   revier.ProjectName      // the host's project the cursor was on when its query began
	query    textinput.Model         // that query, with its own cursor
	asking   string                  // the host an ask is out to, while it is
	name     textinput.Model         // the name field of the last step
	proposed bool                    // whether the name is still the one offered, which the first character typed replaces
}

func newLinkScreen(th theme.Theme) linkScreen {
	return linkScreen{
		hosts: newHostList(th), adding: newHostList(th), remote: newRemoteList(th),
		query: newPrompt(th, ""), name: newLinkNameInput(th),
	}
}

// linked is a link the dialog wrote: the project here, and the host's view of
// the project it points at.
type linked struct {
	project core.Project
	view    revier.ProjectView
}

// linkResult is what a press in the dialog leaves for the surface.
type linkResult struct {
	err    error   // the footer's
	closed bool    // Esc on the first step: back to the surface
	linked *linked // the link the press wrote
}

// openLink is alt+r. The cursor comes back to the list first, so the surface
// the dialog stands over is the one it is left on.
func (m Model) openLink() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if cmd, m.err = m.link.open(m.core); m.err != nil {
		return m, nil
	}
	m.toList()
	m.dialog = dialogLink
	return m, cmd
}

// addMachine is the remote screen's "new".
func (m Model) addMachine() (tea.Model, tea.Cmd) {
	m.err = m.link.openAdd()
	return m, nil
}

// removeMachine is the remote screen's "remove".
func (m Model) removeMachine() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cmd, m.err = m.link.remove(m.core, m.projects)
	return m, cmd
}

// checked takes a machine's answer to the screen's ask for its version.
func (m Model) checked(msg checkedMsg) (tea.Model, tea.Cmd) {
	m.link.checked(msg)
	return m, nil
}

// linkKey is every press while the dialog is up, and what it left for the
// surface: the dialog closed, or a link that is then a row. The bar's keys
// act where the bar is drawn.
func (m Model) linkKey(msg keyPress) (tea.Model, tea.Cmd) {
	if m.link.hasBar() {
		switch {
		case msg.String() == addMachineKey:
			return m.addMachine()
		case key.Matches(msg, m.keys.Delete):
			return m.removeMachine()
		}
	}
	res, cmd := m.link.key(m.surface(), msg)
	m.err = res.err
	switch {
	case res.closed:
		m.dialog = dialogNone
	case res.linked != nil:
		m.addLink(*res.linked)
	}
	return m, cmd
}

// addLink puts a link just written in as a row, at once, as the next survey
// will show it, rather than a refresh later.
func (m *Model) addLink(l linked) {
	p := l.project
	// Provisional, until the survey answers: the host's own view, as the
	// merge would lay it over a local one with no pane here yet. Its agents
	// go with its panes: no panel here shows one yet, so the row counts none
	// rather than counting the host's in a project it draws as closed
	// (decisions.md D104).
	view := l.view
	view.Project, view.Running, view.Home, view.Targets, view.Agents = p.Project, false, revier.TargetRef{}, nil, nil
	m.addProject(p, view)
}

// asked takes a host's answer to the dialog's ask.
func (m Model) asked(msg askedMsg) (tea.Model, tea.Cmd) {
	cmd, err := m.link.asked(m.projects, msg)
	if err != nil {
		m.err = err
	}
	return m, cmd
}

// open is the screen's first step, over the machines config.toml declares,
// and an ask of each one for its revier's version. Each visit asks again,
// and every ask is its own command, so no machine holds the screen or
// another machine's row. With no machines, the step says how to add one.
func (s *linkScreen) open(c *core.Core) (tea.Cmd, error) {
	cfg, err := readConfig()
	if err != nil {
		return nil, err
	}
	s.checks = map[string]machineCheck{}
	cmd := s.load(c, cfg.MachineHosts(), "")
	s.hosts.Select(0)
	// A link written last time left its query behind; Esc here would clear
	// it instead of closing the dialog.
	s.setFilter("")
	s.step, s.removing = linkHosts, ""
	return cmd, nil
}

// readConfig is config.toml as it is now: the screen reads it at each
// change, so a machine added by hand is listed.
func readConfig() (*config.Config, error) {
	root, err := config.Root()
	if err != nil {
		return nil, err
	}
	return config.Read(root)
}

// load lists hosts as the machines, each with what it answered this visit,
// with the cursor on at where at is one of them. A machine not asked this
// visit, one added by hand among them, is asked now.
func (s *linkScreen) load(c *core.Core, hosts []string, at string) tea.Cmd {
	var cmds []tea.Cmd
	items := make([]list.Item, 0, len(hosts))
	for _, h := range hosts {
		if _, ok := s.checks[h]; !ok {
			cmds = append(cmds, s.check(c, h))
		}
		answer := s.checks[h]
		items = append(items, hostItem{host: h, check: &answer})
	}
	_ = s.hosts.SetItems(items)
	if i := slices.Index(hosts, at); i >= 0 {
		s.hosts.Select(i)
	}
	return tea.Batch(cmds...)
}

// check asks a machine for its revier's version, off the terminal. It is no
// survey, so it is no round of the host's (decisions.md D154, D130).
func (s *linkScreen) check(c *core.Core, host string) tea.Cmd {
	s.asks++
	ask := s.asks
	s.checks[host] = machineCheck{ask: ask}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), askWait)
		defer cancel()
		v, err := c.VersionOn(ctx, host)
		return checkedMsg{host: host, ask: ask, version: v, err: err}
	}
}

// checked takes a machine's answer into its row. A machine removed since
// the ask has no row to take it, and the answer to an ask of an earlier
// visit, or of before a remove, is not the one the row waits for.
func (s *linkScreen) checked(msg checkedMsg) {
	if was, ok := s.checks[msg.host]; !ok || was.ask != msg.ask {
		return
	}
	c := machineCheck{ask: msg.ask, answered: true, version: msg.version, err: msg.err}
	s.checks[msg.host] = c
	for i, item := range s.hosts.Items() {
		if it, ok := item.(hostItem); ok && it.host == msg.host {
			s.hosts.SetItem(i, hostItem{host: it.host, check: &c})
		}
	}
}

// hasBar reports the step that draws the screen's bar: the machines, while
// no remove asks about one.
func (s *linkScreen) hasBar() bool { return s.step == linkHosts && s.removing == "" }

// openAdd is "new": the step that offers the hosts the ssh configuration
// names, without the machines on the list and without this machine. With
// none to offer it says why, and the step does not open. An ask still out is
// abandoned with the machines, as Esc abandons it.
func (s *linkScreen) openAdd() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}
	path, err := sshconfig.Path()
	if err != nil {
		return err
	}
	all, err := sshconfig.Hosts(path)
	if err != nil {
		return err
	}
	here := config.ThisMachine()
	hosts := slices.DeleteFunc(slices.Clone(all), func(h string) bool { return cfg.Declares(h) || here.Is(h) })
	switch {
	case len(all) == 0:
		return fmt.Errorf("no hosts in %s; add a Host entry there first", config.ContractHome(path))
	case len(hosts) == 0:
		return fmt.Errorf("every host in %s is on the list or is this machine", config.ContractHome(path))
	}
	items := make([]list.Item, 0, len(hosts))
	for _, h := range hosts {
		items = append(items, hostItem{host: h})
	}
	_ = s.adding.SetItems(items)
	s.adding.Select(0)
	s.step, s.asking = linkAdding, ""
	return nil
}

// add is Enter on an ssh host: a [[machine]] table for it in config.toml,
// at once, and the machines again, with the cursor on it and an ask out to
// it.
func (s *linkScreen) add(c *core.Core) (tea.Cmd, error) {
	it, ok := s.adding.SelectedItem().(hostItem)
	if !ok {
		return nil, nil
	}
	if err := withConfigRoot(func(root string) error { return config.AddMachine(root, it.host) }); err != nil {
		return nil, err
	}
	cfg, err := readConfig()
	if err != nil {
		return nil, err
	}
	s.step = linkHosts
	return s.load(c, cfg.MachineHosts(), it.host), nil
}

// remove is "remove" on the machine under the cursor. A machine a link
// names is asked about first, since the link stays and its machine leaves
// the list; any other one goes at once. The question abandons an ask still
// out, whose answer would open a host's projects under it.
func (s *linkScreen) remove(c *core.Core, projects []core.Project) (tea.Cmd, error) {
	it, ok := s.hosts.SelectedItem().(hostItem)
	if !ok {
		return nil, nil
	}
	if n := linksTo(projects, it.host); n > 0 {
		s.removing, s.links, s.asking = it.host, n, ""
		return nil, nil
	}
	return s.drop(c, it.host)
}

// linksTo is how many of projects are links to host.
func linksTo(projects []core.Project, host string) int {
	n := 0
	for _, p := range projects {
		if p.Remote != nil && p.Remote.Host == host {
			n++
		}
	}
	return n
}

// drop takes host off the list: its [[machine]] table goes from
// config.toml, at once, its row from the machines, and an ask of its
// projects still out is abandoned. The cursor keeps its place, or goes to
// the new last row where it was on the last.
func (s *linkScreen) drop(c *core.Core, host string) (tea.Cmd, error) {
	s.removing = ""
	if s.asking == host {
		s.asking = ""
	}
	if err := withConfigRoot(func(root string) error { return config.RemoveMachine(root, host) }); err != nil {
		return nil, err
	}
	cfg, err := readConfig()
	if err != nil {
		return nil, err
	}
	delete(s.checks, host)
	at := s.hosts.Index()
	hosts := cfg.MachineHosts()
	cmd := s.load(c, hosts, "")
	s.hosts.Select(max(0, min(at, len(hosts)-1)))
	return cmd, nil
}

// removeKey is every press while a remove asks about a machine: Enter takes
// it off the list, and any other key keeps it.
func (s *linkScreen) removeKey(sf surface, msg keyPress) (linkResult, tea.Cmd) {
	var res linkResult
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Enter):
		cmd, res.err = s.drop(sf.core, s.removing)
	default:
		s.removing = ""
	}
	return res, cmd
}

// key is every press while the dialog is up. It takes a step at a time: the
// movement keys walk the rows, Enter takes the step, Esc goes back one, and
// none of the surface's own keys act under it. On a host's projects what is
// typed filters them, and Esc clears the query before it goes back, as on the
// surface.
func (s *linkScreen) key(sf surface, msg keyPress) (linkResult, tea.Cmd) {
	if s.step == linkNaming {
		return s.nameKey(sf, msg)
	}
	if s.removing != "" {
		return s.removeKey(sf, msg)
	}
	var res linkResult
	if by, ok := sf.keys.move(msg, func(int) int { return sf.page }); ok {
		moveRow(s.list(), by)
		return res, nil
	}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		if s.filter != "" {
			s.setFilter("")
			return res, nil
		}
		res.closed = s.back()
	case key.Matches(msg, sf.keys.Enter):
		var cmd tea.Cmd
		switch s.step {
		case linkHosts:
			return res, s.ask(sf.core)
		case linkAdding:
			cmd, res.err = s.add(sf.core)
		default:
			cmd, res.err = s.pick()
		}
		return res, cmd
	case s.step == linkRemote && promptKey(msg):
		next, cmd := s.query.Update(msg.input())
		s.query = next
		if next.Value() != s.filter {
			s.setFilter(next.Value())
		}
		return res, cmd
	}
	return res, nil
}

// setFilter is every change to the query over a host's projects. As on the
// surface, clearing it puts the cursor back on the project it was on when the
// query began.
func (s *linkScreen) setFilter(q string) {
	if s.filter == "" && q != "" {
		s.before = s.selected()
	}
	s.filter = q
	if s.query.Value() != q {
		s.query.SetValue(q)
	}
	if q != "" {
		s.remote.SetFilterText(q)
		return
	}
	s.remote.ResetFilter()
	for i, item := range s.remote.Items() {
		if it, ok := item.(remoteItem); ok && it.view.Project.Name == s.before {
			s.remote.Select(i)
			return
		}
	}
	s.remote.Select(0)
}

// selected is the host's project under the cursor, if any.
func (s *linkScreen) selected() revier.ProjectName {
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	return it.view.Project.Name
}

// back is Esc: a host's projects and the ssh hosts go back to the machines,
// and the machines report that the dialog closed. An ask still out is
// abandoned with the step it was made from, because its answer must not pull
// the surface back into a dialog the user has just left.
func (s *linkScreen) back() (closed bool) {
	s.asking = ""
	switch s.step {
	case linkRemote:
		s.query.Blur()
		s.step = linkHosts
		return false
	case linkAdding:
		s.step = linkHosts
		return false
	}
	return true
}

// list is the rows of the step in view, nil on the step that names the link.
// It is a pointer because the cursor moves on it.
func (s *linkScreen) list() *list.Model {
	switch s.step {
	case linkHosts:
		return &s.hosts
	case linkAdding:
		return &s.adding
	case linkRemote:
		return &s.remote
	}
	return nil
}

// oneLine reports a step whose rows are one line each: a machine or an ssh
// host is its name, and a host's projects are drawn as the projects here.
func (s *linkScreen) oneLine() bool { return s.step == linkHosts || s.step == linkAdding }

// ask is Enter on a host: the host is asked for its projects, off the
// terminal, and the answer opens the second step.
func (s *linkScreen) ask(c *core.Core) tea.Cmd {
	it, ok := s.hosts.SelectedItem().(hostItem)
	if !ok {
		return nil
	}
	s.asking = it.host
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), askWait)
		defer cancel()
		views, err := c.ProjectsOn(ctx, it.host)
		return askedMsg{host: it.host, views: config.Linkable(views), err: err}
	}
}

// asked takes a host's answer. A failure stays on the hosts, with the failure
// for the footer; an answer is the second step. projects are the ones here,
// for the link that already points at a project of the host.
func (s *linkScreen) asked(projects []core.Project, msg askedMsg) (tea.Cmd, error) {
	if msg.host != s.asking {
		return nil, nil // an answer to an ask the user has moved on from
	}
	s.asking = ""
	if msg.err != nil {
		return nil, msg.err
	}
	rows := make([]remoteItem, 0, len(msg.views))
	for _, v := range msg.views {
		rows = append(rows, remoteItem{view: v, linked: core.LinkedAs(projects, msg.host, v.Project.Name)})
	}
	slices.SortStableFunc(rows, remoteOrder)
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = r
	}
	s.host = msg.host
	s.setFilter("")
	_ = s.remote.SetItems(items)
	s.remote.Select(0)
	s.query.Placeholder = "filter the projects on " + msg.host
	s.step = linkRemote
	var err error
	if len(items) == 0 {
		err = fmt.Errorf("%s has no projects; `revier new` there writes one", msg.host)
	}
	return s.query.Focus(), err
}

// pick is Enter on a project of the host: the step that names the link, with
// a name that cannot be taken by a project here in the field. A project
// already linked is refused, and says under which name.
func (s *linkScreen) pick() (tea.Cmd, error) {
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok {
		return nil, nil
	}
	if it.linked != "" {
		return nil, fmt.Errorf("%s on %s is already linked as %s", it.view.Project.Name, s.host, it.linked)
	}
	s.name.SetValue(linkName(s.host, it.view.Project.Name))
	s.name.CursorEnd()
	s.proposed = true
	s.step = linkNaming
	s.query.Blur()
	return s.name.Focus(), nil
}

// linkName is the name a link is offered under: the host and the project
// there, behind "rs-", so a link does not take the name of a project here.
func linkName(host string, project revier.ProjectName) string {
	return "rs-" + host + "-" + string(project)
}

// nameKey is every press while the link is named. Enter writes it, Esc goes
// back to the host's projects, and everything else is the field's. The
// offered name is replaced by the first character typed, as a selected
// field's is; any other edit keeps it.
func (s *linkScreen) nameKey(sf surface, msg keyPress) (linkResult, tea.Cmd) {
	res := linkResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		s.step = linkRemote
		s.name.Blur()
		return res, s.query.Focus()
	case key.Matches(msg, sf.keys.Enter):
		return s.write(sf), nil
	}
	if altRune(msg) {
		return res, nil
	}
	res.err = nil
	if s.proposed && (msg.runes() || msg.space()) {
		s.name.SetValue("")
	}
	s.proposed = false
	in, cmd := s.name.Update(msg.input())
	s.name = in
	return res, cmd
}

// nameFault is why the name in the field cannot be written, or nil. It is
// asked on every frame, so a name that is taken, or that no agent address can
// carry, says so as it is typed.
func (s *linkScreen) nameFault(projects []core.Project) error {
	name := s.nameValue()
	if name == "" {
		return fmt.Errorf("give the link a name")
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}
	if err := config.CanAddress(name); err != nil {
		return err
	}
	if p, ok := projectNamed(projects, name); ok && !config.IsBuiltin(p) {
		return fmt.Errorf("a project named %q exists here: %s", name, config.ContractHome(p.File))
	}
	return nil
}

func (s *linkScreen) nameValue() revier.ProjectName {
	return revier.ProjectName(strings.TrimSpace(s.name.Value()))
}

// write is Enter on the name: a link file under it. A name that is taken
// writes nothing, so no project here is overwritten.
func (s *linkScreen) write(sf surface) linkResult {
	res := linkResult{err: sf.err}
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok {
		return res
	}
	if res.err = s.nameFault(sf.projects); res.err != nil {
		return res
	}
	root, err := config.Root()
	if err != nil {
		res.err = err
		return res
	}
	p, err := app.LinkProject(root, sf.core.Ledger, s.nameValue(), s.host, it.view.Project)
	if err != nil {
		res.err = err
		return res
	}
	s.name.Blur()
	res.err, res.linked = sf.err, &linked{project: p, view: it.view}
	return res
}

// title names the step, on the surface's first line, where the machines draw
// the bar instead.
func (s *linkScreen) title() string {
	switch s.step {
	case linkHosts:
		return "Remove a machine from the list"
	case linkAdding:
		return "Add a machine from the ssh configuration"
	case linkRemote:
		return s.host
	case linkNaming:
		name := "Name the link"
		if it, ok := s.remote.SelectedItem().(remoteItem); ok {
			name += " to " + string(it.view.Project.Name) + " on " + s.host
		}
		return name
	}
	return ""
}

// subtitle is the line over the rule: the query over a host's projects, the
// name field, or the question a remove asks. The machines and the ssh hosts
// say nothing there - the bar or the title over them already says what the
// rows are, and the rule under them counts them - but they keep the line, so
// the rows do not move as the step changes.
func (s *linkScreen) subtitle(th theme.Theme) string {
	if s.removing != "" {
		stay := "stay"
		if s.links == 1 {
			stay = "stays"
		}
		return " " + th.Attention.Render(fmt.Sprintf("Take %s off the list? %s to it %s here.", s.removing, core.Count(s.links, "link"), stay))
	}
	switch s.step {
	case linkRemote:
		return " " + inputView(s.query)
	case linkNaming:
		// The offered name is drawn as a selection until it is edited,
		// because the first character typed replaces it.
		in := s.name
		if s.proposed {
			st := in.Styles()
			st.Focused.Text = th.OnSelection(th.ProjectName)
			st.Blurred.Text = st.Focused.Text
			in.SetStyles(st)
		}
		return " " + inputView(in)
	}
	return ""
}

// count is what the rule says of the rows under it.
func (s *linkScreen) count(th theme.Theme) string {
	switch {
	case s.step == linkHosts:
		return th.NameDim.Render(core.Count(len(s.hosts.Items()), "machine"))
	case s.step == linkAdding:
		return th.NameDim.Render(core.Count(len(s.adding.Items()), "host"))
	case s.step == linkRemote && s.filter != "":
		return th.NameDim.Render(fmt.Sprintf("%d/%d projects", len(s.remote.VisibleItems()), len(s.remote.Items())))
	case s.step == linkRemote:
		return th.NameDim.Render(fmt.Sprintf("%d projects", len(s.remote.Items())))
	}
	return ""
}

// empty is what stands in place of rows: how to add a machine where there is
// none, or that the query left none.
func (s *linkScreen) empty() string {
	switch {
	case s.step == linkHosts && len(s.hosts.Items()) == 0:
		return "No machines on the list. " + addMachineKey + " adds one from the ssh configuration."
	case s.step == linkRemote && s.filter != "":
		return fmt.Sprintf("No project on %s matches %q.", s.host, s.filter)
	}
	return ""
}

// help is the footer. Its list steps take the same keys and mean different
// things by them.
func (s *linkScreen) help(k keyMap) []key.Binding {
	switch {
	case s.removing != "":
		return []key.Binding{helpKey("enter", "take it off"), helpKey("esc", "keep it"), k.Quit}
	case s.step == linkHosts:
		return []key.Binding{helpKey("enter", "list its projects"), helpKey(addMachineKey, "new"), helpKey("alt+d", "remove"), helpKey("esc", "back"), k.Quit}
	case s.step == linkAdding:
		return []key.Binding{helpKey("enter", "add it"), helpKey("esc", "back"), k.Quit}
	case s.step == linkRemote:
		return []key.Binding{helpKey("enter", "name the link"), helpKey("type", "filter"), helpKey("esc", "clear/back"), k.Quit}
	}
	return []key.Binding{helpKey("enter", "link"), helpKey("esc", "back"), k.Quit}
}

// restyle redraws the rows and the fields in a theme.
func (s *linkScreen) restyle(th theme.Theme) {
	s.hosts.SetDelegate(hostDelegate{theme: th})
	s.adding.SetDelegate(hostDelegate{theme: th})
	s.light(th, -1)
	styleField(&s.query, th)
	styleField(&s.name, th)
}

// light draws a host's projects with the row under the pointer lit, -1 for
// none.
func (s *linkScreen) light(th theme.Theme, row int) {
	s.remote.SetDelegate(projectDelegate{theme: th, hover: row}.measured(s.remote))
}

// nameScreen is what stands in the list's place while the link is named:
// what Enter writes, or why it writes nothing. The reason is wrapped where
// every other line is clipped: it is a sentence, and what the name breaks
// comes at its end.
func (s *linkScreen) nameScreen(sf surface) string {
	th, w := sf.theme, sf.list
	say := func(st lipgloss.Style, text string) string {
		return st.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	if err := s.nameFault(sf.projects); err != nil {
		return th.Attention.PaddingLeft(2).Width(w).Render(err.Error()) + "\n" +
			say(th.Meta, "Enter writes nothing until the name changes")
	}
	root, err := config.Root()
	if err != nil {
		return say(th.Attention, err.Error())
	}
	return say(th.NameDim, "Enter writes") + "\n" +
		say(th.Path, config.ContractHome(config.ProjectFile(root, s.nameValue()))) + "\n" +
		say(th.Meta, fmt.Sprintf("a link to %s on %s", it.view.Project.Name, s.host))
}

// newLinkNameInput is the field the link's name is typed in.
func newLinkNameInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.CharLimit = 128
	return in
}

// detail is the pane past the first step: what the host said about the
// project under the cursor.
func (s *linkScreen) detail(sf surface) string {
	th, w := sf.theme, sf.pane
	line := func(label, value string, style lipgloss.Style) string {
		return hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style) + "\n"
	}
	switch s.step {
	case linkHosts:
		return s.machineDetail(sf, line)
	case linkAdding:
		return ""
	}
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	v := it.view
	out := th.Header.Render(clipTo(string(v.Project.Name)+"@"+s.host, w)) + "\n"
	out += line("Path", v.Project.Path, th.Path)
	status, style := "stopped", th.NameDim
	switch {
	case v.Held():
		status, style = "running", th.Running
	case !v.PathExists:
		status, style = "not cloned", th.PathMissing
	}
	out += line("Status", status, style)
	if it.linked != "" {
		out += line("Link", string(it.linked), th.Remote)
	} else if s.step == linkRemote {
		out += th.Meta.Render(clipTo("Enter: name a link to it here", w)) + "\n"
	}
	if len(v.Agents) > 0 {
		out += heading(th, "Agents", w)
		for _, a := range v.Agents {
			out += detailAgent(sf.spun, a, w) + "\n"
		}
	}
	return out
}

// machineDetail is the pane on the machines: what the machine under the
// cursor answered, and why where it gave no version, and how many links
// here name it.
func (s *linkScreen) machineDetail(sf surface, line func(label, value string, style lipgloss.Style) string) string {
	it, ok := s.hosts.SelectedItem().(hostItem)
	if !ok || it.check == nil {
		return ""
	}
	th, w := sf.theme, sf.pane
	out := th.Header.Render(clipTo(it.host, w)) + "\n"
	state, style := it.check.state(th)
	out += line("Status", state, style)
	if it.check.err != nil {
		out += line("Reason", it.check.err.Error(), th.Meta)
	}
	out += line("Links", core.Count(linksTo(sf.projects, it.host), "link")+" here", th.NameDim)
	return out + th.Meta.Render(clipTo("Enter: list its projects", w)) + "\n"
}

// askingLine is the footer while an ask is out.
func (s *linkScreen) askingLine(th theme.Theme) string {
	return th.Meta.Render(fmt.Sprintf(" asking %s for its projects…", s.asking))
}
