package tui_test

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// The surface is the one process that surveys all day, so it is the one that
// records which conversation each agent holds: once, however often it looks.
func TestASurveyRecordsTheConversationEachAgentHoldsOnce(t *testing.T) {
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude"})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle, Session: "abc-123"},
	}}}
	projects := core.Prepare([]revier.Project{{Name: "demo", Path: "/p/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:demo", Launch: []string{"x"}, Match: revier.Match{Title: "^session:demo$"}}},
	}}})

	m := refreshed(t, c, projects, root, nil)
	survey(m)

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAgentSession || got[0].Project != "demo" ||
		got[0].Agent != "claude" || got[0].Session != "abc-123" || got[0].Dir != "/p/demo" {
		t.Errorf("events = %+v, want one session abc-123 of demo in /p/demo", got)
	}
}

// A link's agent is its host's to report, so the conversation it holds is
// recorded when the host answers, under the link's name here, and once.
func TestAHostsAnswerRecordsTheConversationALinksAgentHolds(t *testing.T) {
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	said := hostSays("alpha", revier.StatusIdle)
	said.Agents[0].State.Session = "abc-123"
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{
		"buildbox": hosttest.NewRemote("buildbox", said),
	}}

	m := refreshed(t, c, remoteOnDisk(t, "alpha"), root, nil)
	survey(m)

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAgentSession || got[0].Project != "alpha" ||
		got[0].Agent != "claude" || got[0].Session != "abc-123" {
		t.Errorf("events = %+v, want one session abc-123 of alpha", got)
	}
}
