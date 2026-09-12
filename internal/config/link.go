package config

import (
	"regexp"

	"github.com/hk9890/revier/pkg/revier"
)

// link fills in what a link file leaves to be derived (decisions.md D41).
// The project's name on the host is the link's own name unless the file
// says otherwise. The home target, unless the file declares one, is the
// pane that reaches the workspace: an ssh onto the host that runs `revier
// open` there and ends attached to it. A link may declare further targets,
// which are ordinary local ones - an editor over ssh, a page.
func link(p *revier.Project) {
	if p.Remote.Project == "" {
		p.Remote.Project = p.Name
	}
	if _, ok := p.Home(); ok {
		return
	}
	title := "session:" + string(p.Name)
	p.Targets = append([]revier.Target{{
		Name: "home", Home: true,
		Runtime: &revier.Realization{
			Name:   title,
			Launch: []string{"ssh", "-t", p.Remote.Host, "revier", "open", string(p.Remote.Project), "--attach"},
			Match:  revier.Match{Title: "^" + regexp.QuoteMeta(title) + "$"},
		},
	}}, p.Targets...)
}
