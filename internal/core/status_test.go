package core_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// sharedStatuses is config.toml's statuses: one at each place.
var sharedStatuses = []core.Status{
	{At: core.StatusRuleLeft, Run: []string{"rule-left"}},
	{At: core.StatusRuleRight, Run: []string{"rule-right"}, Every: time.Minute},
	{At: core.StatusProjectLine, Run: []string{"ci"}, Every: 30 * time.Second},
	{At: core.StatusProject, Title: "Info", Run: []string{"info"}, Every: time.Minute},
}

func statusProject(remote *revier.Link, own ...core.Status) core.Project {
	p := core.PrepareProject(revier.Project{Name: "revier", Path: "/src/revier", Remote: remote})
	p.Status = own
	return p
}

func TestStatusOfTakesTheSharedPaneStatusesAndNotTheRule(t *testing.T) {
	got, errs := statusProject(nil).StatusOf(sharedStatuses)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	want := []core.Status{sharedStatuses[2], sharedStatuses[3]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StatusOf = %+v, want %+v", got, want)
	}
}

func TestStatusOfMergesAProjectTableIntoTheSharedOneFieldByField(t *testing.T) {
	p := statusProject(nil,
		core.Status{At: core.StatusProject, Title: "Build"},
		core.Status{At: core.StatusProjectLine, Run: []string{"ci", "--branch"}},
	)
	got, errs := p.StatusOf(sharedStatuses)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	want := []core.Status{
		{At: core.StatusProjectLine, Run: []string{"ci", "--branch"}, Every: 30 * time.Second},
		{At: core.StatusProject, Title: "Build", Run: []string{"info"}, Every: time.Minute},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StatusOf = %+v, want %+v", got, want)
	}
}

func TestStatusOfMergesEveryAlone(t *testing.T) {
	p := statusProject(nil, core.Status{At: core.StatusProjectLine, Every: 10 * time.Second})
	got, _ := p.StatusOf(sharedStatuses)
	want := core.Status{At: core.StatusProjectLine, Run: []string{"ci"}, Every: 10 * time.Second}
	if len(got) == 0 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("StatusOf = %+v, want first %+v", got, want)
	}
}

func TestStatusOfAddsAProjectTableAtAPlaceNoSharedOneFills(t *testing.T) {
	shared := []core.Status{sharedStatuses[0], sharedStatuses[2]}
	own := core.Status{At: core.StatusProject, Title: "Deploy", Run: []string{"deploy-state"}}
	got, errs := statusProject(nil, own).StatusOf(shared)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	want := []core.Status{sharedStatuses[2], own}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StatusOf = %+v, want %+v", got, want)
	}
}

func TestStatusOfOfALinkTakesItsOwnAlone(t *testing.T) {
	own := core.Status{At: core.StatusProject, Run: []string{"remote-info"}}
	got, errs := statusProject(&revier.Link{Host: "buildbox"}, own).StatusOf(sharedStatuses)
	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
	if want := []core.Status{own}; !reflect.DeepEqual(got, want) {
		t.Errorf("StatusOf = %+v, want %+v", got, want)
	}
}

func TestStatusOfOfALinkWithNoTableOfItsOwnIsEmpty(t *testing.T) {
	got, errs := statusProject(&revier.Link{Host: "buildbox"}).StatusOf(sharedStatuses)
	if len(got) != 0 || len(errs) != 0 {
		t.Errorf("StatusOf = %+v, %v; want nothing", got, errs)
	}
}

func TestStatusOfLeavesOutAStatusThatRunsNothing(t *testing.T) {
	shared := []core.Status{sharedStatuses[2]}
	p := statusProject(nil,
		core.Status{At: core.StatusProject, Title: "Build"},
		core.Status{At: core.StatusProjectLine, Every: 10 * time.Second},
	)
	got, errs := p.StatusOf(shared)
	want := []core.Status{{At: core.StatusProjectLine, Run: []string{"ci"}, Every: 10 * time.Second}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StatusOf = %+v, want %+v", got, want)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `"project"`) {
		t.Errorf("errors = %v, want one naming status \"project\"", errs)
	}
}

func TestStatusOfOfALinkLeavesOutItsTableThatRunsNothing(t *testing.T) {
	p := statusProject(&revier.Link{Host: "buildbox"}, core.Status{At: core.StatusProjectLine, Every: 10 * time.Second})
	got, errs := p.StatusOf(sharedStatuses)
	if len(got) != 0 {
		t.Errorf("StatusOf = %+v, want nothing: a link does not take the shared run", got)
	}
	if len(errs) != 1 {
		t.Errorf("errors = %v, want one", errs)
	}
}

func TestStatusOfDoesNotChangeTheSharedStatuses(t *testing.T) {
	shared := []core.Status{{At: core.StatusProject, Title: "Info", Run: []string{"info"}}}
	statusProject(nil, core.Status{At: core.StatusProject, Title: "Build"}).StatusOf(shared)
	if shared[0].Title != "Info" {
		t.Errorf("shared title = %q, want Info", shared[0].Title)
	}
}
