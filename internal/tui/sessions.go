package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The sessions screen: the saved sets of open projects, newest first, as
// `revier session list` prints them. The pane says what restoring the one
// under the cursor would do here now, Enter restores it, and the save button
// records what is open now under an optional name. Both go through the core
// paths `revier session save` and `revier session restore` use.

// sessionsBarKey opens the screen from the surface, and on the screen saves.
// A second press is the name step, never a save on its own.
const sessionsBarKey = "alt+s"

// sessionActions are the screen's own buttons.
var sessionActions = []barAction{
	{label: "save", key: sessionsBarKey, run: Model.openSessionName},
}

// saveTimeout bounds a save: one survey, and one ask of each agent probe.
const saveTimeout = 30 * time.Second

// sessionItem is one row of the screen.
type sessionItem struct{ session session.Session }

func (i sessionItem) FilterValue() string { return i.session.ID }

// sessionDelegate draws a row as two columns, the id and the name, each as
// wide as the widest on the screen: two saves in one second have ids of two
// widths. What a session holds is the pane's.
type sessionDelegate struct {
	theme     theme.Theme
	idWidth   int
	nameWidth int
}

func (d sessionDelegate) Height() int                         { return 1 }
func (d sessionDelegate) Spacing() int                        { return 0 }
func (d sessionDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// maxSessionName is the widest the name column grows; a longer name is cut.
const maxSessionName = 24

// unnamed stands in the name column of a session saved without a name.
const unnamed = "—"

// Render draws the id and the name.
func (d sessionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(sessionItem)
	if !ok {
		return
	}
	th, s := d.theme, it.session
	sel := index == m.Index()
	style := func(st lipgloss.Style) lipgloss.Style {
		if sel {
			return th.OnSelection(st)
		}
		return st
	}
	cell := func(text string, width int, st lipgloss.Style) string {
		return style(st).Render(pad(ellipsis(text, width), width+2))
	}
	name, nameStyle := s.Name, th.Remote
	if name == "" {
		name, nameStyle = unnamed, th.NameDim
	}
	row := cursor(th, sel) + cell(s.ID, d.idWidth, th.ProjectName) +
		cell(name, d.nameWidth, nameStyle)
	_, _ = fmt.Fprint(w, fill(clipTo(row, m.Width()), m.Width(), style))
}

func sessionCounts(s session.Session) string {
	return core.Count(len(s.Projects), "project") + " · " + core.Count(s.Targets(), "target") +
		" · " + core.Count(s.Conversations(), "agent")
}

// sessionOutcome is what the last save or restore on the screen came to, for
// the pane of the session it was about.
type sessionOutcome struct {
	id       string
	saved    bool
	notes    []string // what the save could not record
	restored core.Restored
	back     error
}

// savedMsg is a save's answer.
type savedMsg struct {
	stored session.Session
	gaps   core.SessionGaps
	err    error
}

// restoredMsg is a restore's answer.
type restoredMsg struct {
	id       string
	restored core.Restored
	back     error
	err      error
}

var errNothingOpen = errors.New("nothing is open; no session saved")

func newSessionList(th theme.Theme) list.Model { return plainList(sessionDelegate{theme: th}) }

// newSessionNameInput is the field a session's name is typed in.
func newSessionNameInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = "a name, or none"
	in.CharLimit = 128
	return in
}

// openSessions is the "sessions" button and alt+s. The cursor comes back to
// the list first, so the surface the screen stands over is the one it is left
// on.
func (m Model) openSessions() (tea.Model, tea.Cmd) {
	m.err = nil
	m.toList()
	m.dialog = dialogSessions
	// What the last save or restore came to is about that visit, and the
	// plan is what a new visit is for.
	m.outcome = sessionOutcome{}
	m.loadSessions("")
	return m, nil
}

// loadSessions reads the saved sessions into the screen's rows, with the
// cursor on the session of that id, or on the newest.
func (m *Model) loadSessions(id string) {
	all, err := session.List(m.stateRoot)
	if err != nil {
		m.err = err
	}
	items := make([]list.Item, 0, len(all))
	at := 0
	for i, s := range all {
		items = append(items, sessionItem{session: s})
		if s.ID == id {
			at = i
		}
	}
	d := sessionDelegate{theme: m.theme, nameWidth: lipgloss.Width(unnamed)}
	for _, s := range all {
		d.idWidth = max(d.idWidth, len(s.ID))
		d.nameWidth = max(d.nameWidth, lipgloss.Width(s.Name))
	}
	d.nameWidth = min(d.nameWidth, maxSessionName)
	m.slist.SetDelegate(d)
	_ = m.slist.SetItems(items)
	m.slist.Select(at)
}

// sessionsKey is every press on the screen: up and down walk the sessions,
// Enter restores the one under the cursor, the save key names a new one, and
// Esc leaves.
func (m Model) sessionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.err = nil
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.dialog = dialogNone
	case key.Matches(msg, m.keys.Up):
		m.slist.CursorUp()
	case key.Matches(msg, m.keys.Down):
		m.slist.CursorDown()
	case key.Matches(msg, m.keys.Enter):
		return m.restoreSession()
	case msg.String() == sessionsBarKey:
		return m.openSessionName()
	}
	return m, nil
}

// openSessionName is the save button: the step that asks for a name.
func (m Model) openSessionName() (tea.Model, tea.Cmd) {
	m.err = nil
	if m.saving {
		m.err = errors.New("a save is still running")
		return m, nil
	}
	m.sname.SetValue("")
	m.dialog = dialogSessionName
	return m, m.sname.Focus()
}

// sessionNameKey is every press while the name is typed. Enter saves, Esc
// goes back to the sessions, and everything else is the field's.
func (m Model) sessionNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.dialog = dialogSessions
		m.sname.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.saveSession()
	}
	if altRune(msg) {
		return m, nil
	}
	m.err = nil
	in, cmd := m.sname.Update(msg)
	m.sname = in
	return m, cmd
}

// saveSession records every project open now, as `revier session save` does,
// off the terminal: the save asks each agent probe for its conversations.
func (m Model) saveSession() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.sname.Value())
	m.dialog = dialogSessions
	m.sname.Blur()
	m.saving = true
	c, projects, root := m.core, m.projects, m.stateRoot
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
		defer cancel()
		st := loadedState(root)
		report, err := c.Survey(ctx, projects, st.Bound, st.Attached)
		if err != nil {
			return savedMsg{err: err}
		}
		s, gaps := c.Session(ctx, report, st.Current)
		// An empty save would become the newest session, which a plain
		// restore opens: the save made before a reboot would lose to it.
		if len(s.Projects) == 0 {
			return savedMsg{err: errNothingOpen}
		}
		s.At, s.Name = time.Now(), name
		stored, path, err := session.Save(root, s)
		if err != nil {
			return savedMsg{err: err}
		}
		slog.Info("session saved", "id", stored.ID, "name", stored.Name, "path", path,
			"projects", len(stored.Projects), "targets", stored.Targets(), "conversations", stored.Conversations(),
			"unnamed_agents", gaps.Unnamed, "agents_in_tab", gaps.InTab, "attached_not_recorded", gaps.Attached)
		for _, err := range gaps.Failed {
			slog.Warn("session save: probe could not be asked", "err", err)
		}
		return savedMsg{stored: stored, gaps: gaps}
	}
}

// saved takes a save's answer: the new session is a row, under the cursor,
// and its pane says what the save could not record.
func (m Model) saved(msg savedMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.loadSessions(msg.stored.ID)
	m.outcome = sessionOutcome{id: msg.stored.ID, saved: true, notes: msg.gaps.Notes()}
	return m, nil
}

// restoreSession is Enter on a session: it opens what the session recorded,
// as `revier session restore` does, off the terminal. The restore walks one
// target at a time and waits for each window, so it can take a while, and a
// second one is refused while it runs.
func (m Model) restoreSession() (tea.Model, tea.Cmd) {
	it, ok := m.slist.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	if m.restoring != "" {
		m.err = fmt.Errorf("the restore of %s is still running", m.restoring)
		return m, nil
	}
	m.restoring = it.session.ID
	c, projects, root, s := m.core, m.projects, m.stateRoot, it.session
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		st := loadedState(root)
		report, err := c.Survey(ctx, projects, st.Bound, st.Attached)
		cancel()
		if err != nil {
			return restoredMsg{id: s.ID, err: err}
		}
		// No deadline over the whole walk: each launch bounds its own wait.
		out, back := c.Restore(context.Background(), s, report, projects, stateLedger{root: root})
		return restoredMsg{id: s.ID, restored: out, back: back}
	}
}

// restored takes a restore's answer. What each target came to is the pane's;
// a target that did not open, or a failure to return to the saved project, is
// the footer's too.
func (m Model) restored(msg restoredMsg) (tea.Model, tea.Cmd) {
	m.restoring = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.outcome = sessionOutcome{id: msg.id, restored: msg.restored, back: msg.back}
	if _, _, failed := msg.restored.Counts(); failed > 0 {
		m.err = fmt.Errorf("%s: %d targets did not open", msg.id, failed)
	} else if msg.back != nil {
		m.err = msg.back
	}
	return m, nil
}

// loadedState is state as a save or a restore starts from. One that cannot be
// read is empty: it costs a binding, not the operation.
func loadedState(root string) *state.State {
	if st, err := loadState(root); err == nil && st != nil {
		return st
	}
	return &state.State{}
}

// stateLedger is state on disk as a restore reads and writes it. It is read
// and written under the state's lock at every step, because the surface
// claims windows into the same file while the restore runs.
type stateLedger struct{ root string }

func (l stateLedger) Bound(p revier.ProjectName) core.Bindings {
	return loadedState(l.root).Bound[p]
}

func (l stateLedger) Pending(p revier.ProjectName, t revier.TargetName) bool {
	return loadedState(l.root).Launch.Pending(p, t, core.BindWindow)
}

func (l stateLedger) Launched(p revier.ProjectName, t revier.TargetName, at time.Time) {
	l.update(func(st *state.State) { st.Launched(p, t, at) })
}

func (l stateLedger) Landed(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef) {
	l.update(func(st *state.State) { st.Landed(p, t, ref) })
}

func (l stateLedger) update(apply func(st *state.State)) {
	_, err := state.Update(l.root, func(st *state.State) bool {
		apply(st)
		return true
	})
	if err != nil {
		slog.Warn("restore: state update", "err", err, "root", l.root)
	}
}

// sessionNameScreen is what stands in the list's place while the name is
// typed: what Enter records.
func (m Model) sessionNameScreen() string {
	th := m.theme
	w := m.listWidth()
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	running := 0
	for _, v := range m.views {
		if v.Running {
			running++
		}
	}
	return say(th.NameDim, "Enter saves the projects open now") + "\n" +
		say(th.Meta, core.Count(running, "project")+" open at the last survey") + "\n" +
		say(th.Meta, "a restore finds the session by its name too")
}

// sessionDetail is the pane on the screen: what the session under the cursor
// holds, what the save that wrote it could not record, and what restoring it
// would do here now, or what the restore of it came to.
func (m *Model) sessionDetail() string {
	it, ok := m.slist.SelectedItem().(sessionItem)
	if !ok {
		return ""
	}
	th, s := m.theme, it.session
	w := m.paneCols() - paneChrome
	var b strings.Builder
	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(hang(th.Meta.Render(pad(label, detailLabelWidth)), value, w, style) + "\n")
	}
	say := func(style lipgloss.Style, text string) {
		b.WriteString(hang("", text, w, style) + "\n")
	}
	title := s.ID
	if s.Name != "" {
		title = s.Name
	}
	b.WriteString(th.Header.Render(clipTo(title, w)) + "\n")
	line("Saved", s.At.Local().Format("2006-01-02 15:04"), th.Path)
	line("ID", s.ID, th.Path)
	if s.Current != "" {
		line("Ends on", string(s.Current), th.ProjectName)
	}
	line("File", contractHome(filepath.Join(session.Dir(m.stateRoot), s.ID+".toml")), th.Path)
	line("Holds", sessionCounts(s), th.Path)

	out := m.outcome
	if out.id == s.ID && out.saved {
		b.WriteString(m.heading("Saved", w))
		if len(out.notes) == 0 {
			say(th.Running, "every open target and agent conversation was recorded")
		}
		for _, note := range out.notes {
			say(th.Attention, note)
		}
	}
	switch {
	case m.restoring == s.ID:
		b.WriteString(m.heading("Restore", w))
		say(th.Meta, "restoring, one target at a time…")
	case out.id == s.ID && out.restored != nil:
		b.WriteString(m.heading("Restored", w))
		m.restoreSteps(&b, out.restored, w)
		opened, pending, failed := out.restored.Counts()
		summary := fmt.Sprintf("opened %d", opened)
		if pending > 0 {
			summary += fmt.Sprintf(", %d not up yet", pending)
		}
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		say(th.NameDim, "\n"+summary)
		if out.back != nil {
			say(th.Attention, out.back.Error())
		}
	case !m.surveyed:
		b.WriteString(m.heading("Restore plan", w))
		say(th.Meta, "surveying")
	default:
		b.WriteString(m.heading("Restore plan", w))
		m.restoreSteps(&b, m.core.RestorePreview(s, core.Report{Views: m.views}, m.projects), w)
		say(th.Meta, "\nEnter: restore")
	}
	return b.String()
}

// restoreSteps writes the plan, or what a restore did, grouped by project, on
// the pane's grid: each recorded target as the Targets section draws it, and
// under it each agent it held as the Agents section draws it, each behind the
// operation the restore makes of it - start it, keep the one open, or skip it.
func (m Model) restoreSteps(b *strings.Builder, steps core.Restored, w int) {
	th := m.spun()
	var project revier.ProjectName
	for _, r := range steps {
		if r.Project != project {
			if project != "" {
				b.WriteString("\n")
			}
			project = r.Project
			b.WriteString(th.ProjectName.Bold(true).Render(clipTo(string(r.Project), w)) + "\n")
		}
		tv, _ := m.targetView(r.Project, r.Target)
		mark, state, stateStyle := th.Glyphs.Stopped+" stopped", "", th.Count
		switch {
		case !tv.Ref.IsZero():
			mark, stateStyle = th.Glyphs.Running+" running", th.Running
		case r.Action == core.RestoreNoHost:
			mark = "no host here"
		}
		op, reason := targetOp(th, r)
		b.WriteString(planRow(th, op, th.ProjectName.Render(string(r.Target)), stateStyle.Render(mark+state),
			reason, th.PathMissing, w) + "\n")
		live := m.liveAgents(r.Project, tv.Ref)
		for i, a := range r.Resumes {
			b.WriteString(m.planAgent(th, r, i, a, live, w) + "\n")
		}
	}
}

// planAgent is one recorded agent of a step: its operation, then the harness,
// state and activity of the agent now running in its place, or, where none
// runs, what the restore said about it or the conversation it holds.
func (m Model) planAgent(th theme.Theme, r core.RestoreResult, i int, a core.Resume, live []revier.AgentView, w int) string {
	op, reason := agentOp(th, r, i)
	harness := a.Harness
	if harness == "" {
		harness = "agent"
	}
	if i < len(live) {
		s := live[i].State
		return planRow(th, op, th.ProjectName.Render(harness),
			statusStyle(th, s.Status).Render(statusLabel(th, s.Status)), s.Activity, th.Path, w)
	}
	text, style := reason, th.PathMissing
	if text == "" {
		text, style = string(a.Session), th.Path
		if text == "" {
			text = "no conversation recorded"
		}
		if p, ok := m.project(r.Project); ok && a.Dir != "" && a.Dir != p.Path {
			text += " in " + contractHome(a.Dir)
		}
	}
	return planRow(th, op, th.ProjectName.Render(harness), th.Count.Render(th.Glyphs.Stopped+" stopped"), text, style, w)
}

// planRow is a row of the Targets or Agents section behind an operation: the
// operation, the name, the state, and the rest wrapped under itself.
func planRow(th theme.Theme, op, name, state, rest string, restStyle lipgloss.Style, w int) string {
	lead := " " + gridCell(op, planOpWidth, lipgloss.NewStyle())
	head := clipTo(gridHead(lead, name, state, lipgloss.NewStyle()), w)
	parts := wrap(rest, w-lipgloss.Width(head))
	if len(parts) == 0 {
		return head
	}
	indent := strings.Repeat(" ", lipgloss.Width(head))
	for i := range parts {
		parts[i] = restStyle.Render(parts[i])
	}
	return head + strings.Join(parts, "\n"+indent)
}

// planOpWidth fits a glyph, a space, the longest operation word, "launched",
// and a gap. A Nerd Font glyph can draw two cells wide, so one more.
const planOpWidth = 12

// targetOp is what a restore does, or did, with a recorded target, and why it
// is skipped when it is.
func targetOp(th theme.Theme, r core.RestoreResult) (op, reason string) {
	tense := planTense(r)
	switch {
	case r.Action == core.RestoreRunning:
		return keepOp(th, tense("keep", "kept")), ""
	case r.Action != core.RestoreLaunch:
		return skipOp(th, tense("skip", "skipped")), r.Action.String()
	case r.Planned():
		return startOp(th, "open"), ""
	case r.Err != nil:
		return skipOp(th, "failed"), r.Err.Error()
	case r.Ref.IsZero():
		return startOp(th, "launched"), "not up yet"
	}
	return startOp(th, "opened"), ""
}

// agentOp is what a restore does, or did, with a step's i-th recorded agent,
// and what is worth saying about it where it does not start as recorded.
func agentOp(th theme.Theme, r core.RestoreResult, i int) (op, reason string) {
	tense := planTense(r)
	switch {
	case r.Action == core.RestoreRunning:
		return keepOp(th, tense("keep", "kept")), ""
	case r.Action != core.RestoreLaunch, i >= len(r.Agents):
		return skipOp(th, tense("skip", "skipped")), "its target is not opened"
	}
	switch r.Agents[i] {
	case core.AgentResumed:
		return startOp(th, tense("resume", "resumed")), ""
	case core.AgentEmpty:
		return startOp(th, tense("start", "started")), ""
	case core.AgentDirGone:
		return startOp(th, tense("start", "started")), "empty: its directory is gone"
	case core.AgentUnresumable:
		return startOp(th, tense("start", "started")), "empty: its harness cannot resume here"
	case core.AgentInTab:
		return startOp(th, tense("start", "started")), "without its conversation: it ran in a tab"
	case core.AgentDropped:
		return skipOp(th, tense("skip", "skipped")), "no agent panel to start it in"
	}
	return skipOp(th, tense("skip", "skipped")), r.Agents[i].String()
}

// planTense picks the word for a planned step or for one a restore walked.
func planTense(r core.RestoreResult) func(planned, done string) string {
	return func(planned, done string) string {
		if r.Planned() {
			return planned
		}
		return done
	}
}

func startOp(th theme.Theme, word string) string {
	return th.Running.Render(th.Glyphs.Start + " " + word)
}
func keepOp(th theme.Theme, word string) string {
	return th.NameDim.Render(th.Glyphs.Keep + " " + word)
}
func skipOp(th theme.Theme, word string) string {
	return th.PathMissing.Render(th.Glyphs.Skip + " " + word)
}

// targetView is the survey's view of one target of a project.
func (m Model) targetView(project revier.ProjectName, target revier.TargetName) (revier.TargetView, bool) {
	for _, v := range m.views {
		if v.Project.Name != project {
			continue
		}
		for _, tv := range v.Targets {
			if !tv.Attached && tv.Name == target {
				return tv, true
			}
		}
	}
	return revier.TargetView{}, false
}

// liveAgents are the agents running in an instance of a project now, in the
// order the runtime lists them, which is the order a session records them in.
func (m Model) liveAgents(project revier.ProjectName, ref revier.TargetRef) []revier.AgentView {
	if ref.IsZero() {
		return nil
	}
	var out []revier.AgentView
	for _, v := range m.views {
		if v.Project.Name != project {
			continue
		}
		for _, a := range v.Agents {
			if a.Ref.Host == ref.Host && a.Ref.ID == ref.ID {
				out = append(out, a)
			}
		}
	}
	return out
}

// progressLine is the footer while a save or a restore runs.
func (m Model) progressLine() string {
	if m.restoring != "" {
		return m.theme.Meta.Render(fmt.Sprintf(" restoring %s…", m.restoring))
	}
	return m.theme.Meta.Render(" saving the projects open now…")
}
