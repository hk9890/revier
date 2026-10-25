package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// stateFixture is a state record under a scratch state root: a surface's
// snapshot of this machine, with the workspace of revier and its agent and
// a shell no project holds, and of buildbox, with far's agent; a go after
// them; and a project close that saved revier alone, asking buildbox
// nothing. It returns the moment between the surveys and the close, and the
// close's session.
func stateFixture(t *testing.T) (time.Time, session.Session) {
	t.Helper()
	root := t.TempDir()
	recordingTo(t, root)
	record.Setup(root, "tui")
	t.Cleanup(func() { record.Setup("", "") })
	at := time.Date(2026, 10, 16, 14, 0, 0, 0, time.Local)
	here := record.Seen{
		Current: "revier", Projects: []revier.ProjectName{"revier", "far"}, Hosts: []string{"kitty"},
		Instances: []record.Instance{{Host: "kitty", Ref: "3", Title: "revier"}, {Host: "kitty", Ref: "12", Title: "zsh"}},
		Targets:   []record.Target{{Project: "revier", Target: "home", Host: "kitty", Ref: "3"}},
		Agents:    []record.Agent{{Project: "revier", Host: "kitty", Ref: "3", Panel: "7", Harness: "claude", Session: "s1", Dir: "/src/revier"}},
	}
	there := record.Seen{
		Machine: "buildbox", Projects: []revier.ProjectName{"far"},
		Agents: []record.Agent{{Project: "far", Host: "kitty", Ref: "5", Panel: "2", Harness: "claude", Session: "s2"}},
	}
	record.Write(
		record.Line{Time: at.Add(-2 * time.Minute), Kind: record.KindSnapshot, By: record.BySaw, Seen: &here},
		record.Line{Time: at.Add(-2 * time.Minute), Kind: record.KindSnapshot, By: record.BySaw, Machine: "buildbox", Seen: &there},
		record.Line{Time: at.Add(-time.Minute), Kind: record.KindGo, By: record.ByDid, Project: "revier", Target: "editor", Err: "gnome: wctl list: timeout"},
	)

	s, _, err := session.Save(root, session.Session{At: at.Add(time.Minute), Reason: session.ReasonProjectClosed, Current: "revier",
		Projects: []session.Project{{Name: "revier", Targets: []session.Target{
			{Name: "home", Agents: []session.Agent{{Harness: "claude", Session: "s1", Dir: "/src/revier"}}},
		}}}})
	if err != nil {
		t.Fatal(err)
	}
	asked := here
	record.Write(
		record.Line{Time: s.At, Kind: record.KindSessionSaved, By: record.ByDid, Saved: s.ID, Reason: s.Reason},
		record.Line{Time: s.At, Kind: record.KindSnapshot, By: record.BySaw, Saved: s.ID, Seen: &asked},
	)
	return at, s
}

// `revier state --at` prints each machine: this one with its host, the
// target and its agent, and the instance no project holds; the linked host
// with its link's agent; and the go no survey has seen yet, with its error.
func TestStatePrintsEachMachine(t *testing.T) {
	at, _ := stateFixture(t)
	var out bytes.Buffer
	if err := run(&out, []string{"state", "--at", at.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"current project: revier",
		"projects: 2, 1 open",
		"this machine  seen ",
		"hosts: kitty listed",
		`home  kitty 3 "revier"`,
		"agent  kitty 3 panel 7  claude  s1  /src/revier",
		`instances no project holds: kitty 12 "zsh"`,
		"buildbox  seen ",
		"agent  kitty 5 panel 2  claude  s2",
		"go revier:editor err: gnome: wctl list: timeout",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("printed:\n%s\nwant %q", out.String(), want)
		}
	}
}

// --json is the same state as one document.
func TestStateJSON(t *testing.T) {
	at, _ := stateFixture(t)
	var out bytes.Buffer
	if err := run(&out, []string{"state", "--at", at.Local().Format(time.DateTime), "--json"}); err != nil {
		t.Fatal(err)
	}
	var got stateAt
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got.Current != "revier" || len(got.Machines) != 2 || got.Machines[1].Machine != "buildbox" ||
		len(got.Machines[0].Targets) != 1 || len(got.Since) != 1 || got.Compare != nil {
		t.Errorf("state = %+v, want this machine and buildbox, and the go since", got)
	}
}

// --session replays at the save and names why the save holds less than was
// open: buildbox, which a project close does not ask.
func TestStateSessionComparesTheSave(t *testing.T) {
	_, s := stateFixture(t)
	var out bytes.Buffer
	if err := run(&out, []string{"state", "--session", s.ID}); err != nil {
		t.Fatal(err)
	}
	want := "in state, not in session: buildbox far agent kitty 5 panel 2  claude  s2: " + record.NotAsked
	if !strings.Contains(out.String(), "session "+s.ID+":") || !strings.Contains(out.String(), want) {
		t.Errorf("printed:\n%s\nwant %q", out.String(), want)
	}
	if strings.Contains(out.String(), record.Unexplained) {
		t.Errorf("printed:\n%s\nwant every difference explained", out.String())
	}
}

// `revier list` is a one-shot survey, and a surface that links to this
// machine runs it every refresh: it records nothing it saw, and only what a
// prune changed in the state.
func TestListWritesNoRecord(t *testing.T) {
	wm := hosttest.New("gnome")
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: wm}
	p, root := demoProject(t), t.TempDir()
	record.Setup(root, "list")
	t.Cleanup(func() { record.Setup("", "") })
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{p}, stateRoot: root, core: c}, &state.State{})

	output(t, a, func() error { return cmdList(context.Background(), a, []string{"--json"}) })
	if _, err := os.Stat(record.Dir(root)); !os.IsNotExist(err) {
		t.Fatalf("record directory: %v, want none written by a list that changed nothing", err)
	}

	gone := revier.TargetRef{Host: "gnome", ID: "99"}
	st := &state.State{}
	st.Bind("demo", "editor", gone)
	a = withState(t, a, st)
	output(t, a, func() error { return cmdList(context.Background(), a, []string{"--json"}) })
	lines, err := record.Read(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Kind != record.KindUnbound || lines[0].Ref != "99" {
		t.Errorf("lines = %+v, want the prune's unbound alone", lines)
	}
}

// A moment given at another offset is the same moment in local time: the
// record names each day file by the local date.
func TestAMomentAtAnotherOffsetIsLocal(t *testing.T) {
	got, err := parseMoment("2026-10-15T23:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC); !got.Equal(want) || got.Location() != time.Local {
		t.Errorf("moment = %v in %v, want %v in local time", got, got.Location(), want)
	}
}
