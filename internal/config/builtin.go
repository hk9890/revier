package config

import (
	"fmt"
	"slices"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// Assistant is the name of the project revier builds in and no file declares:
// the coding agent that writes revier's configuration.
const Assistant revier.ProjectName = "revier-config-assistant"

// assistSession is the runtime name of the assistant's workspace. It has no
// "session:" in front, as a workspace the template writes has, so no project
// file's workspace takes it.
const assistSession = string(Assistant)

// Builtin is the assistant as a project: a workspace in dir, whose one tab
// runs command, `revier assist --exec`. That command builds the brief when
// the agent starts, so the brief is never rendered as a template, and it
// replaces itself with the agent, so the probe finds the agent in the panel.
//
// It takes no shared target: config.toml cannot refuse it, and a file that
// does not load cannot take it with it. It goes through the rules a project
// file goes through, so the surface reads a fault in it as it reads one in a
// file.
func Builtin(dir string, command []string) core.Project {
	p := revier.Project{Name: Assistant, Path: dir, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name:  assistSession,
			Match: revier.Match{Title: "^" + assistSession + "$"},
			Tabs:  []revier.TargetName{agentTab},
		}},
		{Name: agentTab, Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
			{Kind: revier.PanelAgent, Command: command},
		}}},
	}}
	prepared := core.PrepareProject(p)
	if err := validateProject(p); err != nil {
		prepared.Invalid = fmt.Errorf("built-in project %s: %w", Assistant, err)
	}
	for i, err := range validateTargets(p) {
		if err != nil {
			prepared.Refuse(i, err)
		}
	}
	return prepared
}

// IsBuiltin reports p the assistant revier built, and not a file's project
// that took its name. It takes no name from a file: a file of its name
// replaces it.
func IsBuiltin(p core.Project) bool {
	return p.Name == Assistant && p.File == ""
}

// Linkable is what a host lists that a link may name: all but its built-in
// assistant, which writes that host's configuration and not this one's. The
// host's list says no more than a name, so a project file there of the same
// name is left out with it.
func Linkable(views []revier.ProjectView) []revier.ProjectView {
	return slices.DeleteFunc(slices.Clone(views), func(v revier.ProjectView) bool { return v.Project.Name == Assistant })
}

// WithBuiltin is projects with the assistant after them. A project file of
// the same name wins: the user has said what assist is on this machine.
func WithBuiltin(projects []core.Project, builtin core.Project) []core.Project {
	if slices.ContainsFunc(projects, func(p core.Project) bool { return p.Name == builtin.Name }) {
		return projects
	}
	return append(slices.Clip(projects), builtin)
}
