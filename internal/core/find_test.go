package core_test

import (
	"slices"
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

// A name that is no project's suggests the nearest names: a few edits away,
// or a project the name finds, and none when nothing is near.
func TestClosestNamesTheProjectsANameProbablyMeant(t *testing.T) {
	var projects []core.Project
	for _, name := range []string{"revier", "task-manager", "task-manager-ui", "dynatui", "copilot-skills", "chromectl"} {
		projects = append(projects, core.Project{Project: revier.Project{Name: revier.ProjectName(name)}})
	}
	for name, want := range map[string][]revier.ProjectName{
		"taskmgr-ui":           {"task-manager-ui"},
		"revir":                {"revier"},
		"skills":               {"copilot-skills"},
		"zzzz-no-such-project": nil,
		"nosuchproject":        nil,
	} {
		if got := core.Closest(projects, name); !slices.Equal(got, want) {
			t.Errorf("Closest(%q) = %q, want %q", name, got, want)
		}
	}
}
