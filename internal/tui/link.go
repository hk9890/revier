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

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/sshconfig"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The link dialog (decisions.md D45): alt+r lists the hosts the ssh
// configuration names, Enter on one asks the revier there for its projects,
// Enter on a project asks what to name the link, and Enter on the name writes
// it here, which is then a row like any other. Three steps in the list's own
// place, with the pane beside them.

// askTimeout bounds one ask of a host. Long enough for a cold ssh; short
// enough that a host that is down is a message, not a wait.
const askTimeout = 15 * time.Second

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

func (i remoteItem) rowNote() string {
	if i.linked == "" {
		return ""
	}
	return "linked as " + string(i.linked)
}

// remoteHome writes a path on the host the way contractHome writes one here,
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

// openHosts is alt+r: the dialog's first step, over the hosts the ssh
// configuration names. No hosts is a message, not an empty list to be
// puzzled at. The cursor comes back to the list first, so the surface the
// dialog stands over is the one it is left on.
func (m Model) openHosts() (tea.Model, tea.Cmd) {
	path, err := sshconfig.Path()
	if err != nil {
		m.err = err
		return m, nil
	}
	hosts, err := sshconfig.Hosts(path)
	if err != nil {
		m.err = err
		return m, nil
	}
	if len(hosts) == 0 {
		m.err = fmt.Errorf("no hosts in %s; add a Host entry to link a project on another machine", contractHome(path))
		return m, nil
	}
	items := make([]list.Item, 0, len(hosts))
	for _, h := range hosts {
		items = append(items, hostItem{host: h})
	}
	_ = m.hlist.SetItems(items)
	m.hlist.Select(0)
	// A link written last time left its query behind; Esc here would clear
	// it instead of closing the dialog.
	m.setRemoteFilter("")
	m.toList()
	m.dialog = dialogHosts
	return m, nil
}

// dialogKey is every press while the dialog is up. It takes a step at a
// time: the movement keys walk the rows, Enter takes the step, Esc goes back
// one, and none of the surface's own keys act under it. On a host's projects
// what is typed filters them, and Esc clears the query before it goes back,
// as on the surface (decisions.md D44).
func (m Model) dialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	if by, ok := m.keys.move(msg, func(int) int { return m.listPage() }); ok {
		moveRow(m.dialogList(), by)
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		if m.rfilter != "" {
			m.setRemoteFilter("")
			return m, nil
		}
		m.stepBack()
	case key.Matches(msg, m.keys.Enter):
		return m.dialogEnter()
	case m.dialog == dialogRemote && m.promptKey(msg):
		next, cmd := m.rinput.Update(msg)
		m.rinput = next
		if next.Value() != m.rfilter {
			m.setRemoteFilter(next.Value())
		}
		return m, cmd
	}
	return m, nil
}

// setRemoteFilter is every change to the query over a host's projects. As on
// the surface, clearing it puts the cursor back on the project it was on when
// the query began.
func (m *Model) setRemoteFilter(q string) {
	if m.rfilter == "" && q != "" {
		m.rbefore = m.remoteSelected()
	}
	m.rfilter = q
	if m.rinput.Value() != q {
		m.rinput.SetValue(q)
	}
	if q != "" {
		m.rlist.SetFilterText(q)
		return
	}
	m.rlist.ResetFilter()
	for i, item := range m.rlist.Items() {
		if it, ok := item.(remoteItem); ok && it.view.Project.Name == m.rbefore {
			m.rlist.Select(i)
			return
		}
	}
	m.rlist.Select(0)
}

// remoteSelected is the host's project under the cursor, if any.
func (m Model) remoteSelected() revier.ProjectName {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	return it.view.Project.Name
}

// dialogEnter takes the step the cursor is on: a host is asked for its
// projects, a project of that host is linked.
func (m Model) dialogEnter() (tea.Model, tea.Cmd) {
	switch m.dialog {
	case dialogHosts:
		return m.askHost()
	case dialogSessions:
		return m.restoreSession()
	}
	return m.link()
}

// stepBack is Esc in the dialog: the second step goes back to the first, the
// first back to the surface. An ask still out is abandoned with the step it
// was made from, because its answer must not pull the surface back into a
// dialog the user has just left.
func (m *Model) stepBack() {
	m.asking = ""
	if m.dialog == dialogRemote {
		m.rinput.Blur()
		m.dialog = dialogHosts
		return
	}
	m.dialog = dialogNone
}

// dialogList is the list of the step in view. It is a pointer because the
// cursor moves on it.
func (m *Model) dialogList() *list.Model {
	if m.dialog == dialogRemote {
		return &m.rlist
	}
	return &m.hlist
}

// askHost is Enter on a host: the host is asked for its projects, off the
// terminal, and the answer opens the second step.
func (m Model) askHost() (tea.Model, tea.Cmd) {
	it, ok := m.hlist.SelectedItem().(hostItem)
	if !ok {
		return m, nil
	}
	m.asking = it.host
	c := m.core
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
		defer cancel()
		views, err := c.ProjectsOn(ctx, it.host)
		return askedMsg{host: it.host, views: views, err: err}
	}
}

// asked takes a host's answer. A failure stays on the hosts, with the
// failure in the footer; an answer is the second step.
func (m Model) asked(msg askedMsg) (tea.Model, tea.Cmd) {
	if msg.host != m.asking {
		return m, nil // an answer to an ask the user has moved on from
	}
	m.asking = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	items := make([]list.Item, 0, len(msg.views))
	for _, v := range msg.views {
		items = append(items, remoteItem{view: v, linked: m.linkedAs(msg.host, v.Project.Name)})
	}
	m.host = msg.host
	m.setRemoteFilter("")
	_ = m.rlist.SetItems(items)
	m.rlist.Select(0)
	m.rinput.Placeholder = "filter the projects on " + msg.host
	m.dialog = dialogRemote
	if len(items) == 0 {
		m.err = fmt.Errorf("%s has no projects; `revier new` there writes one", msg.host)
	}
	return m, m.rinput.Focus()
}

// linkedAs is the name of the link here to a project on a host, if any.
func (m Model) linkedAs(host string, project revier.ProjectName) revier.ProjectName {
	for _, p := range m.projects {
		if p.Remote != nil && p.Remote.Host == host && p.Remote.Project == project {
			return p.Name
		}
	}
	return ""
}

// link is Enter on a project of the host: the step that names the link,
// with a name that cannot be taken by a project here in the field. A project
// already linked is refused, and says under which name.
func (m Model) link() (tea.Model, tea.Cmd) {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return m, nil
	}
	if it.linked != "" {
		m.err = fmt.Errorf("%s on %s is already linked as %s", it.view.Project.Name, m.host, it.linked)
		return m, nil
	}
	m.lname.SetValue(linkName(m.host, it.view.Project.Name))
	m.lname.CursorEnd()
	m.proposed = true
	m.dialog = dialogLinkName
	m.rinput.Blur()
	return m, m.lname.Focus()
}

// linkName is the name a link is offered under: the host and the project
// there, behind "rs-", so a link does not take the name of a project here.
func linkName(host string, project revier.ProjectName) string {
	return "rs-" + host + "-" + string(project)
}

// linkNameKey is every press while the link is named. Enter writes it, Esc
// goes back to the host's projects, and everything else is the field's. The
// offered name is replaced by the first character typed, as a selected
// field's is; any other edit keeps it.
func (m Model) linkNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.dialog = dialogRemote
		m.lname.Blur()
		return m, m.rinput.Focus()
	case key.Matches(msg, m.keys.Enter):
		return m.writeLink()
	}
	if altRune(msg) {
		return m, nil
	}
	m.err = nil
	if m.proposed && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
		m.lname.SetValue("")
	}
	m.proposed = false
	in, cmd := m.lname.Update(msg)
	m.lname = in
	return m, cmd
}

// linkNameFault is why the name in the field cannot be written, or nil. It
// is asked on every frame, so a name that is taken says so as it is typed.
func (m Model) linkNameFault() error {
	name := m.linkNameValue()
	if name == "" {
		return fmt.Errorf("give the link a name")
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}
	if p, ok := m.project(name); ok {
		return fmt.Errorf("a project named %q exists here: %s", name, contractHome(p.File))
	}
	return nil
}

func (m Model) linkNameValue() revier.ProjectName {
	return revier.ProjectName(strings.TrimSpace(m.lname.Value()))
}

// writeLink is Enter on the name: a link file under it, which is then a row.
// A name that is taken writes nothing, so no project here is overwritten.
// The row is put in at once, as the next survey will show it, rather than a
// refresh later.
func (m Model) writeLink() (tea.Model, tea.Cmd) {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return m, nil
	}
	if err := m.linkNameFault(); err != nil {
		m.err = err
		return m, nil
	}
	root, err := config.Root()
	if err != nil {
		m.err = err
		return m, nil
	}
	p, err := config.CreateLink(root, m.linkNameValue(), m.host, it.view.Project)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.projects = append(m.projects, p)
	m.tkeys = targetKeys(m.projects, m.keys)
	// Provisional, until the survey answers: the host's own view, as the
	// merge would lay it over a local one with no pane here yet.
	view := it.view
	view.Project, view.Running, view.Home, view.Targets = p.Project, false, revier.TargetRef{}, nil
	m.views = sorted(append(m.views, view))
	m.dialog = dialogNone
	m.lname.Blur()
	m.reload()
	m.selectName(p.Name)
	return m, nil
}

// linkNameScreen is what stands in the list's place while the link is
// named: what Enter writes, or why it writes nothing.
func (m Model) linkNameScreen() string {
	th := m.theme
	w := m.listWidth()
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	if err := m.linkNameFault(); err != nil {
		return say(th.Attention, err.Error()) + "\n" +
			say(th.Meta, "Enter writes nothing until the name is free")
	}
	root, err := config.Root()
	if err != nil {
		return say(th.Attention, err.Error())
	}
	return say(th.NameDim, "Enter writes") + "\n" +
		say(th.Path, contractHome(config.ProjectFile(root, m.linkNameValue()))) + "\n" +
		say(th.Meta, fmt.Sprintf("a link to %s on %s", it.view.Project.Name, m.host))
}

// linkNameView is the field, with the offered name drawn as a selection
// until it is edited, because the first character typed replaces it.
func (m Model) linkNameView() string {
	in := m.lname
	if m.proposed {
		in.TextStyle = m.theme.OnSelection(m.theme.ProjectName)
	}
	return " " + in.View()
}

// newLinkNameInput is the field the link's name is typed in.
func newLinkNameInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.CharLimit = 128
	return in
}

// remoteDetail is the pane on the dialog's second step: what the host said
// about the project under the cursor.
func (m *Model) remoteDetail() string {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	th, v := m.theme, it.view
	w := m.paneCols() - paneChrome
	line := func(label, value string, style lipgloss.Style) string {
		return hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style) + "\n"
	}
	out := th.Header.Render(clipTo(string(v.Project.Name)+"@"+m.host, w)) + "\n"
	out += line("Path", v.Project.Path, th.Path)
	status, style := "stopped", th.NameDim
	switch {
	case v.Running:
		status, style = "running", th.Running
	case !v.PathExists:
		status, style = "not cloned", th.PathMissing
	}
	out += line("Status", status, style)
	if it.linked != "" {
		out += line("Linked as", string(it.linked), th.Remote)
	} else if m.dialog == dialogRemote {
		out += th.Meta.Render(clipTo("Enter: name a link to it here", w)) + "\n"
	}
	if len(v.Agents) > 0 {
		out += m.heading("Agents", w)
		for _, a := range v.Agents {
			out += m.detailAgent(agentRow{agent: a}, w, false, false) + "\n"
		}
	}
	return out
}

// askingLine is the footer while an ask is out.
func (m Model) askingLine() string {
	return m.theme.Meta.Render(fmt.Sprintf(" asking %s for its projects…", m.asking))
}
