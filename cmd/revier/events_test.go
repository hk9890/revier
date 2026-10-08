package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// recordingTo makes root the state root of this process, as openLog does for a
// real one, and stops the recording when the test ends.
func recordingTo(t *testing.T, root string) {
	t.Helper()
	t.Setenv("REVIER_STATE_HOME", root)
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
}

// lines decodes what `revier events` printed, one event per line.
func lines(t *testing.T, out string) []revier.Event {
	t.Helper()
	var got []revier.Event
	for line := range strings.Lines(out) {
		var e revier.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		got = append(got, e)
	}
	return got
}

// `--local` reads the file and nothing else: no project file, no host.
func TestEventsLocalPrintsTheLastDaysOneLineEach(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	now := time.Now()
	events.Record(revier.Event{Time: now.AddDate(0, 0, -5), Kind: revier.EventGo, Project: "old"})
	events.Record(revier.Event{Time: now.Add(-time.Hour), Kind: revier.EventAgentNew, Project: "demo", Target: "home"})

	var out bytes.Buffer
	if err := run(&out, []string{"events", "--local", "--days", "3"}); err != nil {
		t.Fatalf("events: %v", err)
	}

	got := lines(t, out.String())
	if len(got) != 1 || got[0].Kind != revier.EventAgentNew || got[0].Project != "demo" {
		t.Errorf("printed %q, want the agent new of demo alone", out.String())
	}
	if !strings.Contains(out.String(), `"event":"agent new","project":"demo","target":"home"`) {
		t.Errorf("printed %q, want the documented field names", out.String())
	}
}

func TestEventsRefusesADayCountBelowOneAndAnArgument(t *testing.T) {
	recordingTo(t, t.TempDir())
	for _, args := range [][]string{{"events", "--days", "0"}, {"events", "demo"}} {
		if err := run(&bytes.Buffer{}, args); err == nil || !strings.Contains(err.Error(), "usage: revier events") {
			t.Errorf("%v: err = %v, want the usage", args, err)
		}
	}
}

// A day count past what a Duration holds wraps, and the read then starts in
// the future and prints nothing. It is refused by its number.
func TestEventsRefusesADayCountADurationCannotHold(t *testing.T) {
	recordingTo(t, t.TempDir())
	err := run(&bytes.Buffer{}, []string{"events", "--local", "--days", "106752"})
	if err == nil || !strings.Contains(err.Error(), "--days 106752") {
		t.Errorf("err = %v, want the day count refused", err)
	}
	if err := run(&bytes.Buffer{}, []string{"events", "--local", "--days", "106751"}); err != nil {
		t.Errorf("the most days a Duration holds: %v", err)
	}
}

// The file is in the order it was written. A clock set back writes an earlier
// time after a later one, and the lines are printed by time all the same.
func TestEventsLocalPrintsByTimeWhateverTheOrderOfTheFile(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	now := time.Now()
	events.Record(revier.Event{Time: now.Add(-time.Hour), Kind: revier.EventGo, Project: "later"})
	events.Record(revier.Event{Time: now.Add(-2 * time.Hour), Kind: revier.EventGo, Project: "earlier"})

	var out bytes.Buffer
	if err := run(&out, []string{"events", "--local"}); err != nil {
		t.Fatalf("events: %v", err)
	}

	got := lines(t, out.String())
	if len(got) != 2 || got[0].Project != "earlier" || got[1].Project != "later" {
		t.Errorf("printed %q, want earlier before later", out.String())
	}
}

// What a linked host recorded is printed among what this machine did, in the
// order it happened, under the host's name.
func TestEventsMergesWhatALinkedHostRecordedByTime(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	now := time.Now()
	events.Record(revier.Event{Time: now.Add(-3 * time.Hour), Kind: revier.EventGo, Project: "demo"})
	events.Record(revier.Event{Time: now.Add(-time.Hour), Kind: revier.EventGo, Project: "far"})
	remote := hosttest.NewRemote("buildbox")
	remote.Recorded = []revier.Event{{Time: now.Add(-2 * time.Hour), Kind: revier.EventAgentSession, Project: "far", Agent: "claude", Session: "abc", Dir: "/src/far"}}
	c := &core.Core{Remotes: map[string]revier.Remote{"buildbox": remote}}

	var out bytes.Buffer
	if err := writeEvents(context.Background(), &out, root, 1, c, []core.Project{demoProject(t), remoteProject(t)}); err != nil {
		t.Fatalf("writeEvents: %v", err)
	}

	got := lines(t, out.String())
	if len(got) != 3 || got[0].Project != "demo" || got[1].Host != "buildbox" || got[1].Session != "abc" || got[2].Project != "far" || got[2].Host != "" {
		t.Errorf("printed %q, want demo, then buildbox's session, then far", out.String())
	}
}

// A host that is down costs its own events: the command prints the rest and
// succeeds.
func TestEventsPrintsTheLocalOnesWhenAHostDoesNotAnswer(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	events.Record(revier.Event{Kind: revier.EventGo, Project: "demo"})
	remote := hosttest.NewRemote("buildbox")
	remote.Err = errors.New("connection refused")
	c := &core.Core{Remotes: map[string]revier.Remote{"buildbox": remote}}

	var out bytes.Buffer
	if err := writeEvents(context.Background(), &out, root, 1, c, []core.Project{remoteProject(t)}); err != nil {
		t.Fatalf("writeEvents: %v", err)
	}
	if got := lines(t, out.String()); len(got) != 1 || got[0].Project != "demo" {
		t.Errorf("printed %q, want demo's event", out.String())
	}
}

// A tab whose agent was not resumed holds a conversation of its own, so its
// event names none: no probe here resumes anything.
func TestAnAgentTabThatWasNotResumedNamesNoConversation(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "", revier.Panel{ID: "%1", Kind: revier.PanelAgent})
	p := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Match: revier.Match{Title: "^home$"}, Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}}}},
	}})
	a := &app{cfg: &config.Config{}, projects: []core.Project{p}, state: &state.State{}, stateRoot: root, core: &core.Core{Runtime: rt}}

	if err := a.newAgent(context.Background(), "demo", "", "", "abc-123"); err != nil {
		t.Fatalf("agent new: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAgentNew || got[0].Target != "home" || got[0].Session != "" {
		t.Errorf("events = %+v, want one agent new in home with no conversation", got)
	}
}

// acting is the app over one project and the actions it can run.
func acting(root string, p core.Project, c *core.Core, actions ...config.Action) *app {
	return &app{cfg: &config.Config{Actions: actions}, projects: []core.Project{p}, state: &state.State{}, stateRoot: root, core: c, out: &bytes.Buffer{}}
}

// localProject is a project on this machine, in a directory that exists.
func localProject(t *testing.T) core.Project {
	t.Helper()
	return core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir()})
}

// An action that ran is an event; one that failed is the log's alone.
func TestAnActionThatRanIsAnEvent(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux")}
	a := acting(root, localProject(t), c, config.Action{Name: "broken", Run: []string{"false"}}, config.Action{Name: "sync", Run: []string{"true"}})

	if err := cmdRun(context.Background(), a, []string{"broken", "-p", "demo"}); !errors.Is(err, errActionFailed) {
		t.Fatalf("run = %v, want the action's failure", err)
	}
	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "demo"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAction || got[0].Project != "demo" || got[0].Action != "sync" {
		t.Errorf("events = %+v, want one action sync of demo", got)
	}
}

// A link's action runs as a `revier run` on its host, which records it there:
// one here as well would be a second line of `revier events` for one action.
func TestALinksActionIsNoEventHere(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"true"}
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}

	if err := cmdRun(context.Background(), acting(root, remoteProject(t), c), []string{"sync", "-p", "far"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("events = %+v, want none: the host recorded it", got)
	}
}

// An action is stamped when it started: an editor exits hours after the press
// that opened it.
func TestAnActionsEventCarriesTheTimeItStarted(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	const ran = 100 * time.Millisecond
	a := acting(root, localProject(t), &core.Core{Runtime: hosttest.NewRuntime("tmux")}, config.Action{Name: "wait", Run: []string{"sleep", "0.1"}})

	if err := cmdRun(context.Background(), a, []string{"wait", "-p", "demo"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	exited := time.Now()

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || exited.Sub(got[0].Time) < ran {
		t.Errorf("events = %+v, want one action stamped before the %s it ran, which ended at %s", got, ran, exited)
	}
}
