package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// recording gives the test an event file of its own, and returns what is in
// it when called.
func recording(t *testing.T) func() []revier.Event {
	t.Helper()
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	return func() []revier.Event {
		t.Helper()
		got, err := events.Read(root, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range got {
			got[i].Time = time.Time{}
		}
		return got
	}
}

func TestAPressThatLaunchesIsAGoEventThatSaysSo(t *testing.T) {
	recorded := recording(t)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}

	if _, _, err := c.ActivateWaiting(context.Background(), prepared(t, project()), "home", nil, &ledger{}); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}

	want := []revier.Event{{Kind: revier.EventGo, Project: "revier", Target: "home", Launched: true}}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A go is stamped when it was pressed, not when the wait for its window ended:
// an editor that takes its time must not stand behind a press made while it
// came up.
func TestAGoEventCarriesTheTimeOfThePress(t *testing.T) {
	defer func(w time.Duration) { core.BindWait = w }(core.BindWait)
	core.BindWait = 50 * time.Millisecond
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.NewLateWindows("wm")}

	if _, _, err := c.ActivateWaiting(context.Background(), prepared(t, project()), "editor", nil, &ledger{}); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}
	waited := time.Now()

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || waited.Sub(got[0].Time) < core.BindWait {
		t.Errorf("events = %+v, want one go stamped before the wait of %s that ended at %s", got, core.BindWait, waited)
	}
}

// A toggle back is one press that runs Go twice, and writes two lines to the
// log. It is one event, of the target the press landed on.
func TestAToggleBackIsOneEventOfWhereItLanded(t *testing.T) {
	recorded := recording(t)
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	diff := rt.Add("diff:revier", "kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, revier.Project{Name: "revier", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}})
	rt.SetFocus(diff)

	if _, _, err := c.ActivateWaiting(context.Background(), p, "diff", nil, &ledger{}); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}

	want := []revier.Event{{Kind: revier.EventGo, Project: "revier", Target: "home"}}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

func TestAPressThatFailsOrOnlyWaitsIsNoEvent(t *testing.T) {
	recorded := recording(t)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	p := prepared(t, project())

	if _, _, err := c.ActivateWaiting(context.Background(), p, "nope", nil, &ledger{}); err == nil {
		t.Fatal("a press of an unknown target succeeded")
	}
	if _, res, err := c.ActivateWaiting(context.Background(), p, "editor", nil, pendingLedger{&ledger{}}); err != nil || !res.ComingUp {
		t.Fatalf("press = %+v, %v; want the launch still coming up", res, err)
	}

	if got := recorded(); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

// pendingLedger has a launch of every target on record.
type pendingLedger struct{ *ledger }

func (pendingLedger) Pending(revier.ProjectName, revier.TargetName) bool { return true }

// A tab is reached through the workspace that holds it, and the result names
// the workspace for the binding. The event names the tab, and says launched
// for the tab alone: the workspace was up. The press after it, on the tab now
// current, is the toggle back, and reads as the workspace.
func TestAPressOfATabIsAnEventOfTheTab(t *testing.T) {
	recorded := recording(t)
	rt, wm, _ := tabHosts(t)
	c := &core.Core{Runtime: rt, Window: wm}
	p := prepared(t, tabProject())
	l := &ledger{}

	for range 2 {
		if _, _, err := c.ActivateWaiting(context.Background(), p, "tickets", nil, l); err != nil {
			t.Fatalf("ActivateWaiting: %v", err)
		}
	}

	want := []revier.Event{
		{Kind: revier.EventGo, Project: "revier", Target: "tickets", Launched: true},
		{Kind: revier.EventGo, Project: "revier", Target: "home"},
	}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A restore opens what was open before a reboot. It is nobody's use of the
// project, so a count of presses must not see it.
func TestARestoreIsNoGoEvent(t *testing.T) {
	recorded := recording(t)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home"}, {Name: "notes"},
	}}}}

	out, _, _ := restoreOf(t, c, s)

	if opened, _, _ := out.Counts(); opened != 2 {
		t.Fatalf("restore opened %d, want home and notes", opened)
	}
	if got := recorded(); len(got) != 0 {
		t.Errorf("events = %+v, want none", got)
	}
}

func TestAPressOfAnAgentIsAGoAgentEventWithItsConversation(t *testing.T) {
	recorded := recording(t)
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelAgent})
	c := &core.Core{Runtime: rt}
	a := revier.AgentView{Panel: "1", Ref: ref, State: revier.AgentState{Harness: "claude", Session: "abc"}}

	if _, err := c.ActivateAgent(context.Background(), prepared(t, project()), a, nil, false); err != nil {
		t.Fatalf("ActivateAgent: %v", err)
	}

	want := []revier.Event{{Kind: revier.EventGoAgent, Project: "revier", Agent: "claude", Session: "abc"}}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// Each host is asked once, whatever the number of links to it, and its
// events come back under its name.
func TestRemoteEventsAsksEachHostOnceAndNamesIt(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	box := hosttest.NewRemote("buildbox")
	box.Recorded = []revier.Event{{Time: at, Kind: revier.EventGo, Project: "far"}}
	c := &core.Core{Remotes: map[string]revier.Remote{"buildbox": box}}
	projects := []core.Project{
		prepared(t, project()),
		prepared(t, remoteProject("far")),
		prepared(t, remoteProject("other")),
	}

	got, failed := c.RemoteEvents(context.Background(), projects, 3)

	want := []revier.Event{{Time: at, Kind: revier.EventGo, Host: "buildbox", Project: "far"}}
	if !slices.Equal(got, want) || len(failed) != 0 {
		t.Errorf("events = %+v, failed = %v; want %+v", got, failed, want)
	}
	if !slices.Equal(box.Days, []int{3}) {
		t.Errorf("asked for days %v, want one ask for 3", box.Days)
	}
}

func TestAHostThatFailsCostsItsOwnEventsAndIsNamed(t *testing.T) {
	down := hosttest.NewRemote("buildbox")
	down.Err = errors.New("connection refused")
	up := hosttest.NewRemote("laptop")
	up.Recorded = []revier.Event{{Kind: revier.EventGo, Project: "near"}}
	c := &core.Core{Remotes: map[string]revier.Remote{"buildbox": down, "laptop": up}}
	near := remoteProject("near")
	near.Remote.Host = "laptop"

	got, failed := c.RemoteEvents(context.Background(), []core.Project{prepared(t, remoteProject("far")), prepared(t, near)}, 7)

	if len(got) != 1 || got[0].Host != "laptop" {
		t.Errorf("events = %+v, want the laptop's one", got)
	}
	if len(failed) != 1 || !errors.Is(failed["buildbox"], down.Err) {
		t.Errorf("failed = %v, want buildbox with its error", failed)
	}
}
