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

// An action that ran is an event; one that failed is the log's alone.
func TestAnActionThatRanIsAnEvent(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	remote := hosttest.NewRemote("buildbox")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := &app{cfg: &config.Config{}, projects: []core.Project{remoteProject(t)}, state: &state.State{}, stateRoot: root, core: c, out: &bytes.Buffer{}}

	remote.RunArgv = []string{"false"}
	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "far"}); !errors.Is(err, errActionFailed) {
		t.Fatalf("run = %v, want the action's failure", err)
	}
	remote.RunArgv = []string{"true"}
	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "far"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAction || got[0].Project != "far" || got[0].Action != "sync" {
		t.Errorf("events = %+v, want one action sync of far", got)
	}
}
