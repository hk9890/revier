package tui_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// saidAgent is one agent of saidWorld: its state, and what it said last.
type saidAgent struct {
	status revier.Status
	said   string
	at     time.Time
}

// saidWorld is one running project whose agents are in the given states and
// said the given things, one probe to each, surveyed and with the pane's ask
// for what they said answered. Agent i's row reads "agent-i task".
func saidWorld(t *testing.T, width, height int, agents ...saidAgent) (tui.Model, []*hosttest.FakeDetailedProbe) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	var probes []revier.AgentProbe
	var fakes []*hosttest.FakeDetailedProbe
	var panels []revier.Panel
	for i, a := range agents {
		marker := fmt.Sprintf("agent-%d", i)
		id := revier.PanelID(fmt.Sprint(i + 1))
		p := hosttest.NewDetailedProbe("claude", marker)
		p.State = revier.AgentState{Harness: "claude", Status: a.status, Activity: marker + " task"}
		p.Said[id] = revier.AgentDetail{Message: a.said, At: a.at}
		probes, fakes = append(probes, p), append(fakes, p)
		panels = append(panels, revier.Panel{ID: id, Kind: revier.PanelAgent, Title: "claude " + marker})
	}
	projects := core.Prepare([]revier.Project{{Name: "duo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:duo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:duo$"}}},
	}}})
	rt.Add("session:duo", "kitty", panels...)
	c := &core.Core{Runtime: rt, Probes: probes}
	return resize(refreshed(t, c, projects, stateWith(t, nil), nil), width, height).Said(), fakes
}

// The pane shows an agent's last message without the cursor going there: the
// one that needs the user comes first, then one at rest, then one working.
func TestTheAgentThatNeedsYouIsShownFirst(t *testing.T) {
	m, _ := saidWorld(t, 140, 30,
		saidAgent{status: revier.StatusIdle, said: "resting now"},
		saidAgent{status: revier.StatusRunning, said: "still at it"},
		saidAgent{status: revier.StatusAttention, said: "merge now or wait?"})
	body := pane(m)
	if !strings.Contains(body, "Last message") || !strings.Contains(body, "merge now or wait?") {
		t.Errorf("pane shows no message of the agent that needs the user:\n%s", body)
	}
	if strings.Contains(body, "resting now") || strings.Contains(body, "still at it") {
		t.Errorf("pane shows another agent's message:\n%s", body)
	}
	if strings.Contains(body, "Project Snapshot") {
		t.Errorf("pane still shows the project snapshot:\n%s", body)
	}

	m, _ = saidWorld(t, 140, 30,
		saidAgent{status: revier.StatusRunning, said: "still at it"},
		saidAgent{status: revier.StatusIdle, said: "resting now"})
	if body := pane(m); !strings.Contains(body, "resting now") {
		t.Errorf("pane = %q, want the agent at rest before the working one", body)
	}
}

// Among agents in one state, the one that spoke last is shown.
func TestAmongEqualsTheAgentThatSpokeLastIsShown(t *testing.T) {
	now := time.Now()
	m, _ := saidWorld(t, 140, 30,
		saidAgent{status: revier.StatusIdle, said: "the older word", at: now.Add(-2 * time.Hour)},
		saidAgent{status: revier.StatusIdle, said: "the newer word", at: now.Add(-5 * time.Minute)})
	body := pane(m)
	if !strings.Contains(body, "the newer word") || strings.Contains(body, "the older word") {
		t.Errorf("pane = %q, want the agent that spoke last", body)
	}
	if !strings.Contains(body, "5 minutes ago") {
		t.Errorf("pane = %q, want how long ago it spoke", body)
	}
}

// Tab into the agents is not a choice: the cursor lands on the agent the
// pane already shows. Moving it is, and a survey that brings another agent
// forward leaves the chosen one shown.
func TestMovingTheAgentCursorChoosesThatAgent(t *testing.T) {
	m, fakes := saidWorld(t, 140, 30,
		saidAgent{status: revier.StatusIdle, said: "first agent speaking"},
		saidAgent{status: revier.StatusAttention, said: "second agent speaking"})
	m, _ = press(m, "tab")
	if row := paneCursor(m); !strings.Contains(row, "agent-1") {
		t.Fatalf("pane cursor = %q after tab, want the agent the pane showed", row)
	}
	m, _ = press(m, "up")
	if body := pane(m); !strings.Contains(body, "first agent speaking") {
		t.Fatalf("pane = %q after up, want the agent moved to", body)
	}
	fakes[0].State.Status = revier.StatusRunning
	m = survey(m).Said()
	if body := pane(m); !strings.Contains(body, "first agent speaking") {
		t.Errorf("pane = %q after a survey, want the chosen agent still shown", body)
	}
}

// A message longer than the pane keeps its end, under an ellipsis: an agent
// ends on what it did and what it needs. One that fits is shown whole.
func TestTheMessageKeepsItsTailWhenItDoesNotFit(t *testing.T) {
	var said []string
	for i := range 40 {
		said = append(said, fmt.Sprintf("line %02d", i))
	}
	agent := saidAgent{status: revier.StatusIdle, said: strings.Join(said, "\n")}

	tall, _ := saidWorld(t, 140, 90, agent)
	if body := pane(tall); !strings.Contains(body, "line 00") || !strings.Contains(body, "line 39") || strings.Contains(body, "...") {
		t.Errorf("tall pane does not show the message whole:\n%s", body)
	}
	short, _ := saidWorld(t, 140, 30, agent)
	body := pane(short)
	if !strings.Contains(body, "line 39") || !strings.Contains(body, "...") || strings.Contains(body, "line 00") {
		t.Errorf("short pane does not keep the message's end under an ellipsis:\n%s", body)
	}
	if strings.Index(body, "...") > strings.Index(body, "line 39") {
		t.Errorf("the ellipsis is after the end it stands before:\n%s", body)
	}
}

// A wide pane puts the message beside the facts, level with the name, and
// sets it as wide as the stacked pane just under the switch does: a resize
// across the switch moves the message and does not re-wrap it.
func TestAWidePaneLaysTheMessageBesideTheFactsWrappedAlike(t *testing.T) {
	var words []string
	for i := range 60 {
		words = append(words, fmt.Sprintf("w%03d", i))
	}
	agent := saidAgent{status: revier.StatusIdle, said: strings.Join(words, " ")}

	wide, _ := saidWorld(t, 300, 40, agent)
	top := strings.Split(pane(wide), "\n")[0]
	if !strings.HasPrefix(top, "Project  duo") || !strings.Contains(top, "Last message") {
		t.Errorf("pane top = %q, want the name and the message's heading on one line", top)
	}
	stacked, _ := saidWorld(t, 250, 40, agent)
	if top := strings.Split(pane(stacked), "\n")[0]; strings.Contains(top, "Last message") {
		t.Fatalf("pane top = %q at 250 columns, want the message under the facts", top)
	}
	if w, s := messageLines(wide), messageLines(stacked); len(w) < 2 || !slices.Equal(w, s) {
		t.Errorf("the message wraps differently across the switch:\nwide    %q\nstacked %q", w, s)
	}
}

// messageLines is the message's words on each pane line that carries them,
// wherever on the line they stand.
func messageLines(m tui.Model) []string {
	words := regexp.MustCompile(`w\d{3}( w\d{3})*`)
	var out []string
	for _, line := range strings.Split(pane(m), "\n") {
		if found := words.FindString(line); found != "" {
			out = append(out, found)
		}
	}
	return out
}

// An agent whose probe can say nothing of it says so, rather than leaving the
// pane blank as if it were still reading.
func TestAnAgentWithNothingReadableSaysSo(t *testing.T) {
	m, _ := saidWorld(t, 140, 30, saidAgent{status: revier.StatusIdle})
	if body := pane(m); !strings.Contains(body, "Nothing this agent said can be read.") {
		t.Errorf("pane = %q, want it to say nothing can be read", body)
	}
}

// Markdown reads as text: the markers an agent writes are not shown, and a
// table is set in columns. How each part is set is markdown's own test.
func TestTheMessageIsSetFromItsMarkdown(t *testing.T) {
	said := "## What I did\n\n| Step | Result |\n|---|---|\n| Merge | done |\n\n- **Tests:** `go test` passed\n\n```\ngit log -1\n```"
	m, _ := saidWorld(t, 140, 30, saidAgent{status: revier.StatusIdle, said: said})
	body := pane(m)
	for _, want := range []string{"What I did", "Step   Result", "Merge  done", "• Tests: go test passed", "git log -1"} {
		if !strings.Contains(body, want) {
			t.Errorf("pane has no %q:\n%s", want, body)
		}
	}
	for _, marker := range []string{"##", "**", "`", "|---", "```"} {
		if strings.Contains(body, marker) {
			t.Errorf("pane shows the markdown marker %q:\n%s", marker, body)
		}
	}
}

// A remote project's agent speaks on the other machine, and asking that
// revier is not built: the pane says so, and nothing is read here.
func TestARemoteProjectsAgentSaysTheMessageIsNotImplemented(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 140, 30).Said()
	if body := strings.Join(strings.Fields(pane(m)), " "); !strings.Contains(body, "on another machine is not implemented yet.") {
		t.Errorf("pane = %q, want it to say the message is not implemented for a remote agent", body)
	}
}
