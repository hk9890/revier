package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

func loadStatuses(t *testing.T, cfg string) *config.Config {
	t.Helper()
	c, _, err := config.Load(writeConfig(t, cfg))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func TestConfigStatusesKeepsEveryGoodTable(t *testing.T) {
	c := loadStatuses(t, `
[[status]]
at = "rule-left"
run = ["git-summary"]

[[status]]
at = "rule-right"
run = ["clock", "--short"]
every = "5s"

[[status]]
at = "project-line"
run = ["ci"]
every = "2m"

[[status]]
at = "project"
title = "Build"
run = ["build-info"]
`)
	if len(c.Problems) != 0 {
		t.Fatalf("Problems = %v, want none", c.Problems)
	}
	want := []core.Status{
		{At: core.StatusRuleLeft, Run: []string{"git-summary"}},
		{At: core.StatusRuleRight, Run: []string{"clock", "--short"}, Every: 5 * time.Second},
		{At: core.StatusProjectLine, Run: []string{"ci"}, Every: 2 * time.Minute},
		{At: core.StatusProject, Title: "Build", Run: []string{"build-info"}},
	}
	if got := c.Statuses(); !reflect.DeepEqual(got, want) {
		t.Errorf("Statuses = %+v, want %+v", got, want)
	}
}

func TestConfigStatusesRefusesTheBrokenTableAlone(t *testing.T) {
	good := `
[[status]]
at = "rule-left"
run = ["git-summary"]
`
	for name, tc := range map[string]struct{ table, reason string }{
		"no at":        {`run = ["x"]`, "no at"},
		"unknown at":   {"at = \"sidebar\"\nrun = [\"x\"]", "at is one of"},
		"no run":       {`at = "project-line"`, "runs nothing"},
		"empty run":    {"at = \"project-line\"\nrun = []", "runs nothing"},
		"bad every":    {"at = \"project-line\"\nrun = [\"x\"]\nevery = \"soon\"", "every"},
		"every short":  {"at = \"project-line\"\nrun = [\"x\"]\nevery = \"4s\"", "the least is 5s"},
		"title off":    {"at = \"project-line\"\nrun = [\"x\"]\ntitle = \"CI\"", "title"},
		"title rule":   {"at = \"rule-right\"\nrun = [\"x\"]\ntitle = \"CI\"", "title"},
		"second there": {"at = \"rule-left\"\nrun = [\"y\"]", "two tables"},
	} {
		t.Run(name, func(t *testing.T) {
			c := loadStatuses(t, good+"\n[[status]]\n"+tc.table+"\n")
			if len(c.Problems) != 1 || !strings.Contains(c.Problems[0].Error(), tc.reason) {
				t.Fatalf("Problems = %v, want one saying %q", c.Problems, tc.reason)
			}
			want := []core.Status{{At: core.StatusRuleLeft, Run: []string{"git-summary"}}}
			if got := c.Statuses(); !reflect.DeepEqual(got, want) {
				t.Errorf("Statuses = %+v, want the good table alone %+v", got, want)
			}
		})
	}
}

func TestConfigStatusEveryUnsetIsZero(t *testing.T) {
	c := loadStatuses(t, "[[status]]\nat = \"project\"\nrun = [\"x\"]\n")
	if got := c.Statuses(); len(got) != 1 || got[0].Every != 0 {
		t.Errorf("Statuses = %+v, want one with Every 0", got)
	}
}

func TestConfigWithNoStatusHasNone(t *testing.T) {
	c := loadStatuses(t, "")
	if got := c.Statuses(); len(got) != 0 {
		t.Errorf("Statuses = %+v, want none", got)
	}
}

func loadStatusProject(t *testing.T, body string) core.Project {
	t.Helper()
	_, projects, err := config.Load(projectsRoot(t, "", map[string]string{"revier.toml": body}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	return projects[0]
}

func TestProjectStatusKeepsTheGoodTablesAndRefusesTheRest(t *testing.T) {
	p := loadStatusProject(t, valid+`
[[status]]
at = "project-line"
every = "10s"

[[status]]
at = "project"
title = "Deploy"
run = ["deploy-state"]

[[status]]
at = "rule-left"
run = ["x"]

[[status]]
at = "project"
run = ["again"]
`)
	want := []core.Status{
		{At: core.StatusProjectLine, Every: 10 * time.Second},
		{At: core.StatusProject, Title: "Deploy", Run: []string{"deploy-state"}},
	}
	if !reflect.DeepEqual(p.Status, want) {
		t.Errorf("Status = %+v, want %+v", p.Status, want)
	}
	// config.toml has no project-line status, so the table that changes one
	// runs nothing once merged, and Load says so beside the rules' refusals.
	if len(p.StatusRefused) != 3 ||
		!strings.Contains(p.StatusRefused[0].Error(), `"rule-left"`) ||
		!strings.Contains(p.StatusRefused[1].Error(), "two tables") ||
		!strings.Contains(p.StatusRefused[2].Error(), `"project-line": runs nothing`) {
		t.Errorf("StatusRefused = %v, want the rule-left table, the second project table and the project-line table that runs nothing", p.StatusRefused)
	}
	if p.Invalid != nil {
		t.Errorf("Invalid = %v, want nil: a refused status refuses the table alone", p.Invalid)
	}
	if got := config.Problems(p); !reflect.DeepEqual(got, p.StatusRefused) {
		t.Errorf("Problems = %v, want the status refusals %v", got, p.StatusRefused)
	}
}

// A status table TOML cannot decode is refused with the line it stands on.
// An edit above it moves that line and breaks nothing new, so it is written
// (decisions.md D85).
func TestProjectEditAboveABrokenStatusIsWritten(t *testing.T) {
	file, shared := projectRoot(t, overriding+"\n[[status]]\nat = \"project\"\nrun = [\"x\"]\nevery = 10\n")
	if _, err := config.SetProjectValue(file, "git_url", "git@github.com:me/demo.git", shared); err != nil {
		t.Errorf("SetProjectValue over a broken status table: %v", err)
	}
}

func TestProjectStatusRefusesRuleRight(t *testing.T) {
	p := loadStatusProject(t, valid+"\n[[status]]\nat = \"rule-right\"\nrun = [\"x\"]\n")
	if len(p.Status) != 0 || len(p.StatusRefused) != 1 {
		t.Errorf("Status = %+v, StatusRefused = %v; want the table refused", p.Status, p.StatusRefused)
	}
}

func TestProjectStatusOfALinkIsRead(t *testing.T) {
	p := loadStatusProject(t, link+"\n[[status]]\nat = \"project\"\nrun = [\"remote-info\"]\n")
	want := []core.Status{{At: core.StatusProject, Run: []string{"remote-info"}}}
	if !reflect.DeepEqual(p.Status, want) {
		t.Errorf("Status = %+v, want %+v", p.Status, want)
	}
}
