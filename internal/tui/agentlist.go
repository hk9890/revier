package tui

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The agent list is the surface's second list: every agent of every project,
// one row each, the ones that need the user first. The project list answers
// "which project", and this one "which agent", which is the question when
// thirty of them run across ten projects. The key that opens the surface
// switches between the two, and the surface always opens on the projects
// (decisions.md D110).
//
// A row is the agent's state, what it is on, and how long ago it spoke, over
// its project and the directory it works in. Beside the list the pane names
// the agent and mirrors its terminal (mirror.go).

// switchChord is the key that switches between the two lists: alt+space, the
// default of the desktop key that opens the surface. On a desktop that takes
// that key before the terminal sees it, `revier popup` types it into the
// popup instead (core.SwitchKey).
const switchChord core.Chord = "alt+space"

// switches reports the press that switches between the lists.
func switches(msg tea.KeyMsg) bool {
	c, ok := pressed(msg)
	return ok && c == switchChord
}

// agentItem is one row of the agent list: an agent, the project a panel of
// which shows it, and what it said last. It is carried whole, so the delegate
// renders from the item and looks nothing up: a row is drawn on every frame,
// and the summary of an agent with no title is read out of its whole message.
type agentItem struct {
	agent   revier.AgentView
	project revier.Project
	detail  revier.AgentDetail
	text    string // what it is on (summary)
	titled  bool   // whether the agent said so itself
	age     string // how long ago it spoke, as the row says it
}

// listedKey names one row of the agent list across refreshes. An agent a
// user attached to two projects is a row under each, so its panel alone does
// not name the row.
type listedKey struct {
	project revier.ProjectName
	agent   agentKey
}

func (i agentItem) key() listedKey {
	return listedKey{project: i.project.Name, agent: keyOf(i.agent)}
}

// summary is what an agent is on, and whether the agent said so itself: its
// activity line, which a harness writes as its panel's title. An agent with
// none yet shows the first line of what it said last, and one that has said
// nothing its harness, so no row is blank.
func summary(a revier.AgentView, said revier.AgentDetail) (text string, titled bool) {
	if a.State.Activity != "" {
		return a.State.Activity, true
	}
	for _, line := range strings.Split(plainText(said.Message), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line, false
		}
	}
	return harnessOf(a), false
}

// where is the row's second line: the project, and the directory the agent
// works in when it is not the project's own, which is how a worktree shows.
// Whether it is the project's own is the core's to say, on the machine that
// read the agent: a view carries the directory only when it is not.
func (i agentItem) where() string {
	out := i.project.Label()
	if dir := i.agent.State.Dir; dir != "" {
		out += " · " + filepath.Base(dir)
	}
	return out
}

// FilterValue is what the query matches: what the agent is on, then where.
func (i agentItem) FilterValue() string {
	return i.text + " " + i.where()
}

// agentList is the surface's second list, and the mirror beside it.
type agentList struct {
	shown  bool            // the surface shows it in the project list's place
	list   list.Model      // its rows
	query  textinput.Model // its query, with its own cursor
	filter string          // that query, held here so a refresh can re-apply it
	top    bool            // it opened and the user has not acted on it: the cursor is on the first row whatever agent that is
	mirror mirror          // the screen of the agent under the cursor (mirror.go)
}

func newAgentScreen(th theme.Theme) agentList {
	return agentList{list: newAgentList(th), query: newPrompt(th, agentPlaceholder)}
}

// agentsResult is what a press on the agent list leaves for the surface.
type agentsResult struct {
	leave bool       // Esc with no query: the surface's own leave
	close *agentItem // del on a row: the agent whose tab is to close
	rest  bool       // the press is not the list's own: a key of the bar, or text for the query
}

func newAgentList(th theme.Theme) list.Model {
	l := newProjectList(th)
	l.SetDelegate(agentDelegate{theme: th, hover: -1})
	return l
}

// agentDelegate renders one row of the agent list. hover is the row the
// pointer is on, or -1, as on the project list's delegate.
type agentDelegate struct {
	theme theme.Theme
	hover int
}

// Height is two, as a project row's: the agent, and where it works under it.
func (d agentDelegate) Height() int                         { return 2 }
func (d agentDelegate) Spacing() int                        { return 0 }
func (d agentDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// listedStateWidth is the state column of an agent row: the widest state, a
// glyph and "needs you", and a gap.
const listedStateWidth = 13

func (d agentDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(agentItem)
	if !ok {
		return
	}
	th := d.theme
	sel := index == m.Index()
	over := index == d.hover && !sel
	style := func(s lipgloss.Style) lipgloss.Style {
		switch {
		case sel:
			return th.OnSelection(s)
		case over:
			return th.OnHover(s)
		}
		return s
	}
	space := func(n int) string { return style(th.Path).Render(strings.Repeat(" ", max(n, 0))) }
	cell := func(s string, w int) string { return s + space(w-lipgloss.Width(s)) }

	width := m.Width()
	bar := space(1)
	if sel {
		bar = th.Cursor.Render(th.Glyphs.Cursor)
	}
	prefix := bar + space(1)
	indent := lipgloss.Width(prefix) + listedStateWidth

	// The state leads, because it is what the list is sorted by; the age
	// stands at the right edge, in a column of its own, so the order within
	// a state reads down it.
	status := it.agent.State.Status
	state := cell(style(statusStyle(th, status)).Render(clipTo(statusLabel(th, status), listedStateWidth-1)), listedStateWidth)
	age := ""
	if it.age != "" {
		age = space(gridGap) + style(th.Meta).Render(it.age) + space(1)
	}
	room := width - indent - lipgloss.Width(age)

	matches := m.MatchesForItem(index)
	name := th.ProjectName
	if !it.titled {
		name = th.NameDim
	}
	if sel {
		name = name.Bold(true)
	}
	shown := ellipsis(it.text, room)
	first := prefix + state + cell(highlight(shown, within(matches, 0, len(shown)), style(name), style(th.Match)), room) + age

	where := ellipsis(it.where(), width-indent)
	second := bar + space(indent-1) +
		highlight(where, within(matches, len(it.text)+1, len(where)), style(th.Path), style(th.Match))

	// The list renders into a strings.Builder, which cannot fail.
	_, _ = fmt.Fprint(w, fill(first, width, style)+"\n"+fill(second, width, style))
}

// listedRank is the order of the states on the agent list: the agents that
// need the user, the ones working, the ones at rest, and the ones whose state
// nothing says.
var listedRank = map[revier.Status]int{
	revier.StatusAttention: 0,
	revier.StatusRunning:   1,
	revier.StatusIdle:      2,
	revier.StatusUnknown:   3,
}

// agentItems is every agent on the surface as the agent list orders them. An
// agent that needs the user or is at rest stands by when it spoke, the latest
// first: the one that just stopped is the one to look at. A working agent
// speaks all the time, and a row that moved on every line would not be
// readable, so those stand by project and panel, and so does an agent whose
// state or time nothing says (decisions.md D110).
func agentItems(views []revier.ProjectView, details map[agentKey]revier.AgentDetail, now time.Time) []list.Item {
	var items []agentItem
	for _, v := range views {
		for _, a := range v.Agents {
			d := details[keyOf(a)]
			text, titled := summary(a, d)
			items = append(items, agentItem{agent: a, project: v.Project, detail: d, text: text, titled: titled, age: shortAgo(now, d.At)})
		}
	}
	timed := func(s revier.Status) bool { return s == revier.StatusAttention || s == revier.StatusIdle }
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := listedRank[a.agent.State.Status], listedRank[b.agent.State.Status]; ra != rb {
			return ra < rb
		}
		if at, bt := a.detail.At, b.detail.At; timed(a.agent.State.Status) && !at.Equal(bt) {
			return !at.IsZero() && (bt.IsZero() || at.After(bt))
		}
		return a.project.Name < b.project.Name
	})
	out := make([]list.Item, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}

// shortAgo is how long before now a moment was, as a row's age column says
// it: one number and one letter. A moment nothing says is no age at all.
func shortAgo(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

// reloadAgents puts the current survey and what the agents said into the
// agent list, keeping the query. The cursor stays on its agent through every
// reordering, so the mirror beside it does not change under the reader; an
// agent that left hands the cursor to the row that took its place, as a
// project that closed does (decisions.md D109, D110).
func (m *Model) reloadAgents() {
	if m.agents.shown {
		m.agents.reload(agentItems(m.views, m.adetails, m.now()))
	}
}

// reload puts items into the list, keeping the query and the cursor's agent.
func (al *agentList) reload(items []list.Item) {
	was, had := al.selected()
	at := al.list.Index()
	al.list.Filter = ranked(func(i int) int { return listedRank[items[i].(agentItem).agent.State.Status] })
	_ = al.list.SetItems(items)
	if al.filter != "" {
		al.list.SetFilterText(al.filter)
	}
	if !had || !al.selectKey(was.key()) {
		al.list.Select(clampRow(at, len(al.list.VisibleItems())))
	}
}

// selected is the row under the cursor.
func (al *agentList) selected() (agentItem, bool) {
	it, ok := al.list.SelectedItem().(agentItem)
	return it, ok
}

// selectKey puts the cursor on a row, and reports whether the list holds it.
func (al *agentList) selectKey(key listedKey) bool {
	for i, item := range al.list.VisibleItems() {
		if it, ok := item.(agentItem); ok && it.key() == key {
			al.list.Select(i)
			return true
		}
	}
	return false
}

// switchList is the switch key and its button: the other list takes the
// surface. The agent list opens on its first row, the agent that asked for
// the user last, and with no query; the project list is as it was left.
//
// Which row is the first is known once every agent's last word is read, and
// that answer comes after the list is drawn. The cursor stays on the first
// row until it has come, unless the user moves it before (took).
func (m Model) switchList() (tea.Model, tea.Cmd) {
	m.err = nil
	m.toList()
	m.agents.shown = !m.agents.shown
	if !m.agents.shown {
		return m, nil
	}
	m.agents.setFilter("")
	m.reloadAgents()
	m.agents.list.Select(0)
	m.agents.top = true
	m.body.SetYOffset(0)
	return m, m.agents.query.Focus()
}

// switchButton is the bar's first button: the list the switch key goes to,
// and the key. In the popup the key is the desktop's, which a configuration
// can change.
func (m Model) switchButton() barAction {
	label := "agents"
	if m.agents.shown {
		label = "projects"
	}
	return barAction{label: label, key: m.switchKey(), run: Model.switchList}
}

// switchKey is the key that switches lists, as the user presses it: the
// desktop's trigger key in the popup, and alt+space in any other terminal.
func (m Model) switchKey() string {
	if m.popup {
		if trigger, err := (&config.Config{UI: m.config.ui}).TriggerKey(); err == nil {
			return string(trigger)
		}
	}
	return string(switchChord)
}

// agentsKey is every press on the agent list, and what it left for the
// surface. The rows are agents, so Enter goes to one and del closes one; the
// keys that act on a project - its screen, its targets, the actions - have no
// row to act on here.
func (m Model) agentsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	res, cmd := m.agents.key(m.surface(), msg)
	switch {
	case res.leave:
		return m, m.leave()
	case res.close != nil:
		a := res.close.agent
		return m.closeRow(res.close.project.Name, core.CloseRow{Agent: a.Ref, Panel: a.Panel}, harnessOf(a), false)
	case !res.rest:
		return m, cmd
	}
	if switches(msg) {
		return m.switchList()
	}
	if next, cmd, ok := m.barKey(msg); ok {
		return next, cmd
	}
	cmd = m.agents.edit(msg)
	return m, cmd
}

// key is every press the list itself takes: the movement keys, Enter, del
// and Esc.
func (al *agentList) key(sf surface, msg tea.KeyMsg) (agentsResult, tea.Cmd) {
	al.top = false
	if by, ok := sf.keys.move(msg, func(int) int { return sf.page }); ok {
		moveRow(&al.list, by)
		return agentsResult{}, nil
	}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return agentsResult{}, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		if al.filter != "" {
			al.endSearch()
			return agentsResult{}, nil
		}
		return agentsResult{leave: true}, nil
	case key.Matches(msg, sf.keys.Enter):
		return agentsResult{}, al.goSelected(sf)
	case key.Matches(msg, sf.keys.Close) && atEnd(&al.query):
		if it, ok := al.selected(); ok {
			return agentsResult{close: &it}, nil
		}
		return agentsResult{}, nil
	}
	return agentsResult{rest: true}, nil
}

// edit feeds a key to the query, and filters again if it changed.
func (al *agentList) edit(msg tea.KeyMsg) tea.Cmd {
	if !promptKey(msg) {
		return nil
	}
	next, cmd := al.query.Update(msg)
	al.query = next
	if next.Value() != al.filter {
		al.setFilter(next.Value())
	}
	return cmd
}

// setFilter is every change to the query. The first match is selected, as on
// a project query.
func (al *agentList) setFilter(q string) {
	al.filter = q
	if al.query.Value() != q {
		al.query.SetValue(q)
	}
	if q != "" {
		al.list.SetFilterText(q)
		return
	}
	al.list.ResetFilter()
}

// endSearch drops the query and leaves the cursor on the agent the search
// led to.
func (al *agentList) endSearch() {
	was, had := al.selected()
	al.setFilter("")
	if had {
		al.selectKey(was.key())
	}
}

// goSelected is Enter, or a double click, on a row: the agent's tab comes to
// the front, as from the pane's Agents, and the query that found it ends.
func (al *agentList) goSelected(sf surface) tea.Cmd {
	it, ok := al.selected()
	if !ok {
		return nil
	}
	p, ok := projectNamed(sf.projects, it.project.Name)
	if !ok {
		return nil
	}
	al.endSearch()
	return goAgent(sf.core, p, it.agent)
}

// goAgent brings an agent to the front: the panel here that shows it, for a
// link's agent too, unless the link's workspace is still coming up.
func goAgent(c *core.Core, p core.Project, agent revier.AgentView) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), core.BindWait)
		defer cancel()
		_, err := c.ActivateAgentWaiting(ctx, p, agent)
		return actedMsg{err: err}
	}
}

// pane is the pane beside the list, rows lines high: the agent under the
// cursor, then its terminal.
func (al *agentList) pane(sf surface, now time.Time, rows int) string {
	it, ok := al.selected()
	if !ok {
		return ""
	}
	facts := agentFacts(sf.spun, it, now, sf.pane)
	return facts + al.mirrorView(sf.theme, sf.pane, rows-strings.Count(facts, "\n"))
}

// agentFacts is the head of the agent list's pane, laid out as a project's
// is: the agent in the title's place, and its project among the lines under
// it. It ends in a blank line, which sets the mirror off.
func agentFacts(th theme.Theme, it agentItem, now time.Time, w int) string {
	var b strings.Builder
	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style))
		b.WriteString("\n")
	}

	b.WriteString(clipTo(th.Meta.Render(pad("Agent", detailLabelWidth))+th.Header.Render(it.text), w))
	b.WriteString("\n")
	b.WriteString(th.Border.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	status := it.agent.State.Status
	line("Status", statusLabel(th, status), statusStyle(th, status))
	line("Project", it.project.Label(), th.ProjectName)
	if it.project.Remote != nil {
		line("Host", it.project.Remote.Host, th.Path)
	}
	path := it.project.Path
	if it.agent.State.Dir != "" {
		path = it.agent.State.Dir
	}
	line("Path", config.ContractHome(path), th.Path)
	if it.project.GitURL != "" {
		line("Git URL", it.project.GitURL, th.Path)
	}
	line("Harness", harnessOf(it.agent), th.Path)
	if at := it.detail.At; !at.IsZero() {
		line("Last", spokeAt(now, at), th.Path)
	}
	b.WriteString("\n")
	return b.String()
}

// spokeAt is when an agent spoke last, for the pane: the time of day, the
// day too when it was not today, and how long ago.
func spokeAt(now, t time.Time) string {
	t = t.In(now.Location())
	layout := "15:04"
	if y, mo, d := t.Date(); y != now.Year() || mo != now.Month() || d != now.Day() {
		layout = "2 Jan 15:04"
	}
	return t.Format(layout) + ", " + ago(now, t)
}

// totals is the agents under the rule counted by state, for the totals the
// rule carries: the rows the query left.
func (al *agentList) totals() map[revier.Status]int {
	counts := map[revier.Status]int{}
	for _, item := range al.list.VisibleItems() {
		if it, ok := item.(agentItem); ok {
			counts[it.agent.State.Status]++
		}
	}
	return counts
}

// help is the footer on the agent list.
func (al *agentList) help(k keyMap) []key.Binding {
	return []key.Binding{
		helpKey("enter", "go to agent"),
		helpKey("type", "filter"),
		helpKey("esc", "clear/quit"),
		k.Close,
		k.Quit,
	}
}
