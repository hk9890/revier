package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/sshconfig"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The link dialog (decisions.md D42): alt+r lists the hosts the ssh
// configuration names, Enter on one asks the revier there for its projects,
// and Enter on a project writes a link to it here, which is then a row like
// any other. Two levels over the same list and pane the other two use.

// askTimeout bounds one ask of a host. Long enough for a cold ssh; short
// enough that a host that is down is a message, not a wait.
const askTimeout = 15 * time.Second

// hostItem is one row at the host level.
type hostItem struct{ host string }

func (i hostItem) FilterValue() string { return i.host }

// remoteItem is one row at the remote level: a project on the host, and the
// link here that already points at it, if any.
type remoteItem struct {
	view   revier.ProjectView
	linked revier.ProjectName
}

func (i remoteItem) FilterValue() string { return string(i.view.Project.Name) }

// askedMsg is a host's answer to the dialog's ask, or why it gave none.
type askedMsg struct {
	host  string
	views []revier.ProjectView
	err   error
}

func newHostList(th theme.Theme) list.Model   { return plainList(hostDelegate{theme: th}) }
func newRemoteList(th theme.Theme) list.Model { return plainList(remoteDelegate{theme: th}) }

// plainList is a list with nothing of its own on screen and no filter, as
// the target list is: the dialog's rows are few and each is a choice.
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
	bar, name := cursor(th, sel), th.ProjectName
	if sel {
		name = th.OnSelection(name)
	}
	row := bar + th.Remote.Render(th.Glyphs.Remote) + " " + name.Render(it.host)
	_, _ = fmt.Fprint(w, fill(row, m.Width(), sel, th))
}

type remoteDelegate struct{ theme theme.Theme }

func (d remoteDelegate) Height() int                         { return 1 }
func (d remoteDelegate) Spacing() int                        { return 0 }
func (d remoteDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d remoteDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(remoteItem)
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
	v := it.view
	mark, markStyle := th.Glyphs.Stopped, th.NameDim
	if v.Running {
		mark, markStyle = th.Glyphs.Running, th.Running
	}
	// A project already linked is grey: Enter on it has nothing to write.
	name, path := th.ProjectName, th.Path
	note := ""
	if it.linked != "" {
		name = th.NameDim
		note = "linked as " + string(it.linked)
	}
	row := cursor(th, sel) +
		style(markStyle).Render(mark) + style(th.Path).Render(" ") +
		style(name).Render(pad(string(v.Project.Name), nameWidth)) +
		style(path).Render(pad(elide(contractHome(v.Project.Path), remotePathWidth), remotePathWidth+1)) +
		style(th.Meta).Render(note)
	_, _ = fmt.Fprint(w, fill(row, m.Width(), sel, th))
}

// remotePathWidth is the room a project's path on the host has beside its
// name, so the note after it stays on a narrow list.
const remotePathWidth = 28

// cursor is the bar down the left of a row, lit on the selected one.
func cursor(th theme.Theme, sel bool) string {
	if sel {
		return th.Cursor.Render(th.Glyphs.Cursor) + th.OnSelection(th.Path).Render(" ")
	}
	return th.Path.Render("  ")
}

// openHosts is alt+r: the host level, over the hosts the ssh configuration
// names. No hosts is a message, not an empty list to be puzzled at.
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
	m.level = levelHosts
	return m, nil
}

// askHost is Enter at the host level: the host is asked for its projects,
// off the terminal, and the answer opens the remote level.
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

// asked takes a host's answer. A failure stays at the host level, with the
// failure in the footer; an answer is the remote level.
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
	_ = m.rlist.SetItems(items)
	m.rlist.Select(0)
	m.level = levelRemote
	if len(items) == 0 {
		m.err = fmt.Errorf("%s has no projects; `revier new` there writes one", msg.host)
	}
	return m, nil
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

// link is Enter at the remote level: a link file for the project under the
// cursor, under its own name, which is then a row. The row is put in at
// once, as the next survey will show it, rather than a refresh later.
func (m Model) link() (tea.Model, tea.Cmd) {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return m, nil
	}
	if it.linked != "" {
		m.err = fmt.Errorf("%s on %s is already linked as %s", it.view.Project.Name, m.host, it.linked)
		return m, nil
	}
	name := it.view.Project.Name
	if p, ok := m.project(name); ok {
		m.err = fmt.Errorf("project %q already exists: %s; link it as another name with `revier link %s %s --name <name>`", name, contractHome(p.File), m.host, name)
		return m, nil
	}
	root, err := config.Root()
	if err != nil {
		m.err = err
		return m, nil
	}
	p, err := config.CreateLink(root, name, m.host, name)
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
	m.level = levelProjects
	m.reload()
	m.selectName(p.Name)
	return m, nil
}

// remoteDetail is the pane at the remote level: what the host said about
// the project under the cursor.
func (m *Model) remoteDetail() string {
	it, ok := m.rlist.SelectedItem().(remoteItem)
	if !ok {
		return ""
	}
	th, v := m.theme, it.view
	w := m.paneWidth() - paneChrome
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
	} else {
		out += th.Meta.Render(clipTo("Enter: link it here as "+string(v.Project.Name), w)) + "\n"
	}
	if len(v.Agents) > 0 {
		out += m.heading("Agents", w)
		for _, a := range v.Agents {
			out += m.detailAgent(a, w) + "\n"
		}
	}
	return out
}

// askingLine is the footer while an ask is out.
func (m Model) askingLine() string {
	return m.theme.Meta.Render(fmt.Sprintf(" asking %s for its projects…", m.asking))
}
