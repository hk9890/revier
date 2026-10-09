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

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/session"
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
	{label: "save", key: sessionsBarKey, run: Model.nameSession},
}

// saveWait bounds a save: one survey, and one ask of each agent probe.
const saveWait = 30 * time.Second

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

// sessionsScreen is the sessions screen, and the save or the restore it
// started, which outlives the visit.
type sessionsScreen struct {
	naming    bool            // the step that asks a new session's name
	list      list.Model      // the saved sessions
	name      textinput.Model // the name field of a session being saved
	saving    bool            // whether a save is out
	restoring string          // the session a restore is walking, while it is
	outcome   sessionOutcome  // what the last save or restore came to
}

func newSessionsScreen(th theme.Theme) sessionsScreen {
	return sessionsScreen{list: newSessionList(th), name: newSessionNameInput(th)}
}

// sessionsResult is what a press on the screen leaves for the surface.
type sessionsResult struct {
	err    error // the footer's
	closed bool  // Esc on the sessions: back to the surface
}

// openSessions is the "sessions" button and alt+s. The cursor comes back to
// the list first, so the surface the screen stands over is the one it is left
// on.
func (m Model) openSessions() (tea.Model, tea.Cmd) {
	m.toList()
	m.dialog = dialogSessions
	m.err = m.sessions.open(m.stateRoot, m.theme)
	return m, nil
}

// sessionsKey is every press on the screen, and what it left for the surface.
func (m Model) sessionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res, cmd := m.sessions.key(m.surface(), msg)
	m.err = res.err
	if res.closed {
		m.dialog = dialogNone
	}
	return m, cmd
}

// nameSession is the screen's save button.
func (m Model) nameSession() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cmd, m.err = m.sessions.openName()
	return m, cmd
}

// saved takes a save's answer.
func (m Model) saved(msg savedMsg) (tea.Model, tea.Cmd) {
	if err := m.sessions.saved(m.stateRoot, m.theme, msg); err != nil {
		m.err = err
	}
	return m, nil
}

// restored takes a restore's answer.
func (m Model) restored(msg restoredMsg) (tea.Model, tea.Cmd) {
	if err := m.sessions.restored(msg); err != nil {
		m.err = err
	}
	return m, nil
}

// open is a new visit. What the last save or restore came to is about the
// visit before, and the plan is what a new visit is for.
func (sc *sessionsScreen) open(root string, th theme.Theme) error {
	sc.naming = false
	sc.outcome = sessionOutcome{}
	return sc.load(root, th, "")
}

// load reads the saved sessions under root into the screen's rows, with the
// cursor on the session of that id, or on the newest.
func (sc *sessionsScreen) load(root string, th theme.Theme, id string) error {
	all, err := session.List(root)
	items := make([]list.Item, 0, len(all))
	at := 0
	for i, s := range all {
		items = append(items, sessionItem{session: s})
		if s.ID == id {
			at = i
		}
	}
	d := sessionDelegate{theme: th, nameWidth: lipgloss.Width(unnamed)}
	for _, s := range all {
		d.idWidth = max(d.idWidth, len(s.ID))
		d.nameWidth = max(d.nameWidth, lipgloss.Width(s.Name))
	}
	d.nameWidth = min(d.nameWidth, maxSessionName)
	sc.list.SetDelegate(d)
	_ = sc.list.SetItems(items)
	sc.list.Select(at)
	return err
}

// key is every press on the screen: up and down walk the sessions, Enter
// restores the one under the cursor, the save key names a new one, and Esc
// leaves.
func (sc *sessionsScreen) key(sf surface, msg tea.KeyMsg) (sessionsResult, tea.Cmd) {
	if sc.naming {
		return sc.nameKey(sf, msg)
	}
	var res sessionsResult
	var cmd tea.Cmd
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.closed = true
	case key.Matches(msg, sf.keys.Up):
		sc.list.CursorUp()
	case key.Matches(msg, sf.keys.Down):
		sc.list.CursorDown()
	case key.Matches(msg, sf.keys.Enter):
		cmd, res.err = sc.restore(sf)
	case msg.String() == sessionsBarKey:
		cmd, res.err = sc.openName()
	}
	return res, cmd
}

// openName is the save button: the step that asks for a name.
func (sc *sessionsScreen) openName() (tea.Cmd, error) {
	if sc.saving {
		return nil, errors.New("a save is still running")
	}
	// A save during a restore records a desktop half restored, and it would
	// be the newest session, the one a plain restore opens.
	if sc.restoring != "" {
		return nil, fmt.Errorf("the restore of %s is still running; save once it is done", sc.restoring)
	}
	sc.name.SetValue("")
	sc.naming = true
	return sc.name.Focus(), nil
}

// nameKey is every press while the name is typed. Enter saves, Esc goes back
// to the sessions, and everything else is the field's.
func (sc *sessionsScreen) nameKey(sf surface, msg tea.KeyMsg) (sessionsResult, tea.Cmd) {
	res := sessionsResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Quit):
		return res, tea.Quit
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		sc.naming = false
		sc.name.Blur()
		return res, nil
	case key.Matches(msg, sf.keys.Enter):
		return res, sc.save(sf)
	}
	if altRune(msg) {
		return res, nil
	}
	res.err = nil
	in, cmd := sc.name.Update(msg)
	sc.name = in
	return res, cmd
}

// save records every project open now, as `revier session save` does, off
// the terminal: the save asks each agent probe for its conversations.
func (sc *sessionsScreen) save(sf surface) tea.Cmd {
	name := strings.TrimSpace(sc.name.Value())
	sc.naming = false
	sc.name.Blur()
	sc.saving = true
	c, projects, root := sf.core, sf.projects, sf.stateRoot
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), saveWait)
		defer cancel()
		report, err := c.Survey(ctx, projects)
		if err != nil {
			return savedMsg{err: err}
		}
		stored, _, gaps, err := c.SaveSession(ctx, root, report, c.Ledger.State().Current, name, time.Now())
		if err != nil {
			return savedMsg{err: err}
		}
		return savedMsg{stored: stored, gaps: gaps}
	}
}

// saved takes a save's answer: the new session is a row, under the cursor,
// and its pane says what the save could not record.
func (sc *sessionsScreen) saved(root string, th theme.Theme, msg savedMsg) error {
	sc.saving = false
	switch {
	case errors.Is(msg.err, core.ErrNothingOpen):
		// A normal outcome, as the CLI prints it, and not a failure.
		slog.Info("session save: nothing open")
		return msg.err
	case msg.err != nil:
		slog.Error("session save", "err", msg.err)
		return msg.err
	}
	err := sc.load(root, th, msg.stored.ID)
	sc.outcome = sessionOutcome{id: msg.stored.ID, saved: true, notes: msg.gaps.Notes()}
	return err
}

// restore is Enter on a session: it opens what the session recorded, as
// `revier session restore` does, off the terminal. The restore walks one
// target at a time and waits for each window, so it can take a while, and a
// second one is refused while it runs.
func (sc *sessionsScreen) restore(sf surface) (tea.Cmd, error) {
	it, ok := sc.list.SelectedItem().(sessionItem)
	if !ok {
		return nil, nil
	}
	if sc.restoring != "" {
		return nil, fmt.Errorf("the restore of %s is still running", sc.restoring)
	}
	sc.restoring = it.session.ID
	projects, s := sf.projects, it.session
	written := make(chan struct{}, 1)
	// The restore's own ledger, on the same state: it says when it wrote.
	c := sf.core.WithLedger(ledger.File{Root: sf.stateRoot, Written: written})
	walk := func() tea.Msg {
		defer close(written)
		ctx, cancel := context.WithTimeout(context.Background(), localHostWait)
		report, err := c.Survey(ctx, projects)
		cancel()
		if err != nil {
			return restoredMsg{id: s.ID, err: err}
		}
		// No deadline over the whole walk: each step bounds its own.
		out, back := c.Restore(context.Background(), s, report, projects)
		return restoredMsg{id: s.ID, restored: out, back: back}
	}
	return tea.Batch(walk, waitLedger(written)), nil
}

// restored takes a restore's answer. What each target came to is the pane's;
// a target that did not open, or a failure to return to the saved project, is
// the footer's too.
func (sc *sessionsScreen) restored(msg restoredMsg) error {
	sc.restoring = ""
	if msg.err != nil {
		return msg.err
	}
	sc.outcome = sessionOutcome{id: msg.id, restored: msg.restored, back: msg.back}
	if _, _, failed := msg.restored.Counts(); failed > 0 {
		return fmt.Errorf("%s: %s did not open", msg.id, core.Count(failed, "target"))
	}
	return msg.back
}

// help is the footer of the step in view.
func (sc *sessionsScreen) help(k keyMap) []key.Binding {
	if sc.naming {
		return []key.Binding{helpKey("enter", "save"), helpKey("esc", "back"), k.Quit}
	}
	return []key.Binding{helpKey("enter", "restore"), helpKey(sessionsBarKey, "save"), helpKey("esc", "back"), k.Quit}
}

// subtitle is the line over the rule: where the sessions are, or the name
// field.
func (sc *sessionsScreen) subtitle(sf surface) string {
	if sc.naming {
		return " " + sc.name.View()
	}
	return " " + sf.theme.Meta.Render("saved in "+config.ContractHome(session.Dir(sf.stateRoot))+", newest first")
}

// ledgerMsg says a restore wrote a launch or a landing to state, on the
// channel it came on, so the next wait reads the same restore's writes. ok is
// false once the restore has ended.
type ledgerMsg struct {
	written <-chan struct{}
	ok      bool
}

// waitLedger delivers the next write of a restore to the update loop, which
// takes state from disk into the surface at once: a press on a target the
// restore is launching must find the launch before the next survey does, or
// it launches a second copy (decisions.md D21).
func waitLedger(written <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-written
		return ledgerMsg{written: written, ok: ok}
	}
}

// ledgerWritten takes a restore's write into the surface, and waits for the
// next.
func (m Model) ledgerWritten(msg ledgerMsg) (tea.Model, tea.Cmd) {
	if !msg.ok {
		return m, nil
	}
	m.takeState()
	return m, waitLedger(msg.written)
}

// nameScreen is what stands in the list's place while the name is typed:
// what Enter records.
func (sc *sessionsScreen) nameScreen(sf surface) string {
	th := sf.theme
	w := sf.list
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	running := 0
	for _, v := range sf.views {
		if heldHere(sf.attached, v) {
			running++
		}
	}
	return say(th.NameDim, "Enter saves the projects open now") + "\n" +
		say(th.Meta, core.Count(running, "project")+" open at the last survey") + "\n" +
		say(th.Meta, "a restore finds the session by its name too")
}

// detail is the pane on the screen: what the session under the cursor
// holds, what the save that wrote it could not record, and what restoring it
// would do here now, or what the restore of it came to.
func (sc *sessionsScreen) detail(sf surface) string {
	it, ok := sc.list.SelectedItem().(sessionItem)
	if !ok {
		return ""
	}
	th, s := sf.theme, it.session
	w := sf.pane
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
	line("File", config.ContractHome(filepath.Join(session.Dir(sf.stateRoot), s.ID+".toml")), th.Path)
	line("Holds", sessionCounts(s), th.Path)

	out := sc.outcome
	if out.id == s.ID && out.saved {
		b.WriteString(heading(th, "Saved", w))
		if len(out.notes) == 0 {
			say(th.Running, "every open target and agent conversation was recorded")
		}
		for _, note := range out.notes {
			say(th.Attention, note)
		}
	}
	switch {
	case sc.restoring == s.ID:
		b.WriteString(heading(th, "Restore", w))
		say(th.Meta, "restoring, one target at a time…")
	case out.id == s.ID && out.restored != nil:
		b.WriteString(heading(th, "Restored", w))
		restoreSteps(sf, &b, out.restored, w)
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
	case !sf.surveyed:
		b.WriteString(heading(th, "Restore plan", w))
		say(th.Meta, "surveying")
	default:
		b.WriteString(heading(th, "Restore plan", w))
		restoreSteps(sf, &b, sf.core.RestorePreview(s, core.Report{Views: sf.views}, sf.projects), w)
		say(th.Meta, "\nEnter: restore")
	}
	return b.String()
}

// restoreSteps writes the plan, or what a restore did, grouped by project, on
// the pane's grid: each recorded target as the Targets section draws it, and
// under it each agent it held as the Agents section draws it, each behind the
// operation the restore makes of it - start it, keep the one open, or skip it.
func restoreSteps(sf surface, b *strings.Builder, steps core.Restored, w int) {
	th := sf.spun
	var project revier.ProjectName
	for _, r := range steps {
		if r.Project != project {
			if project != "" {
				b.WriteString("\n")
			}
			project = r.Project
			b.WriteString(th.ProjectName.Bold(true).Render(clipTo(string(r.Project), w)) + "\n")
		}
		tv, _ := targetView(sf.views, r.Project, r.Target)
		mark, markStyle := th.Glyphs.Stopped+" stopped", th.Count
		switch {
		case !tv.Ref.IsZero():
			mark, markStyle = th.Glyphs.Running+" running", th.Running
		case r.Action == core.RestoreNoHost:
			mark = "no host here"
		case r.Action == core.RestoreRefused:
			mark = "refused"
		}
		op, reason := targetOp(th, r)
		b.WriteString(planRow(th, op, th.ProjectName.Render(string(r.Target)), markStyle.Render(mark),
			reason, th.PathMissing, w) + "\n")
		live := liveAgents(sf.views, r.Project, tv.Ref)
		for i, a := range r.Resumes {
			b.WriteString(planAgent(sf.projects, th, r, i, a, live, w) + "\n")
		}
	}
}

// planAgent is one recorded agent of a step: its operation, then the harness,
// state and activity of the agent now running in its place, or, where none
// runs, what the restore said about it or the conversation it holds.
func planAgent(projects []core.Project, th theme.Theme, r core.RestoreResult, i int, a core.Resume, live []revier.AgentView, w int) string {
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
	// A restore keeps a running target as it is, so an agent that no longer
	// runs in it is not started.
	if r.Action == core.RestoreRunning {
		op, reason = skipOp(th, planTense(r)("skip", "skipped")), "not running, and its target is kept as it is"
	}
	text, style := reason, th.PathMissing
	if text == "" {
		text, style = string(a.Session), th.Path
		if text == "" {
			text = "no conversation recorded"
		}
		if p, ok := projectNamed(projects, r.Project); ok && a.Dir != "" && a.Dir != p.Path {
			text += " in " + config.ContractHome(a.Dir)
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
	case core.AgentNotAdded:
		if r.AgentErr != nil {
			return skipOp(th, tense("skip", "skipped")), "its tab did not open: " + r.AgentErr.Error()
		}
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
func targetView(views []revier.ProjectView, project revier.ProjectName, target revier.TargetName) (revier.TargetView, bool) {
	for _, v := range views {
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
func liveAgents(views []revier.ProjectView, project revier.ProjectName, ref revier.TargetRef) []revier.AgentView {
	if ref.IsZero() {
		return nil
	}
	var out []revier.AgentView
	for _, v := range views {
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
func (sc *sessionsScreen) progressLine(th theme.Theme) string {
	if sc.restoring != "" {
		return th.Meta.Render(fmt.Sprintf(" restoring %s…", sc.restoring))
	}
	return th.Meta.Render(" saving the projects open now…")
}
