package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// linkTOML is a new link: where the project is, and what the host says about
// it that a target here renders (decisions.md D41, D83). The path and the
// repository are the host's answer when the link is written, kept as written
// because neither names anything on this machine; everything else is derived.
// The name on the host is written only when it differs, so the file says no
// more than it has to.
func linkTOML(name revier.ProjectName, host string, on revier.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: written by `revier link`.\n", name)
	if on.Path != "" {
		fmt.Fprintf(&b, "path = %s\n", quote(on.Path))
	}
	if on.GitURL != "" {
		fmt.Fprintf(&b, "git_url = %s\n", quote(on.GitURL))
	}
	b.WriteString("[remote]\n")
	fmt.Fprintf(&b, "host = %s\n", quote(host))
	if on.Name != "" && on.Name != name {
		fmt.Fprintf(&b, "project = %s\n", quote(string(on.Name)))
	}
	return b.String()
}

// link fills in what a link file leaves to be derived (decisions.md D41).
// The project's name on the host is the link's own name unless the file
// says otherwise. The home target is the workspace as it is laid out here: an
// agent panel and a shell panel, each reaching the host to run `revier agent
// exec` or `revier shell exec` there (decisions.md D84). What argv reaches
// the host is the remote port's to say, and the core asks it when the target
// is resolved: a panel here carries its kind and no command. Which harness
// and which directory is the host's project file's to say. A link may declare
// further targets, which run here and reach the host themselves - an editor
// over ssh, a page (decisions.md D82).
//
// A home target the link already has - its own, or the remote part of the
// shared one - keeps every field it sets, and the derived pane fills the
// rest of its runtime realization. That is how a shared home contributes a
// placement without repeating the ssh launch it knows nothing about. A home
// that says it is a window is left alone: the link has named the tool that
// reaches the workspace, and a second realization beside it would decide the
// question the other way on a machine with no window host.
func link(p *revier.Project) {
	if p.Remote.Project == "" {
		p.Remote.Project = p.Name
	}
	title := "session:" + string(p.Name)
	pane := revier.Realization{
		Name:   title,
		Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}},
		Match:  revier.Match{Title: "^" + regexp.QuoteMeta(title) + "$"},
	}
	for i, t := range p.Targets {
		if !t.Home {
			continue
		}
		if t.Runtime == nil {
			if t.Window == nil {
				p.Targets[i].Runtime = &pane
			}
			return
		}
		fillPane(p.Targets[i].Runtime, pane)
		return
	}
	p.Targets = append([]revier.Target{{
		Name: "home", Home: true, Runtime: &pane,
	}}, p.Targets...)
}

// fillPane writes the derived pane into the fields the link left empty. A
// realization that declares panels is already launched by them, and a launch
// beside them is refused, so the pane's is not one of the fields it left out.
func fillPane(r *revier.Realization, pane revier.Realization) {
	if r.Name == "" {
		r.Name = pane.Name
	}
	if len(r.Launch) == 0 && len(r.Panels) == 0 {
		r.Panels = pane.Panels
	}
	if r.Match.IsZero() {
		r.Match = pane.Match
	}
}
