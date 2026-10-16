package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// A [[status]] is a command whose output stands at one place on the surface
// (decisions.md D134). The surface runs each one on its own timer, never in
// the way of a frame, and draws what the last good run printed.
const (
	// statusBeat is how often the surface looks for a status that is due.
	// No status runs more often than config.MinStatusEvery, so a beat of
	// that length finds each one at most a beat late.
	statusBeat = 5 * time.Second
	// statusEvery is the wait of a status that names none.
	statusEvery = time.Minute
	// statusTimeout ends a run that has not answered: a command that hangs
	// must not keep the place from ever being run again.
	statusTimeout = 5 * time.Second
	// statusTitle heads the pane's section when the status names none.
	statusTitle = "Info"

	segmentSeparator = "  ·  "
)

// segment is one piece of a status, coloured by its own level: the 5-hour
// window red beside a 7-day window that is not.
type segment struct {
	Text  string `json:"text"`
	Level string `json:"level"`
}

// statusKey names one place's result: a place, and for a place in the pane,
// the project it ran for.
type statusKey struct {
	at      core.StatusPlace
	project revier.ProjectName
}

// statusResult is a place's last good answer, and the status that gave it:
// an answer is shown only while its place still has that status, so a table
// changed or removed in either file takes its answer with it. stale is a run
// since then that failed: the answer stays, dimmed, rather than the place
// going blank.
type statusResult struct {
	segments []segment
	from     core.Status
	stale    bool
}

// statuses is everything the surface knows about the [[status]] tables.
type statuses struct {
	config   []core.Status // config.toml's: the rule's, and the pane's shared ones
	results  map[statusKey]statusResult
	started  map[statusKey]core.Status // the status each key last started a run of
	at       map[statusKey]time.Time   // and when
	running  map[core.StatusPlace]bool // per place, so a cursor run down the list costs one project's runs
	selected revier.ProjectName        // the project the pane's places last ran for
	beating  bool                      // a beat is out
}

func newStatuses(config []core.Status) statuses {
	return statuses{
		config:  config,
		results: map[statusKey]statusResult{},
		started: map[statusKey]core.Status{},
		at:      map[statusKey]time.Time{},
		running: map[core.StatusPlace]bool{},
	}
}

type statusBeatMsg struct{}

type statusMsg struct {
	key      statusKey
	ran      core.Status
	segments []segment
	err      error
}

// retakeStatuses takes config.toml's statuses as a reload read them, and
// runs what fell due while the popup was hidden. A status that changed is
// due at once, and its old answer is no longer shown (statusAt).
func (m *Model) retakeStatuses(config []core.Status) tea.Cmd {
	m.status.config = config
	return m.startStatus()
}

func sameStatus(a, b core.Status) bool {
	return a.At == b.At && a.Title == b.Title && a.Every == b.Every && slices.Equal(a.Run, b.Run)
}

// startStatus runs every place that is due, and starts the beat where none
// is out. No status at all, anywhere, starts nothing.
func (m *Model) startStatus() tea.Cmd {
	if !m.anyStatus() {
		return nil
	}
	if m.status.beating {
		return m.runDue()
	}
	m.status.beating = true
	return tea.Batch(m.runDue(), statusTick())
}

// firstBeat is Init's start of the statuses: every place runs, and the beat
// New marked out goes out.
func (m Model) firstBeat() tea.Cmd {
	if !m.status.beating {
		return nil
	}
	return tea.Batch(m.runDue(), statusTick())
}

func (m Model) anyStatus() bool {
	if len(m.status.config) > 0 {
		return true
	}
	for _, p := range m.projects {
		if len(p.Status) > 0 {
			return true
		}
	}
	return false
}

func statusTick() tea.Cmd {
	return tea.Tick(statusBeat, func(time.Time) tea.Msg { return statusBeatMsg{} })
}

// beat runs what is due and sets the next beat. A beat of a hidden popup, or
// of a surface a reload left with no status, ends the chain; the raise or the
// reload that brings one back starts it again. A surface without the focus
// keeps beating, since it may stand on a second screen, and every status is
// slow already.
func (m *Model) beat() tea.Cmd {
	if m.hidden || !m.anyStatus() {
		m.status.beating = false
		return nil
	}
	return tea.Batch(m.runDue(), statusTick())
}

// followSelection runs the pane's places for a project the cursor has just
// reached, so its section is not a beat behind the cursor.
func (m *Model) followSelection() tea.Cmd {
	name, _ := m.selectedName()
	if name == m.status.selected {
		return nil
	}
	m.status.selected = name
	return m.runDue()
}

// runDue starts a run of every place that is due: the rule's, and the
// pane's for the selected project. Nothing runs while the popup is hidden;
// the raise runs what fell due meanwhile.
func (m *Model) runDue() tea.Cmd {
	if m.hidden {
		return nil
	}
	now := m.now()
	var cmds []tea.Cmd
	due := func(key statusKey, s core.Status, env []string) {
		every := s.Every
		if every == 0 {
			every = statusEvery
		}
		if m.status.running[key.at] {
			return
		}
		if last, ok := m.status.started[key]; ok && sameStatus(last, s) && now.Sub(m.status.at[key]) < every {
			return
		}
		m.status.running[key.at] = true
		m.status.started[key], m.status.at[key] = s, now
		cmds = append(cmds, runStatus(key, s, env))
	}
	for _, s := range m.status.config {
		if !s.At.OfProject() {
			due(statusKey{at: s.At}, s, nil)
		}
	}
	if p, ok := m.highlighted(); ok {
		for _, s := range m.paneStatuses(p) {
			due(statusKey{at: s.At, project: p.Name}, s, projectEnv(p))
		}
	}
	return tea.Batch(cmds...)
}

// paneStatuses is the pane's statuses of one project. What runs nothing is
// left out; `revier doctor` says why.
func (m Model) paneStatuses(p core.Project) []core.Status {
	out, _ := p.StatusOf(m.status.config)
	return out
}

// projectEnv tells a pane's command which project it runs for. A link's
// path is the one on its host, and the host is named, so the command can
// reach it there.
func projectEnv(p core.Project) []string {
	env := []string{"REVIER_PROJECT=" + string(p.Name), "REVIER_PROJECT_PATH=" + p.Path}
	if p.Remote != nil {
		env = append(env, "REVIER_PROJECT_HOST="+p.Remote.Host)
	}
	return env
}

// runStatus runs one command. Like an agent probe, it runs in its own
// process group, and the timeout ends the whole group: a child the command
// started would otherwise hold the output open past it.
func runStatus(key statusKey, s core.Status, env []string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
		defer cancel()
		var out, errb bytes.Buffer
		c := exec.CommandContext(ctx, s.Run[0], s.Run[1:]...)
		c.Env = append(os.Environ(), env...)
		c.Stdout, c.Stderr = &out, &errb
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
		c.WaitDelay = 50 * time.Millisecond
		err := c.Run()
		switch {
		case err == nil:
		case ctx.Err() != nil:
			err = fmt.Errorf("timed out after %s", statusTimeout)
		default:
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
		}
		logging.Poll(strings.TrimSpace("status "+string(key.at)+" "+string(key.project)), "status", start, err,
			"at", key.at, "project", key.project)
		return statusMsg{key: key, ran: s, segments: parseStatus(out.Bytes()), err: err}
	}
}

// takeStatus keeps an answer. A failed run keeps the last good one of the
// same status, dimmed.
func (m *Model) takeStatus(msg statusMsg) {
	m.status.running[msg.key.at] = false
	if msg.err != nil {
		if r, ok := m.status.results[msg.key]; ok && sameStatus(r.from, msg.ran) {
			r.stale = true
			m.status.results[msg.key] = r
		}
		return
	}
	m.status.results[msg.key] = statusResult{segments: msg.segments, from: msg.ran}
}

// parseStatus reads what a command printed: a JSON list of segments, one JSON
// segment, or plain text, whose every line is a segment with no level.
func parseStatus(out []byte) []segment {
	var segs []segment
	if err := json.Unmarshal(out, &segs); err != nil {
		var one segment
		if json.Unmarshal(out, &one) == nil {
			segs = []segment{one}
		} else {
			segs = nil
			for line := range strings.Lines(string(out)) {
				if strings.TrimSpace(line) != "" {
					segs = append(segs, segment{Text: line})
				}
			}
		}
	}
	kept := segs[:0]
	for _, s := range segs {
		s.Text = printable(s.Text)
		if s.Text != "" {
			kept = append(kept, s)
		}
	}
	return kept
}

// statusAt is what a place shows: the screen's for the rule, the project's
// for the pane. An answer of a status the place no longer has is not shown.
func (m Model) statusAt(at core.StatusPlace, project revier.ProjectName) statusResult {
	r, ok := m.status.results[statusKey{at: at, project: project}]
	if !ok {
		return statusResult{}
	}
	now, ok := m.statusOfPlace(at, project)
	if !ok || !sameStatus(now, r.from) {
		return statusResult{}
	}
	return r
}

// statusOfPlace is the status a place has now: config.toml's for the rule,
// the project's merged one for the pane.
func (m Model) statusOfPlace(at core.StatusPlace, project revier.ProjectName) (core.Status, bool) {
	list := m.status.config
	if at.OfProject() {
		p, ok := m.project(project)
		if !ok {
			return core.Status{}, false
		}
		list = m.paneStatuses(p)
	}
	i := slices.IndexFunc(list, func(s core.Status) bool { return s.At == at })
	if i < 0 {
		return core.Status{}, false
	}
	return list[i], true
}

// fit draws the segments that fit in room, each in its level's colour, and
// leaves out whole segments from the right: half a segment reads as a
// different value.
func (r statusResult) fit(th theme.Theme, room int) string {
	var b strings.Builder
	used := 0
	for i, s := range r.segments {
		sep := ""
		if i > 0 {
			sep = segmentSeparator
		}
		w := lipgloss.Width(sep + s.Text)
		if used+w > room {
			break
		}
		b.WriteString(th.Help.Render(sep))
		b.WriteString(r.style(th, s.Level).Render(s.Text))
		used += w
	}
	return b.String()
}

// style is the colour of a level: quiet unless the command says otherwise.
// A stale answer is fainter than quiet, so a segment that had no level
// still shows that its command failed.
func (r statusResult) style(th theme.Theme, level string) lipgloss.Style {
	if r.stale {
		return th.Help.Faint(true)
	}
	switch level {
	case "warning":
		return th.Warning
	case "critical":
		return th.Attention
	}
	return th.Help
}

// printable drops escape sequences and control characters: the command's
// colours are not the theme's, and a byte the width cannot measure breaks the
// frame.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < ' ' || r == 0x7f:
			return -1
		}
		return r
	}, ansi.Strip(s))
}
