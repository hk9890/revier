package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/sshconfig"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The link dialog (decisions.md D45): alt+r lists the hosts the ssh
// configuration names, Enter on one asks the revier there for its projects,
// Enter on a project asks what to name the link, and Enter on the name writes
// it here, which is then a row like any other. Three steps in the list's own
// place, with the pane beside them.

// askWait bounds one ask of a host. Long enough for a cold ssh; short
// enough that a host that is down is a message, not a wait.
const askWait = 15 * time.Second

// hostItem is one row of the dialog's first step.
type hostItem struct{ host string }

func (i hostItem) FilterValue() string { return i.host }

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

func (i remoteItem) rowNote() string {
	if i.linked == "" {
		return ""
	}
	return "linked as " + string(i.linked)
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
	_, _ = fmt.Fprint(w, fill(row, m.Width(), style))
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
	linkHosts  linkStep = iota // which host
	linkRemote                 // which of its projects
	linkNaming                 // what to name the link
)

// linkScreen is the link dialog while it is up.
type linkScreen struct {
	step     linkStep
	hosts    list.Model         // the hosts, the first step
	remote   list.Model         // a host's projects, the second
	host     string             // the host the second step shows
	filter   string             // the query over the host's projects
	before   revier.ProjectName // the host's project the cursor was on when its query began
	query    textinput.Model    // that query, with its own cursor
	asking   string             // the host an ask is out to, while it is
	name     textinput.Model    // the name field of the last step
	proposed bool               // whether the name is still the one offered, which the first character typed replaces
}

func newLinkScreen(th theme.Theme) linkScreen {
	return linkScreen{
		hosts: newHostList(th), remote: newRemoteList(th),
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
	if m.err = m.link.open(); m.err != nil {
		return m, nil
	}
	m.toList()
	m.dialog = dialogLink
	return m, nil
}

// linkKey is every press while the dialog is up, and what it left for the
// surface: the dialog closed, or a link that is then a row.
func (m Model) linkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	m.projects = append(m.projects, p)
	m.setKeys()
	// Provisional, until the survey answers: the host's own view, as the
	// merge would lay it over a local one with no pane here yet. Its agents
	// go with its panes: no panel here shows one yet, so the row counts none
	// rather than counting the host's in a project it draws as closed
	// (decisions.md D104).
	view := l.view
	view.Project, view.Running, view.Home, view.Targets, view.Agents = p.Project, false, revier.TargetRef{}, nil, nil
	m.views = m.sorted(append(m.views, view))
	m.dialog = dialogNone
	m.reload()
	m.selectName(p.Name)
}

// asked takes a host's answer to the dialog's ask.
func (m Model) asked(msg askedMsg) (tea.Model, tea.Cmd) {
	cmd, err := m.link.asked(m.projects, msg)
	if err != nil {
		m.err = err
	}
	return m, cmd
}

// open is the dialog's first step, over the hosts the ssh configuration
// names. No hosts is a message, not an empty list to be puzzled at.
func (s *linkScreen) open() error {
	path, err := sshconfig.Path()
	if err != nil {
		return err
	}
	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts in %s; add a Host entry to link a project on another machine", config.ContractHome(path))
	}
	items := make([]list.Item, 0, len(hosts))
	for _, h := range hosts {
		items = append(items, hostItem{host: h})
	}
	_ = s.hosts.SetItems(items)
	s.hosts.Select(0)
	// A link written last time left its query behind; Esc here would clear
	// it instead of closing the dialog.
	s.setFilter("")
	s.step = linkHosts
	return nil
}

// key is every press while the dialog is up. It takes a step at a time: the
// movement keys walk the rows, Enter takes the step, Esc goes back one, and
// none of the surface's own keys act under it. On a host's projects what is
// typed filters them, and Esc clears the query before it goes back, as on the
// surface (decisions.md D44).
func (s *linkScreen) key(sf surface, msg tea.KeyMsg) (linkResult, tea.Cmd) {
	if s.step == linkNaming {
		return s.nameKey(sf, msg)
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
		if s.step == linkHosts {
			return res, s.ask(sf.core)
		}
		var cmd tea.Cmd
		cmd, res.err = s.pick()
		return res, cmd
	case s.step == linkRemote && promptKey(msg):
		next, cmd := s.query.Update(msg)
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

// back is Esc: the second step goes back to the first, and the first reports
// that the dialog closed. An ask still out is abandoned with the step it was
// made from, because its answer must not pull the surface back into a dialog
// the user has just left.
func (s *linkScreen) back() (closed bool) {
	s.asking = ""
	if s.step == linkRemote {
		s.query.Blur()
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
	case linkRemote:
		return &s.remote
	}
	return nil
}

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
		return askedMsg{host: it.host, views: views, err: err}
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
	items := make([]list.Item, 0, len(msg.views))
	for _, v := range msg.views {
		items = append(items, remoteItem{view: v, linked: core.LinkedAs(projects, msg.host, v.Project.Name)})
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
func (s *linkScreen) nameKey(sf surface, msg tea.KeyMsg) (linkResult, tea.Cmd) {
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
	if s.proposed && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
		s.name.SetValue("")
	}
	s.proposed = false
	in, cmd := s.name.Update(msg)
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
	if p, ok := projectNamed(projects, name); ok {
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
	p, err := app.LinkProject(root, s.nameValue(), s.host, it.view.Project)
	if err != nil {
		res.err = err
		return res
	}
	s.name.Blur()
	res.err, res.linked = sf.err, &linked{project: p, view: it.view}
	return res
}

// title names the step, on the surface's first line.
func (s *linkScreen) title() string {
	switch s.step {
	case linkRemote:
		return s.host
	case linkNaming:
		name := "Name the link"
		if it, ok := s.remote.SelectedItem().(remoteItem); ok {
			name += " to " + string(it.view.Project.Name) + " on " + s.host
		}
		return name
	}
	return "Link a project on another machine"
}

// subtitle is the line over the rule: the query over a host's projects, or
// the name field. The hosts step says nothing there - the title over it
// already says what the rows are, and the rule under it counts them - but it
// keeps the line, so the rows do not move as the step changes.
func (s *linkScreen) subtitle(th theme.Theme) string {
	switch s.step {
	case linkRemote:
		return " " + s.query.View()
	case linkNaming:
		// The offered name is drawn as a selection until it is edited,
		// because the first character typed replaces it.
		in := s.name
		if s.proposed {
			in.TextStyle = th.OnSelection(th.ProjectName)
		}
		return " " + in.View()
	}
	return ""
}

// count is what the rule says of the rows under it.
func (s *linkScreen) count(th theme.Theme) string {
	switch {
	case s.step == linkHosts:
		return th.NameDim.Render(fmt.Sprintf("%d hosts", len(s.hosts.Items())))
	case s.step == linkRemote && s.filter != "":
		return th.NameDim.Render(fmt.Sprintf("%d/%d projects", len(s.remote.VisibleItems()), len(s.remote.Items())))
	case s.step == linkRemote:
		return th.NameDim.Render(fmt.Sprintf("%d projects", len(s.remote.Items())))
	}
	return ""
}

// empty is what stands in place of rows when the query left none.
func (s *linkScreen) empty() string {
	if s.step == linkRemote && s.filter != "" {
		return fmt.Sprintf("No project on %s matches %q.", s.host, s.filter)
	}
	return ""
}

// help is the footer. Its two list steps take the same keys and mean
// different things by them.
func (s *linkScreen) help(k keyMap) []key.Binding {
	switch s.step {
	case linkHosts:
		return []key.Binding{helpKey("enter", "list its projects"), helpKey("esc", "back"), k.Quit}
	case linkRemote:
		return []key.Binding{helpKey("enter", "name the link"), helpKey("type", "filter"), helpKey("esc", "clear/back"), k.Quit}
	}
	return []key.Binding{helpKey("enter", "link"), helpKey("esc", "back"), k.Quit}
}

// restyle redraws the rows and the fields in a theme.
func (s *linkScreen) restyle(th theme.Theme) {
	s.hosts.SetDelegate(hostDelegate{theme: th})
	s.light(th, -1)
	styleField(&s.query, th)
	styleField(&s.name, th)
}

// light draws a host's projects with the row under the pointer lit, -1 for
// none.
func (s *linkScreen) light(th theme.Theme, row int) {
	s.remote.SetDelegate(projectDelegate{theme: th, hover: row})
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
	it, ok := s.remote.SelectedItem().(remoteItem)
	if !ok || s.step == linkHosts {
		return ""
	}
	th, v, w := sf.theme, it.view, sf.pane
	line := func(label, value string, style lipgloss.Style) string {
		return hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style) + "\n"
	}
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
		out += line("Linked as", string(it.linked), th.Remote)
	} else if s.step == linkRemote {
		out += th.Meta.Render(clipTo("Enter: name a link to it here", w)) + "\n"
	}
	if len(v.Agents) > 0 {
		out += heading(th, "Agents", w)
		for _, a := range v.Agents {
			out += detailAgent(sf.spun, agentRow{agent: a}, w, false, false, false) + "\n"
		}
	}
	return out
}

// askingLine is the footer while an ask is out.
func (s *linkScreen) askingLine(th theme.Theme) string {
	return th.Meta.Render(fmt.Sprintf(" asking %s for its projects…", s.asking))
}
