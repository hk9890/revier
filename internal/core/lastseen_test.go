// Layer L2: what a surface last saw is built from its survey by the walk a
// save uses, and the fake host reports what it walks over.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// What a surface saw is what a save of the same moment records - the tab,
// the listed tab's agent, the named agent, no attachment, and each agent's
// directory - and the probe is asked nothing: the survey read it already.
func TestSeenRecordsWhatSessionRecordsWithoutAskingAProbe(t *testing.T) {
	marked := agent("3", "", "")
	marked.Vars[core.PanelAgentVar] = "dtop"
	listed := agent("4", "def-456", "/p/wt")
	listed.Vars[core.PanelTargetVar] = "agent"
	tab := agent("5", "ghi-789", "/p")
	tab.Vars[core.PanelTargetVar] = "tickets"
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", agent("1", "abc-123", "/p"),
		revier.Panel{ID: "2", Kind: revier.PanelShell, Title: "zsh"}, marked, listed, tab)
	stray := rt.Add("some tool nobody declared", "meld")
	probe := resumable()
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}, Ledger: &ledger{st: state.State{
		Current: "revier", Attached: map[revier.ProjectName][]revier.TargetRef{"revier": {stray}},
	}}}
	p := prepared(t, tabProject())
	p.Agents = []core.NamedAgent{{Name: "dtop", Command: []string{"dtop"}}}
	report, err := c.Survey(context.Background(), []core.Project{p})
	if err != nil {
		t.Fatal(err)
	}

	seen, ok := c.Seen(report, core.RemoteAnswers{}, session.Session{})
	if !ok {
		t.Fatal("Seen took nothing, want what the survey saw")
	}
	if probe.Calls != 0 {
		t.Errorf("Sessions was called %d times, want none", probe.Calls)
	}
	saved, gaps := c.Session(context.Background(), report)
	if gaps.Attached != 1 || len(saved.Projects) != 1 || len(saved.Projects[0].Targets) != 2 || saved.Agents() != 4 {
		t.Fatalf("save = %+v, %+v, want home and the tab with four agents and one attachment left out", saved, gaps)
	}
	if !seen.SameAs(saved) || seen.Current != saved.Current {
		t.Errorf("seen = %+v, want what the save records: %+v", seen, saved)
	}
}

// A host here that could not list hides what it holds, so nothing is taken
// from that survey (decisions.md D89).
func TestSeenTakesNothingWhileAHostHereCannotList(t *testing.T) {
	c, projects := degraded(t, "wm")
	report, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := c.Seen(report, core.RemoteAnswers{}, session.Session{}); ok {
		t.Errorf("Seen = %+v, want nothing while wm cannot list", s)
	}
}

// A held link whose host gives no answer keeps what was last seen of it,
// its agents and their conversations with it.
func TestSeenKeepsALinkAsLastSeenWhileItsHostDoesNotAnswer(t *testing.T) {
	answering := hostAgent("box.4242", revier.StatusIdle)
	answering.State.Session, answering.State.Dir = "abc-123", "/srv/far/wt"
	c, _, remote, _ := linked(t, answering)
	projects := []core.Project{linkProject(t)}
	local, err := c.SurveyLocal(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	prev, ok := c.Seen(local, c.AskRemotes(context.Background(), projects), session.Session{})
	want := []session.Agent{{Harness: "claude", Session: "abc-123", Dir: "/srv/far/wt"}}
	if !ok || len(prev.Projects) != 1 || !slices.Equal(prev.Projects[0].Targets[0].Agents, want) {
		t.Fatalf("Seen = %+v, %v, want the link's home with the host's agent", prev, ok)
	}

	remote.Err = errors.New("buildbox: connection refused")
	got, ok := c.Seen(local, c.AskRemotes(context.Background(), projects), prev)
	if !ok || !got.SameAs(prev) {
		t.Errorf("Seen = %+v, %v, want what was last seen of far: %+v", got, ok, prev)
	}
}

// A held link whose host never answered and that was never seen leaves
// nothing to keep in its place, so nothing is taken.
func TestSeenTakesNothingForALinkNeverAnsweredAndNeverSeen(t *testing.T) {
	c, _, remote, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	remote.Err = errors.New("buildbox: connection refused")
	projects := []core.Project{linkProject(t)}
	local, err := c.SurveyLocal(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	for _, answers := range []core.RemoteAnswers{{}, c.AskRemotes(context.Background(), projects)} {
		if s, ok := c.Seen(local, answers, session.Session{}); ok {
			t.Errorf("Seen = %+v, want nothing for a link never seen", s)
		}
	}
}

// A snapshot lost something when a target it holds is not open now, and
// says nothing while a host here cannot list.
func TestLostIsATargetTheSnapshotHoldsThatIsNotOpen(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	seen, ok := c.Seen(report, core.RemoteAnswers{}, session.Session{})
	if !ok || seen.Targets() != 3 {
		t.Fatalf("Seen = %+v, %v, want the three open targets", seen, ok)
	}
	if c.Lost(seen, report) {
		t.Error("Lost with every target open, want false")
	}

	closed := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	if !closed.Lost(seen, survey(t, closed, projects, nil)) {
		t.Error("Lost with nothing open, want true")
	}

	failing, projects := degraded(t, "rt")
	if failing.Lost(seen, survey(t, failing, projects, nil)) {
		t.Error("Lost while rt cannot list, want false")
	}
}
