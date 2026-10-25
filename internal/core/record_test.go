// Layer L2: what the core writes to the state record, and what a survey
// gives it to write, on fake hosts.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// A replay of what the surveys and the operations wrote is, at each step,
// what that step's survey found: two targets pressed open, one closed, the
// window host failing its listing - its window kept as last seen and the host
// unknown - and the host back.
func TestReplayOfSurveysIsWhatEachSurveyFound(t *testing.T) {
	ctx := context.Background()
	rt, wm := hosttest.NewRuntime("rt"), hosttest.New("wm")
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}}
	l := &ledger{}
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{probe}, Ledger: l}
	p := prepared(t, project())
	projects := []core.Project{p}
	observer := record.NewObserver()
	var saw []record.Line
	step := func(what string) {
		t.Helper()
		r, err := c.Survey(ctx, projects)
		if err != nil {
			t.Fatal(err)
		}
		c.Settle(r, projects, time.Now())
		seen := c.SeenHere(r, l.State().Attached)
		at := time.Now()
		saw = append(saw, observer.Saw(seen, at)...)
		got := machineHere(t, record.Replay(append(slices.Clone(saw), l.noted()...), at))
		if !sameEntries(got.Instances, seen.Instances) || !sameEntries(got.Targets, seen.Targets) || !sameEntries(got.Agents, seen.Agents) {
			t.Errorf("%s: replay = %+v, want what the survey found: %+v", what, got, seen)
		}
	}

	for _, target := range []revier.TargetName{"home", "editor"} {
		if _, err := press(ctx, c, p, target); err != nil {
			t.Fatalf("go %s: %v", target, err)
		}
	}
	home := landed(t, c, "home")
	rt.AddPanel(home, agent("2", "abc-123", "/home/user/dev/github/revier/wt"))
	step("both open")
	if got := l.noted(record.KindGo); len(got) != 2 || got[0].Target != "home" || got[1].Target != "editor" || got[0].Err != "" {
		t.Errorf("go lines = %+v, want one for home and one for editor", got)
	}

	rt.Remove(home)
	step("home closed")

	editor := landed(t, c, "editor")
	wm.SetInstancesErr(errors.New("wctl list: timeout"))
	r, err := c.Survey(ctx, projects)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	saw = append(saw, observer.Saw(c.SeenHere(r, nil), at)...)
	got := machineHere(t, record.Replay(append(slices.Clone(saw), l.noted()...), at))
	if !slices.ContainsFunc(got.Hosts, func(h record.HostState) bool {
		return h.Host == "wm" && strings.Contains(h.Unknown, "wctl list: timeout")
	}) {
		t.Errorf("hosts = %+v, want wm unknown with its error", got.Hosts)
	}
	kept := record.Target{Project: "revier", Target: "editor", Host: "wm", Ref: editor.ID}
	if !slices.Contains(got.Targets, kept) {
		t.Errorf("targets = %+v, want the editor kept as last seen while wm cannot list", got.Targets)
	}

	wm.SetInstancesErr(nil)
	step("wm back")
}

// A press that fails is in the record with its error, and is no use of the
// project in the event file.
func TestAFailedGoIsRecordedWithItsError(t *testing.T) {
	events := recording(t)
	rt := hosttest.NewRuntime("rt")
	rt.OpenErr = errors.New("kitty: no socket")
	l := &ledger{}
	c := &core.Core{Runtime: rt, Ledger: l}
	if _, err := press(context.Background(), c, prepared(t, project()), "home"); err == nil {
		t.Fatal("press succeeded, want the open's error")
	}
	got := l.noted(record.KindGo)
	if len(got) != 1 || got[0].Project != "revier" || got[0].Target != "home" || !strings.Contains(got[0].Err, "no socket") {
		t.Errorf("go lines = %+v, want one for revier:home with the error", got)
	}
	if e := events(); len(e) != 0 {
		t.Errorf("events = %+v, want none", e)
	}
}

// A linked host that gave no answer is unreachable, with why, and holds the
// links to it; one that answered holds its agents by the panel here that
// shows them.
func TestSeenThereOfAnUnansweredHostIsUnreachable(t *testing.T) {
	c, rt, remote, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	projects := []core.Project{linkProject(t)}
	ctx := context.Background()

	answered := c.SeenThere(c.SurveyToClose(ctx, projects, "far"))
	want := record.Agent{Project: "far", Host: rt.Name(), Ref: pane.ID, Panel: "9", Harness: "claude"}
	if len(answered) != 1 || answered[0].Machine != "buildbox" || answered[0].Unreachable != "" || !slices.Equal(answered[0].Agents, []record.Agent{want}) {
		t.Errorf("SeenThere = %+v, want buildbox with far's agent in panel 9", answered)
	}

	remote.Err = errors.New("buildbox: connection refused")
	unanswered := c.SeenThere(c.SurveyToClose(ctx, projects, "far"))
	if len(unanswered) != 1 || !strings.Contains(unanswered[0].Unreachable, "connection refused") ||
		!slices.Equal(unanswered[0].Projects, []revier.ProjectName{"far"}) {
		t.Errorf("SeenThere = %+v, want buildbox unreachable, holding far", unanswered)
	}
}

// A survey that asked one linked host says nothing of another: what it has
// of that host's links is the window here that reaches them.
func TestSeenThereLeavesOutAHostNotAsked(t *testing.T) {
	c, _, _, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	c.Remotes["devbox"] = hosttest.NewRemote("devbox", revier.ProjectView{Project: revier.Project{Name: "near"}, PathExists: true})
	near := prepared(t, revier.Project{Name: "near", Remote: &revier.Link{Host: "devbox", Project: "near"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "near", Match: revier.Match{Title: "^near$"}}},
	}})
	projects := []core.Project{linkProject(t), near}
	ctx := context.Background()

	got := c.SeenThere(c.SurveyToClose(ctx, projects, "far"))
	if len(got) != 1 || got[0].Machine != "buildbox" {
		t.Errorf("SeenThere = %+v, want buildbox alone", got)
	}
	local, err := c.SurveyLocal(ctx, projects)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SeenThere(local); len(got) != 0 {
		t.Errorf("SeenThere of a survey that asked no host = %+v, want none", got)
	}
}

// machineHere is what a replay says of this machine.
func machineHere(t *testing.T, st record.State) record.Machine {
	t.Helper()
	for _, m := range st.Machines {
		if m.Machine == "" {
			return m
		}
	}
	t.Fatalf("replay = %+v, want this machine", st)
	return record.Machine{}
}

// sameEntries reports whether a and b hold the same entries, in any order.
func sameEntries[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for _, e := range a {
		if !slices.Contains(b, e) {
			return false
		}
	}
	return true
}
