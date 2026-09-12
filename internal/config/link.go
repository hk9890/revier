package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// linkTOML is a new link: the [remote] table alone, since everything else
// is derived (decisions.md D41). The name on the host is written only when
// it differs, so the file says no more than it has to.
func linkTOML(name revier.ProjectName, host string, project revier.ProjectName) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: written by `revier link`.\n", name)
	b.WriteString("[remote]\n")
	fmt.Fprintf(&b, "host = %s\n", quote(host))
	if project != "" && project != name {
		fmt.Fprintf(&b, "project = %s\n", quote(string(project)))
	}
	return b.String()
}

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
