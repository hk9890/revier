package tui_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// shownAgent is one agent of shownWorld: its state, when it spoke last, and
// what its panel shows.
type shownAgent struct {
	status revier.Status
	at     time.Time
	screen string
}

// shownWorld is one running project whose agents are in the given states,
// one probe to each, surveyed, with the pane's ask for when each spoke and
// the mirror's read of the agent the pane chose answered. Agent i's row reads
// "agent-i task", and its panel is i+1.
func shownWorld(t *testing.T, width, height int, agents ...shownAgent) (tui.Model, *hosttest.FakeRuntime, []*hosttest.FakeDetailedProbe) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Screens = map[revier.PanelID]string{}
	var probes []revier.AgentProbe
	var fakes []*hosttest.FakeDetailedProbe
	var panels []revier.Panel
	for i, a := range agents {
		marker := fmt.Sprintf("agent-%d", i)
		id := revier.PanelID(fmt.Sprint(i + 1))
		p := hosttest.NewDetailedProbe("claude", marker)
		p.State = revier.AgentState{Harness: "claude", Status: a.status, Activity: marker + " task"}
		p.Said[id] = revier.AgentDetail{At: a.at}
		probes, fakes = append(probes, p), append(fakes, p)
		panels = append(panels, revier.Panel{ID: id, Kind: revier.PanelTool, Title: "claude " + marker})
		rt.Screens[id] = a.screen
	}
	projects := core.Prepare([]revier.Project{{Name: "duo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:duo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:duo$"}}},
	}}})
	rt.Add("session:duo", "kitty", panels...)
	c := &core.Core{Runtime: rt, Probes: probes}
	return resize(refreshed(t, c, projects, stateWith(t, nil), nil), width, height).Said().Mirrored(), rt, fakes
}

// The pane mirrors an agent's panel without the cursor going there: the one
// that needs the user comes first, then one at rest, then one working.
func TestTheAgentThatNeedsYouIsMirroredFirst(t *testing.T) {
	m, _, _ := shownWorld(t, 140, 30,
		shownAgent{status: revier.StatusIdle, screen: "resting now"},
		shownAgent{status: revier.StatusRunning, screen: "still at it"},
		shownAgent{status: revier.StatusAttention, screen: "merge now or wait?"})
	body := pane(m)
	if !strings.Contains(body, "Screen") || !strings.Contains(body, "merge now or wait?") {
		t.Errorf("pane does not mirror the agent that needs the user:\n%s", body)
	}
	if strings.Contains(body, "resting now") || strings.Contains(body, "still at it") {
		t.Errorf("pane mirrors another agent's panel:\n%s", body)
	}

	m, _, _ = shownWorld(t, 140, 30,
		shownAgent{status: revier.StatusRunning, screen: "still at it"},
		shownAgent{status: revier.StatusIdle, screen: "resting now"})
	if body := pane(m); !strings.Contains(body, "resting now") {
		t.Errorf("pane = %q, want the agent at rest before the working one", body)
	}
}

// Among agents in one state, the one that spoke last is mirrored.
func TestAmongEqualsTheAgentThatSpokeLastIsMirrored(t *testing.T) {
	now := time.Now()
	m, _, _ := shownWorld(t, 140, 30,
		shownAgent{status: revier.StatusIdle, screen: "the older screen", at: now.Add(-2 * time.Hour)},
		shownAgent{status: revier.StatusIdle, screen: "the newer screen", at: now.Add(-5 * time.Minute)})
	if body := pane(m); !strings.Contains(body, "the newer screen") || strings.Contains(body, "the older screen") {
		t.Errorf("pane = %q, want the agent that spoke last", body)
	}
}

// Tab into the agents is not a choice: the cursor lands on the agent the
// pane already mirrors. Moving it is, and a survey that brings another agent
// forward leaves the chosen one mirrored.
func TestMovingTheAgentCursorChoosesThatAgent(t *testing.T) {
	m, _, fakes := shownWorld(t, 140, 30,
		shownAgent{status: revier.StatusIdle, screen: "first agent's screen"},
		shownAgent{status: revier.StatusAttention, screen: "second agent's screen"})
	m, _ = press(m, "tab")
	if row := paneCursor(m); !strings.Contains(row, "agent-1") {
		t.Fatalf("pane cursor = %q after tab, want the agent the pane mirrored", row)
	}
	m, _ = press(m, "up")
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "first agent's screen") || strings.Contains(body, "second agent's screen") {
		t.Fatalf("pane = %q after up, want the agent moved to", body)
	}
	fakes[0].State.Status = revier.StatusRunning
	m = survey(m).Said().Mirrored()
	if body := pane(m); !strings.Contains(body, "first agent's screen") {
		t.Errorf("pane = %q after a survey, want the chosen agent still mirrored", body)
	}
}

// An agent that comes under the pane's cursor is drawn with its own screen
// pending, and not over the screen of the agent the cursor left.
func TestTheScreenOfTheAgentThePaneCursorLeftIsNotShownUnderTheNext(t *testing.T) {
	m, _, _ := shownWorld(t, 140, 30,
		shownAgent{status: revier.StatusIdle, screen: "first agent's screen"},
		shownAgent{status: revier.StatusAttention, screen: "second agent's screen"})
	m, _ = press(m, "tab")
	m, _ = press(m, "up")
	if body := pane(m); strings.Contains(body, "second agent's screen") || !strings.Contains(body, "reading...") {
		t.Errorf("pane shows the screen of the agent the cursor left:\n%s", body)
	}
}

// The mirror is of the project under the list's cursor: another project
// under it is read anew, and its screen replaces the first one's.
func TestAnotherProjectUnderTheCursorIsMirroredAnew(t *testing.T) {
	m, _, _ := listedSurface(t, 160, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "one", screen: "alpha's screen"},
		listed{project: "beta", status: revier.StatusIdle, on: "two", screen: "beta's screen"})
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "alpha's screen") || strings.Contains(body, "beta's screen") {
		t.Fatalf("pane does not mirror the agent of the project under the cursor:\n%s", body)
	}
	m, _ = press(m, "down")
	if body := pane(m); strings.Contains(body, "alpha's screen") {
		t.Errorf("pane shows another project's screen:\n%s", body)
	}
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "beta's screen") {
		t.Errorf("pane does not mirror the project the cursor moved to:\n%s", body)
	}
}

// A screen longer than the rows the pane leaves under the facts shows its
// end, and the pane follows the panel as it writes on.
func TestTheProjectPaneShowsTheEndOfTheScreenAndFollowsIt(t *testing.T) {
	var screen []string
	for i := range 40 {
		screen = append(screen, fmt.Sprintf("line %02d", i))
	}
	m, rt, _ := shownWorld(t, 140, 30, shownAgent{status: revier.StatusIdle, screen: strings.Join(screen, "\n")})
	if body := pane(m); !strings.Contains(body, "line 39") || strings.Contains(body, "line 00") {
		t.Fatalf("pane does not show the end of the screen:\n%s", body)
	}
	if body := pane(m); !strings.Contains(body, "agent-0 task") || !strings.Contains(body, "Targets") {
		t.Errorf("the mirror took the rows of the facts above it:\n%s", body)
	}
	rt.Screens["1"] = "the screen moved on"
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "the screen moved on") || strings.Contains(body, "line 39") {
		t.Errorf("pane does not follow the panel:\n%s", body)
	}
}

// The wheel over the project pane scrolls the mirror into the panel's
// scrollback, as it does beside the agent list, and the facts above it stay.
func TestTheWheelOverTheProjectPaneScrollsTheMirror(t *testing.T) {
	var screen []string
	for i := range 80 {
		screen = append(screen, fmt.Sprintf("row %02d", i))
	}
	m, rt, _ := shownWorld(t, 140, 30, shownAgent{status: revier.StatusIdle, screen: strings.Join(screen[60:], "\n")})
	rt.Screens["1"] = strings.Join(screen, "\n")
	border := paneBorder(t, m)
	next, cmd := m.Update(tea.MouseMsg{X: border + 4, Y: 12, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("the wheel over the mirror asked for nothing, want a read of the scrollback")
	}
	m = run(next.(tui.Model), cmd)
	if last := rt.ScreenReads[len(rt.ScreenReads)-1]; !last.Scrollback {
		t.Errorf("last read = %+v, want the scrollback", last)
	}
	body := pane(m)
	if strings.Contains(body, "row 79") || !strings.Contains(body, "row 76") {
		t.Errorf("pane is not scrolled up by a notch:\n%s", body)
	}
	if !strings.HasPrefix(body, "Project  duo") {
		t.Errorf("the wheel moved the facts above the mirror:\n%s", body)
	}
}

// A wide pane puts the mirror beside the facts, level with the name, where
// it has the pane's whole height; a narrower one puts it under them.
func TestAWidePaneLaysTheMirrorBesideTheFacts(t *testing.T) {
	var screen []string
	for i := range 60 {
		screen = append(screen, fmt.Sprintf("line %02d", i))
	}
	agent := shownAgent{status: revier.StatusIdle, screen: strings.Join(screen, "\n")}

	wide, _, _ := shownWorld(t, 300, 40, agent)
	top := strings.Split(pane(wide), "\n")[0]
	if !strings.HasPrefix(top, "Project  duo") || !strings.Contains(top, "Screen") {
		t.Errorf("pane top = %q, want the name and the mirror's heading on one line", top)
	}
	stacked, _, _ := shownWorld(t, 250, 40, agent)
	if top := strings.Split(pane(stacked), "\n")[0]; strings.Contains(top, "Screen") {
		t.Fatalf("pane top = %q at 250 columns, want the mirror under the facts", top)
	}
	if w, s := strings.Count(pane(wide), "line "), strings.Count(pane(stacked), "line "); w <= s {
		t.Errorf("the mirror beside the facts shows %d lines and the one under them %d, want more beside", w, s)
	}
}

// A pane with no row left under the facts shows no heading with nothing
// under it.
func TestAShortPaneShowsNoHeadingWithoutTheMirror(t *testing.T) {
	shown := 0
	for height := 8; height <= 30; height++ {
		m, _, _ := shownWorld(t, 140, height, shownAgent{status: revier.StatusIdle, screen: "the only line"})
		body := pane(m)
		if !strings.Contains(body, "Screen") {
			continue
		}
		shown++
		if !strings.Contains(body, "the only line") {
			t.Errorf("pane at %d rows has the mirror's heading and no mirror:\n%s", height, body)
		}
	}
	if shown == 0 {
		t.Error("no height showed the mirror")
	}
}

// A read that fails once does not blank the screen the pane was showing.
func TestAReadThatFailsKeepsTheScreenShown(t *testing.T) {
	m, rt, _ := shownWorld(t, 140, 30, shownAgent{status: revier.StatusIdle, screen: "all tests pass"})
	rt.ScreenErr = errors.New("kitty timed out")
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "all tests pass") {
		t.Errorf("pane = %q after a read that failed, want the screen it showed", body)
	}
}

// A runtime that cannot read a panel leaves the mirror a note, and the facts
// stand.
func TestAPanelTheProjectPaneCannotReadLeavesANote(t *testing.T) {
	m, rt, _ := shownWorld(t, 140, 30, shownAgent{status: revier.StatusIdle, screen: "never shown again"})
	rt.ScreenErr = core.ErrNoScreen
	m = m.Mirrored()
	body := pane(m)
	if !strings.Contains(body, "cannot be read") || !strings.Contains(body, "agent-0 task") {
		t.Errorf("pane = %q, want the facts and a note in the mirror's place", body)
	}
}

// A link's agent is in a panel here, so the pane mirrors it as any other
// (decisions.md D104): the screen is this machine's terminal, and nothing is
// asked of the agent's host for it.
func TestALinksAgentIsMirroredFromItsPanelHere(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	rt := openHere("alpha")
	rt.Screens = map[revier.PanelID]string{"9": "the far agent's screen"}
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 140, 30).Mirrored()
	if body := pane(m); !strings.Contains(body, "the far agent's screen") {
		t.Errorf("pane = %q, want the screen of the panel here that shows the link's agent", body)
	}
}

// Nothing is read for a pane that is not on screen: on a terminal too narrow
// for it beside the list, with the cursor on the list.
func TestNothingIsReadWithoutAPaneToShowIt(t *testing.T) {
	m, rt, fakes := shownWorld(t, 80, 30, shownAgent{status: revier.StatusIdle, screen: "unseen"})
	if n := fakes[0].DetailCalls(); n != 0 {
		t.Errorf("the probe was asked %d times with no pane on screen, want none", n)
	}
	if n := len(rt.ScreenReads); n != 0 || m.MirrorAsked() != 0 {
		t.Errorf("the panel was read %d times and asked for %d times with no pane on screen, want neither", n, m.MirrorAsked())
	}
}

// crowd is n agents at rest: more rows than a short pane holds, so it has
// none left for the mirror.
func crowd(n int) []shownAgent {
	agents := make([]shownAgent, n)
	for i := range agents {
		agents[i] = shownAgent{status: revier.StatusIdle, screen: "the only line"}
	}
	return agents
}

// Nothing is read for a pane with no row left for the mirror either, however
// often the timer fires. The read is sent once the pane has a row for it.
func TestNothingIsReadForAPaneWithNoRowForTheMirror(t *testing.T) {
	m, rt, _ := shownWorld(t, 140, 22, crowd(16)...)
	if body := pane(m); strings.Contains(body, "Screen") {
		t.Fatalf("the pane has a row for the mirror:\n%s", body)
	}
	m = m.MirrorTicked().MirrorTicked()
	if n := len(rt.ScreenReads); n != 0 || m.MirrorAsked() != 0 {
		t.Errorf("the panel was read %d times and asked for %d times with no row for the mirror, want neither", n, m.MirrorAsked())
	}
	m = resize(m, 140, 60)
	if got := m.MirrorAsked(); got != 1 {
		t.Errorf("reads asked for = %d once the pane has a row for the mirror, want 1", got)
	}
}

// A pane scrolled while it had no row for the mirror stands at its top once
// it fits: the wheel over a mirror scrolls the mirror, so nothing else would
// bring the facts back.
func TestAPaneThatFitsAgainStandsAtItsTop(t *testing.T) {
	m, _, _ := shownWorld(t, 140, 22, crowd(16)...)
	m = wheel(m, paneBorder(t, m)+4, tea.MouseButtonWheelDown)
	if body := pane(m); strings.HasPrefix(body, "Project  duo") {
		t.Fatalf("the wheel did not scroll a pane with no row for the mirror:\n%s", body)
	}
	m = resize(m, 140, 60)
	if body := pane(m); !strings.HasPrefix(body, "Project  duo") || !strings.Contains(body, "Screen") {
		t.Errorf("the pane that fits again stands scrolled:\n%s", body)
	}
}

// In a wide pane the mirror stands beside the rows on the same lines; a
// click on its text is a click on the mirror, not on the row it is level
// with.
func TestAClickOnTheMirrorBesideARowRunsNothing(t *testing.T) {
	var screen []string
	for i := range 30 {
		screen = append(screen, fmt.Sprintf("line %02d", i))
	}
	m, _, _ := shownWorld(t, 300, 40, shownAgent{status: revier.StatusIdle, screen: strings.Join(screen, "\n")})
	_, y := paneCell(t, m, "agent-0 task")
	raw := strings.Split(m.View(), "\n")[y]
	parts := strings.Split(raw, "│")
	at := strings.Index(parts[1], "line ")
	if at < 0 {
		t.Fatalf("no mirror text on the agent row's line %q", raw)
	}
	x := paneBorder(t, m) + 1 + lipgloss.Width(parts[1][:at])
	m, _ = clickCell(m, x, y)
	m, _ = clickCell(m, x, y)
	if row := paneCursor(m); row != "" {
		t.Errorf("pane cursor = %q after a double click on the mirror, want it still on the list", row)
	}
}

// A click on one project's pane row and a click on the same cell of another
// project's are two single clicks, not a double click on the second.
func TestClicksOnTwoProjectsRowsAreNotADoubleClick(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	var raw []revier.Project
	for _, name := range []string{"alpha", "beta"} {
		rt.Add("session:"+name, "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude idle " + name + " work"})
		raw = append(raw, revier.Project{Name: revier.ProjectName(name), Path: "/p/" + name, Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "session:" + name, Launch: []string{"x"}, Match: revier.Match{Title: "^session:" + name + "$"}}},
		}})
	}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "claude"}}}
	m := resize(refreshed(t, c, core.Prepare(raw), stateWith(t, nil), nil), 140, 30)

	x, y := paneCell(t, m, "alpha work")
	m, _ = clickCell(m, x, y)
	m, _ = press(m, "esc")
	m, _ = press(m, "down")
	if bx, by := paneCell(t, m, "beta work"); bx != x || by != y {
		t.Fatalf("beta's agent is at %d,%d, alpha's was at %d,%d: the test needs one cell", bx, by, x, y)
	}
	_, cmd := clickCell(m, x, y)
	runAll(cmd)
	if len(rt.PanelFocuses) != 0 {
		t.Errorf("panel focuses = %v, want none: one click on beta's agent", rt.PanelFocuses)
	}
}

// The facts, the rows and the rules of a stacked pane run to its edge,
// however wide it is.
func TestTheFactsRunToThePanesEdge(t *testing.T) {
	m, _, _ := shownWorld(t, 250, 40, shownAgent{status: revier.StatusIdle, screen: "short"})
	rule := strings.Split(pane(m), "\n")[1]
	if n := utf8.RuneCountInString(rule); n <= 100 {
		t.Errorf("the rule under the name is %d columns, want it at the pane's edge", n)
	}
}
