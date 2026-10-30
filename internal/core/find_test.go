package core_test

import (
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A word finds a project by a part of its name, its directory, its
// repository, its name on a link's host or an agent's activity, whatever the
// case, and by the whole of the name a harness gives an agent alone.
func TestAWordFindsAProjectByItsFields(t *testing.T) {
	tm := revier.ProjectView{Project: revier.Project{Name: "tm", Path: "/x/task-manager", GitURL: "git@github.com:o/taskmgr.git"}}
	link := revier.ProjectView{Project: revier.Project{Name: "rs-cli", Remote: &revier.Link{Host: "buildbox", Project: "dt-assist-cli"}}}
	busy := revier.ProjectView{Project: revier.Project{Name: "busy"}, Agents: []revier.AgentView{
		{Panel: "1", State: revier.AgentState{Activity: "Dtop authentication options"}},
		{Panel: "2", State: revier.AgentState{Name: "dynatui-94"}},
	}}
	for _, c := range []struct {
		view revier.ProjectView
		word string
		want bool
	}{
		{tm, "task-manager", true},
		{tm, "TASKMGR", true},
		{tm, "tm", true},
		{tm, "git", false},
		{link, "assist", true},
		{busy, "dtop", true},
		{busy, "dynatui-94", true},
		{busy, "DYNATUI-94", true},
		{busy, "tui-9", false},
	} {
		if got := core.Matches(c.view, c.word); got != c.want {
			t.Errorf("Matches(%s, %q) = %v, want %v", c.view.Project.Name, c.word, got, c.want)
		}
	}
}
