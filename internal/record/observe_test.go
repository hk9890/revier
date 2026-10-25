package record_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/pkg/revier"
)

func TestTheFirstSurveyOfAScopeIsASnapshot(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	dt := record.Seen{Machine: "dt", Projects: []revier.ProjectName{"far"}}

	here := o.Saw(desktop(), now)
	there := o.Saw(dt, now)

	if len(here) != 1 || here[0].Kind != record.KindSnapshot || !reflect.DeepEqual(*here[0].Seen, desktop()) {
		t.Errorf("first survey here = %+v, want one snapshot of the whole survey", here)
	}
	if here[0].By != record.BySaw || !here[0].Time.Equal(now) {
		t.Errorf("snapshot = %+v, want a saw line at %v", here[0], now)
	}
	if len(there) != 1 || there[0].Kind != record.KindSnapshot || there[0].Machine != "dt" {
		t.Errorf("first survey of dt = %+v, want a snapshot of its own", there)
	}
}

func TestAHostThatCannotListClosesNothing(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	o.Saw(desktop(), now)

	failing := desktop()
	failing.Hosts = []string{"gnome"}
	failing.Unlisted = map[string]string{"kitty": "kitten @ ls: timeout"}
	failing.Instances = failing.Instances[1:]
	failing.Targets = failing.Targets[1:]
	failing.Agents = nil
	now = now.Add(10 * time.Second)
	down := o.Saw(failing, now)

	if !slices.Equal(kinds(down), []string{record.KindHostUnreachable}) || down[0].Host != "kitty" || down[0].Err != "kitten @ ls: timeout" {
		t.Errorf("survey while kitty cannot list = %+v, want one host unreachable for kitty with its error", down)
	}

	now = now.Add(10 * time.Second)
	back := o.Saw(desktop(), now)
	if !slices.Equal(kinds(back), []string{record.KindHostBack}) || back[0].Host != "kitty" {
		t.Errorf("survey once kitty lists again = %+v, want one host back and nothing opened", back)
	}
}

func TestAnUnreachableMachineClosesNothing(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	dt := record.Seen{Machine: "dt", Projects: []revier.ProjectName{"far"}, Agents: []record.Agent{
		{Project: "far", Host: "kitty", Ref: "5", Panel: "2", Harness: "claude", Session: "s9"},
	}}
	o.Saw(dt, now)

	gone := record.Seen{Machine: "dt", Projects: dt.Projects, Unreachable: "ssh: connect to host dt: timeout"}
	down := o.Saw(gone, now.Add(time.Minute))
	back := o.Saw(dt, now.Add(2*time.Minute))

	if !slices.Equal(kinds(down), []string{record.KindHostUnreachable}) || down[0].Machine != "dt" || down[0].Host != "" {
		t.Errorf("survey while dt is unreachable = %+v, want one host unreachable for dt", down)
	}
	if !slices.Equal(kinds(back), []string{record.KindHostBack}) {
		t.Errorf("survey once dt answers = %+v, want one host back and nothing opened", back)
	}
}

func TestAChangeIsWrittenAsWhatOpenedAndClosed(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	o.Saw(desktop(), now)

	next := desktop()
	next.Instances = append(next.Instances[:1], record.Instance{Host: "gnome", Ref: "0x2b", Title: "editor"})
	next.Targets = append(next.Targets[:1], record.Target{Project: "taskmgr", Target: "editor", Host: "gnome", Ref: "0x2b"})
	next.Agents[0].Session = "s2"
	got := o.Saw(next, now.Add(10*time.Second))

	want := []string{
		record.KindInstanceOpened, record.KindInstanceClosed,
		record.KindTargetOpened, record.KindTargetClosed,
		record.KindAgentSession,
	}
	if !slices.Equal(kinds(got), want) {
		t.Fatalf("kinds = %v, want %v", kinds(got), want)
	}
	if got[0].Ref != "0x2b" || got[1].Ref != "0x1a" || got[4].Session != "s2" {
		t.Errorf("lines = %+v, want the editor opened, the browser closed and the new conversation", got)
	}
}

func TestNoChangeWritesOnlyAHeartbeat(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	o.Saw(desktop(), now)

	if got := o.Saw(desktop(), now.Add(time.Second)); len(got) != 0 {
		t.Errorf("same survey a second later = %+v, want nothing", got)
	}
	if got := o.Saw(desktop(), now.Add(record.Beat)); !slices.Equal(kinds(got), []string{record.KindSurvey}) {
		t.Errorf("same survey a beat later = %v, want one survey line", kinds(got))
	}
	if got := o.Saw(desktop(), now.Add(record.Beat+time.Second)); len(got) != 0 {
		t.Errorf("same survey after the heartbeat = %+v, want nothing", got)
	}
}

func TestTheFirstSurveyOfADayIsASnapshot(t *testing.T) {
	o := record.NewObserver()
	o.Saw(desktop(), at("2026-10-16", "23:59:50"))

	if got := o.Saw(desktop(), at("2026-10-17", "00:00:00")); !slices.Equal(kinds(got), []string{record.KindSnapshot}) {
		t.Errorf("first survey of the 17th = %v, want a snapshot", kinds(got))
	}
}

func TestAChangedProjectListIsASnapshot(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	o.Saw(desktop(), now)

	reordered := desktop()
	slices.Reverse(reordered.Projects)
	if got := o.Saw(reordered, now.Add(time.Second)); len(got) != 0 {
		t.Errorf("the same projects in another order = %v, want nothing", kinds(got))
	}
	created := desktop()
	created.Projects = append(created.Projects, "pi")
	if got := o.Saw(created, now.Add(2*time.Second)); !slices.Equal(kinds(got), []string{record.KindSnapshot}) {
		t.Errorf("a survey with a new project = %v, want a snapshot", kinds(got))
	}
}

func TestATitleChangeWritesNothing(t *testing.T) {
	o := record.NewObserver()
	now := at("2026-10-16", "09:00:00")
	o.Saw(desktop(), now)

	retitled := desktop()
	retitled.Instances[0].Title = "revier - running tests"
	if got := o.Saw(retitled, now.Add(time.Second)); len(got) != 0 {
		t.Errorf("a new title = %+v, want nothing", got)
	}
}
