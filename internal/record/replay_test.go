package record_test

import (
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

// sequence is the recorded morning in testdata/sequence: a go that opens
// the taskmgr editor at 09:02, gnome failing to list from 09:05 to 09:10,
// dt unreachable from 09:05:30 to 09:08, and taskmgr selected at 09:10:30.
func sequence(t *testing.T, when time.Time) record.State {
	t.Helper()
	return record.Replay(read(t, fixture("sequence"), when), when)
}

func targetsOf(m record.Machine) []string {
	var out []string
	for _, tg := range m.Targets {
		out = append(out, string(tg.Project)+":"+string(tg.Target))
	}
	return out
}

func sessionsOf(m record.Machine) []revier.SessionID {
	var out []revier.SessionID
	for _, a := range m.Agents {
		out = append(out, a.Session)
	}
	return out
}

func refsOf(m record.Machine) []string {
	var out []string
	for _, i := range m.Instances {
		out = append(out, i.Host+" "+i.Ref)
	}
	return out
}

func hostOf(m record.Machine, host string) record.HostState {
	for _, h := range m.Hosts {
		if h.Host == host {
			return h
		}
	}
	return record.HostState{}
}

func TestReplayOfARecordedSequence(t *testing.T) {
	for _, tc := range []struct {
		clock     string
		current   revier.ProjectName
		targets   []string
		instances []string
		here, dt  []revier.SessionID
	}{
		{
			clock: "09:01:00", current: "revier",
			targets:   []string{"far:home", "revier:workspace", "taskmgr:browser"},
			instances: []string{"gnome 0x1a", "kitty 3", "kitty 5"},
			here:      []revier.SessionID{"s1"}, dt: []revier.SessionID{"s9"},
		},
		{
			clock: "09:04:00", current: "revier",
			targets:   []string{"far:home", "revier:workspace", "taskmgr:browser", "taskmgr:editor"},
			instances: []string{"gnome 0x1a", "gnome 0x2b", "kitty 3", "kitty 5"},
			here:      []revier.SessionID{"s1", "s2"}, dt: []revier.SessionID{"s9"},
		},
		{
			clock: "09:11:30", current: "taskmgr",
			targets:   []string{"far:home", "revier:workspace", "taskmgr:editor"},
			instances: []string{"gnome 0x2b", "kitty 3", "kitty 5"},
			here:      []revier.SessionID{"s1"}, dt: []revier.SessionID{"s9"},
		},
	} {
		st := sequence(t, at("2026-10-16", tc.clock))
		here, dt := machineOf(t, st, ""), machineOf(t, st, "dt")
		if st.Current != tc.current || !slices.Equal(st.Projects, []revier.ProjectName{"far", "revier", "taskmgr"}) {
			t.Errorf("at %s: current %q of %v, want %q of far, revier, taskmgr", tc.clock, st.Current, st.Projects, tc.current)
		}
		if got := targetsOf(here); !slices.Equal(got, tc.targets) {
			t.Errorf("at %s: targets = %v, want %v", tc.clock, got, tc.targets)
		}
		if got := refsOf(here); !slices.Equal(got, tc.instances) {
			t.Errorf("at %s: instances = %v, want %v", tc.clock, got, tc.instances)
		}
		if got := sessionsOf(here); !slices.Equal(got, tc.here) {
			t.Errorf("at %s: agents here = %v, want %v", tc.clock, got, tc.here)
		}
		if got := sessionsOf(dt); !slices.Equal(got, tc.dt) {
			t.Errorf("at %s: agents of dt = %v, want %v", tc.clock, got, tc.dt)
		}
		if here.Unknown != "" || dt.Unknown != "" || hostOf(here, "gnome").Unknown != "" {
			t.Errorf("at %s: here %+v, dt %+v, want every host known", tc.clock, here, dt)
		}
	}
}

func TestAnUnlistedHostIsUnknownNotEmpty(t *testing.T) {
	here := machineOf(t, sequence(t, at("2026-10-16", "09:07:00")), "")

	if got := hostOf(here, "gnome").Unknown; got != "wctl list: timeout" {
		t.Errorf("gnome = %q, want unknown with its error", got)
	}
	if got := hostOf(here, "kitty"); got.Host != "kitty" || got.Unknown != "" {
		t.Errorf("kitty = %+v, want listed", got)
	}
	if got := targetsOf(here); !slices.Contains(got, "taskmgr:browser") || !slices.Contains(got, "taskmgr:editor") {
		t.Errorf("targets = %v, want gnome's last targets kept", got)
	}
}

func TestAnUnreachableMachineIsUnknownNotEmpty(t *testing.T) {
	dt := machineOf(t, sequence(t, at("2026-10-16", "09:07:00")), "dt")

	if dt.Unknown != "ssh: connect to host dt: timeout" {
		t.Errorf("dt unknown = %q, want the ssh error", dt.Unknown)
	}
	if got := sessionsOf(dt); !slices.Equal(got, []revier.SessionID{"s9"}) {
		t.Errorf("agents of dt = %v, want the last seen kept", got)
	}
}

func TestNoSurveyForSixMinutesIsStale(t *testing.T) {
	st := sequence(t, at("2026-10-16", "09:20:00"))
	here := machineOf(t, st, "")

	if want := at("2026-10-16", "09:16:00"); !here.StaleSince.Equal(want) {
		t.Errorf("stale since %v, want %v: six minutes after the last line", here.StaleSince, want)
	}
	if got := targetsOf(here); len(got) != 3 {
		t.Errorf("targets = %v, want the last seen still listed", got)
	}
	if fresh := machineOf(t, sequence(t, at("2026-10-16", "09:15:00")), ""); !fresh.StaleSince.IsZero() {
		t.Errorf("five minutes after the last line, stale since %v, want not stale", fresh.StaleSince)
	}
}

func TestNoSurveyRecordedIsUnknown(t *testing.T) {
	st := record.Replay(nil, at("2026-10-16", "09:00:00"))

	if len(st.Machines) != 1 || st.Machines[0].Machine != "" || st.Machines[0].Unknown != record.NoSurvey {
		t.Errorf("machines = %+v, want this machine unknown: %s", st.Machines, record.NoSurvey)
	}
}

func TestARepeatedLineChangesNothing(t *testing.T) {
	start := at("2026-10-16", "09:00:00")
	opened := record.Line{Kind: record.KindTargetOpened, By: record.BySaw, Project: "taskmgr", Target: "editor", Host: "gnome", Ref: "0x2b"}
	closed := opened
	closed.Kind = record.KindTargetClosed
	snapshot := desktop()
	var lines []record.Line
	for i, l := range []record.Line{
		{Kind: record.KindSnapshot, By: record.BySaw, Seen: &snapshot},
		opened, opened, closed, closed, opened,
	} {
		l.Time, l.Pid = start.Add(time.Duration(i)*time.Second), 4242+i%2
		lines = append(lines, l)
	}

	st := record.Replay(lines, start.Add(time.Minute))
	if got := targetsOf(machineOf(t, st, "")); !slices.Equal(got, []string{"revier:workspace", "taskmgr:browser", "taskmgr:editor"}) {
		t.Errorf("targets = %v, want the editor once", got)
	}
}

func TestDoneLinesAfterTheLastSurveyAreSince(t *testing.T) {
	st := sequence(t, at("2026-10-16", "09:11:30"))

	if len(st.Since) != 1 || st.Since[0].Kind != record.KindGo || st.Since[0].Target != "editor" {
		t.Errorf("since = %+v, want the go at 09:11 alone", st.Since)
	}
	if st := sequence(t, at("2026-10-16", "09:07:00")); len(st.Since) != 0 {
		t.Errorf("since at 09:07 = %+v, want none: the go at 09:02 was surveyed", st.Since)
	}
}

// A replay changes none of the lines it reads: a host that stops and one
// that starts listing after a snapshot leave that snapshot as it was
// written, so a second replay of the same lines starts from it again.
func TestAReplayLeavesTheSnapshotItStartsAtAsItWas(t *testing.T) {
	start := at("2026-10-16", "09:00:00")
	snapshot := record.Seen{Hosts: []string{"kitty", "tmux"}, Unlisted: map[string]string{"gnome": "wctl list: timeout"}}
	lines := []record.Line{
		{Time: start, Kind: record.KindSnapshot, By: record.BySaw, Seen: &snapshot},
		{Time: start.Add(time.Minute), Kind: record.KindHostUnreachable, By: record.BySaw, Host: "tmux", Err: "no server"},
		{Time: start.Add(2 * time.Minute), Kind: record.KindHostBack, By: record.BySaw, Host: "gnome"},
	}

	record.Replay(lines, start.Add(3*time.Minute))
	if !slices.Equal(snapshot.Hosts, []string{"kitty", "tmux"}) || snapshot.Unlisted["gnome"] != "wctl list: timeout" {
		t.Errorf("snapshot = %+v after a replay, want it as written", snapshot)
	}
	if got := hostOf(machineOf(t, record.Replay(lines, start.Add(30*time.Second)), ""), "gnome"); got.Unknown == "" {
		t.Errorf("gnome = %+v in a second replay before it listed again, want unknown", got)
	}
}
