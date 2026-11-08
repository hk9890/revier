package app_test

import (
	"context"
	"os"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// TestMain keeps every test off the user's own revier files (testenv.Run).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}

// ledger is a ledger in memory: the state, and every event recorded.
type ledger struct {
	st     state.State
	events []revier.Event
	notes  []record.Line
}

func (l *ledger) State() *state.State { st := l.st; return &st }

func (l *ledger) Update(apply func(*state.State) bool) *state.State {
	apply(&l.st)
	return l.State()
}

func (l *ledger) Record(e revier.Event) { l.events = append(l.events, e) }

func (l *ledger) Note(n record.Line) {
	if n.By == "" {
		n.By = record.ByDid
	}
	l.notes = append(l.notes, n)
}

func (l *ledger) MarkPopupHidden() error { return nil }

func (l *ledger) TakePopupHidden() bool { return false }

// desktop is a core whose runtime holds the workspace, with an agent in it,
// and the notes of demo, the project the ledger focused last.
func desktop(t *testing.T) (*core.Core, *hosttest.FakeRuntime, core.Report, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude", Vars: map[string]string{"session": "abc-123"}})
	rt.Add("notes:demo", "kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.NewResumableProbe("claude", "claude")}, Ledger: &ledger{st: state.State{Current: "demo"}}}
	projects := []core.Project{core.PrepareProject(revier.Project{Name: "demo", Path: "/src/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "session:demo", Match: revier.Match{Title: "^session:demo$"}}},
		{Name: "notes", Runtime: &revier.Realization{Name: "notes:demo", Launch: []string{"less"}, Match: revier.Match{Title: "^notes:demo$"}}},
	}})}
	return c, rt, surveyed(t, c, projects), projects
}

func surveyed(t *testing.T, c *core.Core, projects []core.Project) core.Report {
	t.Helper()
	r, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return r
}
