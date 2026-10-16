package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// statusSurface is a surveyed surface of the given size over two local
// projects, alpha and beta, with config.toml's statuses as shared. The
// cursor is on alpha. What the first selection started is not run: a test
// feeds the answers it needs.
func statusSurface(t *testing.T, shared []core.Status, w, h int, own ...core.Status) Model {
	t.Helper()
	projects := core.Prepare([]revier.Project{{Name: "alpha", Path: "/p/alpha"}, {Name: "beta", Path: "/p/beta"}})
	projects[0].Status = own
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
	m.status = newStatuses(shared)
	next, _ := m.Update(m.Survey()())
	next, _ = next.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

// forgetRuns is the model with no run started, on the project it has
// selected: a selection that changed would start the pane's places again.
func forgetRuns(m Model) Model {
	selected := m.status.selected
	m.status = newStatuses(m.status.config)
	m.status.selected = selected
	return m
}

// answer is a status command's answer as the run delivers it.
func answer(m Model, key statusKey, err error, segs ...segment) Model {
	ran, _ := m.statusOfPlace(key.at, key.project)
	next, _ := m.Update(statusMsg{key: key, ran: ran, segments: segs, err: err})
	return next.(Model)
}

func plainLines(m Model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

// ruleOf is the thin rule under the action bar, as the screen shows it.
func ruleOf(t *testing.T, m Model) string {
	t.Helper()
	for i, line := range plainLines(m) {
		if strings.TrimSpace(line) != "" {
			return plainLines(m)[i+1]
		}
	}
	t.Fatal("the surface draws nothing")
	return ""
}

var (
	ruleLeft   = statusKey{at: core.StatusRuleLeft}
	ruleRight  = statusKey{at: core.StatusRuleRight}
	alphaLine  = statusKey{at: core.StatusProjectLine, project: "alpha"}
	alphaInfo  = statusKey{at: core.StatusProject, project: "alpha"}
	everyPlace = []core.Status{{At: core.StatusRuleLeft, Run: []string{"l"}}, {At: core.StatusRuleRight, Run: []string{"r"}}, {At: core.StatusProjectLine, Run: []string{"pl"}}, {At: core.StatusProject, Run: []string{"p"}}}
	errFailed  = errors.New("exit status 1")
)

func texts(segs []segment) []string {
	var out []string
	for _, s := range segs {
		out = append(out, s.Text)
	}
	return out
}

// A command may print a JSON list of segments, one JSON segment, or plain
// text, of which every line that holds anything is a segment. Its escape
// sequences and control characters do not reach the frame, and a segment
// with no text is dropped.
func TestStatusParsesWhatACommandPrints(t *testing.T) {
	for _, c := range []struct {
		name string
		out  string
		want []segment
	}{
		{"a list", `[{"text":"5h 40%","level":"critical"},{"text":"7d 12%"}]`,
			[]segment{{Text: "5h 40%", Level: "critical"}, {Text: "7d 12%"}}},
		{"one segment", `{"text":"main","level":"warning"}`, []segment{{Text: "main", Level: "warning"}}},
		{"plain text", "first\n\n  \nsecond\n", []segment{{Text: "first"}, {Text: "second"}}},
		{"escapes stripped", "\x1b[31mred\x1b[0m\x07 text\r\n", []segment{{Text: "red text"}}},
		{"a tab is a space", "branch\tmain\n", []segment{{Text: "branch main"}}},
		{"empty text dropped", `[{"text":""},{"text":"kept"},{"text":"\u001b[1m\u001b[0m"}]`, []segment{{Text: "kept"}}},
		{"nothing", "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := parseStatus([]byte(c.out)); !slices.Equal(got, c.want) {
				t.Errorf("parseStatus(%q) = %+v, want %+v", c.out, got, c.want)
			}
		})
	}
}

// One line holds the segments that fit, whole, from the left: half a
// segment reads as a different value.
func TestStatusFitDropsWholeSegmentsFromTheRight(t *testing.T) {
	th := theme.Default()
	r := statusResult{segments: []segment{{Text: "aaaa"}, {Text: "bbbb"}, {Text: "cc"}}}
	for _, c := range []struct {
		room int
		want string
	}{
		{100, "aaaa  ·  bbbb  ·  cc"},
		{20, "aaaa  ·  bbbb  ·  cc"},
		{19, "aaaa  ·  bbbb"},
		{13, "aaaa  ·  bbbb"},
		{12, "aaaa"},
		{4, "aaaa"},
		{3, ""},
	} {
		if got := ansi.Strip(r.fit(th, c.room)); got != c.want {
			t.Errorf("fit(%d) = %q, want %q", c.room, got, c.want)
		}
	}
}

// Each segment takes its level's colour, and a stale answer is quiet
// throughout.
func TestStatusFitColoursByLevel(t *testing.T) {
	th := theme.Default()
	if th.Warning.Render("x") == th.Help.Render("x") || th.Attention.Render("x") == th.Help.Render("x") {
		t.Skip("the theme renders no colour here")
	}
	segs := []segment{{Text: "calm", Level: "normal"}, {Text: "near", Level: "warning"}, {Text: "over", Level: "critical"}, {Text: "bare"}}
	fresh := statusResult{segments: segs}.fit(th, 100)
	for _, want := range []string{th.Help.Render("calm"), th.Warning.Render("near"), th.Attention.Render("over"), th.Help.Render("bare")} {
		if !strings.Contains(fresh, want) {
			t.Errorf("fit = %q, want it to hold %q", fresh, want)
		}
	}
	stale := statusResult{segments: segs, stale: true}.fit(th, 100)
	for _, s := range segs {
		if !strings.Contains(stale, th.Help.Render(s.Text)) {
			t.Errorf("stale fit = %q, want %q drawn quiet", stale, s.Text)
		}
	}
}

// A failed run keeps the last good answer, marked stale; the next good one
// replaces it, fresh. A failure with nothing to keep keeps nothing.
func TestStatusKeepsTheLastGoodAnswer(t *testing.T) {
	m := statusSurface(t, everyPlace, 90, 30)
	m = answer(m, ruleLeft, errFailed)
	if _, ok := m.status.results[ruleLeft]; ok {
		t.Errorf("a failure with nothing before it left %+v", m.status.results[ruleLeft])
	}
	m = answer(m, ruleLeft, nil, segment{Text: "good"})
	m = answer(m, ruleLeft, errFailed)
	got := m.status.results[ruleLeft]
	if !got.stale || !slices.Equal(texts(got.segments), []string{"good"}) {
		t.Errorf("after a failure = %+v, want good, stale", got)
	}
	m = answer(m, ruleLeft, nil, segment{Text: "better"})
	if got := m.status.results[ruleLeft]; got.stale || !slices.Equal(texts(got.segments), []string{"better"}) {
		t.Errorf("after a good run = %+v, want better, fresh", got)
	}
}

// A status changed by a reload, in config.toml or in a project file, no
// longer shows the answer of the old one, and is due at once.
func TestStatusChangedByAReloadDropsItsAnswer(t *testing.T) {
	m := forgetRuns(statusSurface(t, everyPlace, 140, 30))
	m = answer(m, ruleLeft, nil, segment{Text: "old rule"})
	m = answer(m, alphaInfo, nil, segment{Text: "old info"})

	changed := slices.Clone(everyPlace)
	for i := range changed {
		changed[i].Run = []string{"other"}
	}
	m.status.config = changed
	if got := m.statusAt(core.StatusRuleLeft, ""); len(got.segments) != 0 {
		t.Errorf("rule-left after config.toml changed = %+v, want nothing", got)
	}
	if got := m.statusAt(core.StatusProject, "alpha"); len(got.segments) != 0 {
		t.Errorf("project after config.toml changed = %+v, want nothing", got)
	}
	m.runDue()
	for _, key := range []statusKey{ruleLeft, alphaInfo} {
		if !m.status.running[key] {
			t.Errorf("%+v not run again after its status changed", key)
		}
	}

	m = forgetRuns(statusSurface(t, everyPlace, 140, 30))
	m = answer(m, alphaInfo, nil, segment{Text: "old info"})
	m.projects[0].Status = []core.Status{{At: core.StatusProject, Run: []string{"own"}}}
	if got := m.statusAt(core.StatusProject, "alpha"); len(got.segments) != 0 {
		t.Errorf("project after the project file changed = %+v, want nothing", got)
	}
}

// A failed run of a status that has changed since does not mark the new
// one's answer stale.
func TestStatusFailureOfAnOldRunLeavesTheNewAnswer(t *testing.T) {
	m := forgetRuns(statusSurface(t, everyPlace, 90, 30))
	m = answer(m, ruleLeft, nil, segment{Text: "new"})
	next, _ := m.Update(statusMsg{key: ruleLeft, ran: core.Status{At: core.StatusRuleLeft, Run: []string{"old"}}, err: errors.New("exit status 1")})
	m = next.(Model)
	if got := m.statusAt(core.StatusRuleLeft, ""); got.stale {
		t.Errorf("rule-left = %+v, want fresh", got)
	}
}

// The thin rule carries rule-left at its left end and rule-right at its
// right end, beside the pane and without it.
func TestStatusStandsInTheThinRule(t *testing.T) {
	for _, c := range []struct {
		name string
		w    int
		pane bool
	}{{"without the pane", 90, false}, {"beside the pane", 140, true}} {
		t.Run(c.name, func(t *testing.T) {
			m := statusSurface(t, everyPlace, c.w, 30)
			if got := m.paneWidth() > 0; got != c.pane {
				t.Fatalf("pane shown = %v at %d columns, want %v", got, c.w, c.pane)
			}
			m = answer(m, ruleLeft, nil, segment{Text: "left"})
			m = answer(m, ruleRight, nil, segment{Text: "5h 40%"}, segment{Text: "7d 12%"})
			rule := ruleOf(t, m)
			if !strings.Contains(rule, "── left ──") {
				t.Errorf("rule = %q, want left in it", rule)
			}
			if !strings.HasSuffix(strings.TrimRight(rule, " "), " 5h 40%  ·  7d 12% ──") {
				t.Errorf("rule = %q, want the right status at its right end", rule)
			}
			if c.pane && strings.Index(rule, "left") > strings.Index(rule, "┬") {
				t.Errorf("rule = %q, want left over the list", rule)
			}
			if c.pane && strings.Index(rule, "5h") < strings.Index(rule, "┬") {
				t.Errorf("rule = %q, want the right status over the pane", rule)
			}
		})
	}
}

// With nothing to show the rule runs through, as it did before statuses.
func TestStatusAbsentLeavesTheRuleWhole(t *testing.T) {
	m := statusSurface(t, nil, 90, 30)
	if rule := strings.TrimSpace(ruleOf(t, m)); strings.Trim(rule, "─") != "" {
		t.Errorf("rule = %q, want a plain rule", rule)
	}
}

// The pane carries project-line at the right end of its first line, and
// the project status as a section of its own under Info, one segment to a
// line.
func TestStatusStandsInThePane(t *testing.T) {
	m := statusSurface(t, everyPlace, 140, 40)
	m = answer(m, alphaLine, nil, segment{Text: "main ↑2"})
	m = answer(m, alphaInfo, nil, segment{Text: "3 open PRs"}, segment{Text: "CI green"})
	content := ansi.Strip(m.detail.GetContent())
	first := strings.SplitN(content, "\n", 2)[0]
	if !strings.Contains(first, "alpha") || !strings.HasSuffix(strings.TrimRight(first, " "), "main ↑2") {
		t.Errorf("first pane line = %q, want alpha with main ↑2 at its end", first)
	}
	section := strings.Index(content, "Info ─")
	if section < 0 {
		t.Fatalf("pane = %q, want an Info section", content)
	}
	if !strings.Contains(content[section:], "3 open PRs\nCI green\n") {
		t.Errorf("Info section = %q, want one segment to a line", content[section:])
	}
}

// A project with no answer yet shows no section.
func TestStatusSectionWaitsForAnAnswer(t *testing.T) {
	m := statusSurface(t, everyPlace, 140, 40)
	if content := ansi.Strip(m.detail.GetContent()); strings.Contains(content, "Info") {
		t.Errorf("pane = %q, want no Info section before an answer", content)
	}
}

// A title the project's table sets heads the section over the shared one's.
func TestStatusSectionTakesItsTitle(t *testing.T) {
	m := statusSurface(t, everyPlace, 140, 40, core.Status{At: core.StatusProject, Title: "Tickets"})
	m = answer(m, alphaInfo, nil, segment{Text: "2 assigned"})
	content := ansi.Strip(m.detail.GetContent())
	if !strings.Contains(content, "Tickets ─") || strings.Contains(content, "Info") {
		t.Errorf("pane = %q, want the section titled Tickets", content)
	}
}

// Nothing runs while the popup is hidden.
func TestStatusRunsNothingWhileHidden(t *testing.T) {
	m := statusSurface(t, everyPlace, 90, 30)
	m = forgetRuns(m)
	m.hidden = true
	if cmd := m.runDue(); cmd != nil {
		t.Error("runDue started a run while hidden")
	}
	if len(m.status.running) != 0 || len(m.status.started) != 0 {
		t.Errorf("hidden: running %v, started %v, want neither", m.status.running, m.status.started)
	}
}

// A key whose command is still running, or ran less than its wait ago, is
// not started again; one whose wait is up and whose run answered is.
func TestStatusStartsOneRunOfAKeyAtATime(t *testing.T) {
	m := statusSurface(t, []core.Status{{At: core.StatusRuleLeft, Run: []string{"l"}}}, 90, 30)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return at }
	m = forgetRuns(m)
	if m.runDue() == nil || !m.status.running[ruleLeft] {
		t.Fatal("the first runDue started nothing")
	}
	at = at.Add(2 * statusEvery)
	if m.runDue() != nil {
		t.Error("a second run started while the first is running")
	}
	m = answer(m, ruleLeft, nil, segment{Text: "done"})
	if m.runDue() == nil {
		t.Error("a key past its wait with no run out started nothing")
	}
	m = answer(m, ruleLeft, nil, segment{Text: "done"})
	at = at.Add(statusEvery / 2)
	if m.runDue() != nil {
		t.Error("a key started again before its wait was up")
	}
}

// The pane's places run for the selected project, told which one it is.
func TestStatusRunsThePaneForTheSelectedProject(t *testing.T) {
	m := statusSurface(t, everyPlace, 90, 30)
	for _, key := range []statusKey{alphaLine, alphaInfo} {
		if !m.status.running[key] {
			t.Errorf("%+v not started for the selected project", key)
		}
	}
	for key := range m.status.running {
		if key.project == "beta" {
			t.Errorf("%+v started for a project not selected", key)
		}
	}
}

// New marks the beat out for Init, which runs on a copy: a reload after it
// starts no second beat chain.
func TestStatusInitBeatIsKept(t *testing.T) {
	projects := core.Prepare([]revier.Project{{Name: "alpha", Path: "/p/alpha"}})
	projects[0].Status = []core.Status{{At: core.StatusProject, Run: []string{"p"}}}
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, projects, t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
	if m.firstBeat() == nil || !m.status.beating {
		t.Fatal("a surface with a status starts no beat")
	}
	if cmd := m.retakeStatuses(nil); cmd != nil {
		if _, ok := cmd().(statusBeatMsg); ok {
			t.Error("a reload started a second beat")
		}
	}

	none := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, core.Prepare([]revier.Project{{Name: "beta", Path: "/p/beta"}}), t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
	if none.firstBeat() != nil {
		t.Error("a surface with no status started a beat")
	}
}

// A command runs without a shell, and its output is read as a status.
func TestStatusRunsTheCommand(t *testing.T) {
	s := core.Status{At: core.StatusProject, Run: []string{"sh", "-c", `echo "$REVIER_PROJECT"`}}
	msg := runStatus(alphaInfo, s, []string{"REVIER_PROJECT=alpha"})().(statusMsg)
	if msg.err != nil || msg.key != alphaInfo || !sameStatus(msg.ran, s) || !slices.Equal(texts(msg.segments), []string{"alpha"}) {
		t.Errorf("runStatus = %+v, want alpha", msg)
	}
}
