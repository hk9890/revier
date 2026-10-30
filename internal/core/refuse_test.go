package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// A delete or a rename is refused while anything of the project runs here or
// may, and goes ahead when only another host's refs name it.
func TestRefuseChange(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: hosttest.New("gnome")}
	idle := revier.ProjectView{Project: revier.Project{Name: "demo"}, Targets: []revier.TargetView{{Name: "home"}}}
	running := idle
	running.Targets = []revier.TargetView{{Name: "home", Ref: revier.TargetRef{Host: "tmux", ID: "1"}}}
	unknown := idle
	unknown.Targets = []revier.TargetView{{Name: "home", Unknown: "tmux could not list"}}
	bound := func(host string) map[revier.ProjectName]map[revier.TargetName]revier.TargetRef {
		return map[revier.ProjectName]map[revier.TargetName]revier.TargetRef{"demo": {"home": {Host: host, ID: "1"}}}
	}
	attached := func(host string) map[revier.ProjectName][]revier.TargetRef {
		return map[revier.ProjectName][]revier.TargetRef{"demo": {{Host: host, ID: "9"}}}
	}

	for _, tc := range []struct {
		name    string
		view    revier.ProjectView
		settled core.Settled
		want    string // "" for no refusal
	}{
		{"nothing runs", idle, core.Settled{Surveyed: true}, ""},
		{"no survey yet", idle, core.Settled{}, "no survey has answered yet; wait for it before renaming demo"},
		{"the survey saw it run", running, core.Settled{Surveyed: true}, "demo is running"},
		{"state bound it on a host here", idle, core.Settled{Surveyed: true, Bound: bound("tmux")}, "demo is running"},
		{"state bound it on another host", idle, core.Settled{Surveyed: true, Bound: bound("kitty")}, ""},
		{"its host could not list", unknown, core.Settled{Surveyed: true}, "tmux could not list; wait for the host before renaming it"},
		{"a window attached here", idle, core.Settled{Surveyed: true, Attached: attached("gnome")}, "demo has an attached window open"},
		{"a window attached on another host", idle, core.Settled{Surveyed: true, Attached: attached("other")}, ""},
		{"a launch of it still to show its window", idle, core.Settled{Surveyed: true, Launch: &state.Launch{Project: "demo", Target: "home", At: time.Now()}}, "demo is coming up; wait for its window before renaming it"},
		{"a launch of it past its window", idle, core.Settled{Surveyed: true, Launch: &state.Launch{Project: "demo", Target: "home", At: time.Now().Add(-time.Hour)}}, ""},
		{"a launch of another project", idle, core.Settled{Surveyed: true, Launch: &state.Launch{Project: "other", Target: "home", At: time.Now()}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := c.RefuseChange(tc.view, tc.settled, "renaming")
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("RefuseChange = %v, want nil", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("RefuseChange = %v, want %q", err, tc.want)
			}
		})
	}
}
