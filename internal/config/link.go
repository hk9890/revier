package config

import (
	"fmt"
	"regexp"
	"slices"
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

// agentTab is the name of the tab a link's workspace opens with, and of the
// tab `revier new` writes.
const agentTab revier.TargetName = "agent"

// link fills in what a link file leaves to be derived (decisions.md D41).
// The project's name on the host is the link's own name unless the file
// says otherwise. The home target is the workspace as it is laid out here: a
// container that lists one tab, and that tab, a target named agent inside it
// with an agent panel and a shell panel, each reaching the host to run
// `revier agent exec` or `revier shell exec` there (D84, D128).
// What argv reaches the host is the remote port's to say, and the core asks
// it where the tabs are read: a panel here carries its kind and no command.
// Which harness and which directory is the host's project file's to say. A
// link may declare further targets, which run here and reach the host
// themselves - an editor over ssh, a page (D82).
//
// A home target the link already has - its own, or the remote part of the
// shared one - keeps every field it sets, and the derived name and match
// fill the rest of its runtime realization. That is how a shared home
// contributes a placement without repeating the ssh launch it knows nothing
// about. A home with a launch is launched by it, and one with panels is
// refused at load, so neither gets the tab. A home that says it is a window is
// left alone: the link has named the tool that reaches the workspace, and a
// second realization beside it would decide the question the other way on a
// machine with no window host. A home with both realizations gets the name
// and the match and no tab, because a tab is refused inside a target that
// also has a window.
//
// A target named agent the link already has is the tab when it is inside the
// home: it keeps its launch or its panels, and gets the two panels when it
// has neither. Any other target of that name is left as it is, and the home
// that lists it is refused with the reason. A home that has the name itself
// gets no tab, and is refused for the name.
//
// A link has no list of tabs of its own (D128): one a runtime
// realization declares is dropped, with its active tab, before the home gets
// the derived one.
func link(p *revier.Project) {
	if p.Remote.Project == "" {
		p.Remote.Project = p.Name
	}
	dropTabs(p.Targets)
	i := slices.IndexFunc(p.Targets, func(t revier.Target) bool { return t.Home })
	if i < 0 {
		p.Targets = append([]revier.Target{{Name: "home", Home: true}}, p.Targets...)
		i = 0
	}
	home := &p.Targets[i]
	if home.Runtime == nil {
		if home.Window != nil {
			return
		}
		home.Runtime = &revier.Realization{}
	}
	r := home.Runtime
	title := "session:" + string(p.Name)
	if r.Name == "" {
		r.Name = title
	}
	if r.Match.IsZero() {
		r.Match = revier.Match{Title: "^" + regexp.QuoteMeta(title) + "$"}
	}
	if len(r.Launch) > 0 || len(r.Panels) > 0 || home.Window != nil || home.Name == agentTab {
		return
	}
	r.Tabs = []revier.TargetName{agentTab}
	panels := []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}
	if j := findTarget(p.Targets, agentTab); j >= 0 {
		if tab := p.Targets[j].Runtime; tab != nil && tab.Inside == home.Name && len(tab.Launch) == 0 && len(tab.Panels) == 0 {
			tab.Panels = panels
		}
		return
	}
	p.Targets = slices.Insert(p.Targets, i+1, revier.Target{
		Name: agentTab, Runtime: &revier.Realization{Inside: home.Name, Panels: panels},
	})
}

// dropTabs removes the list of tabs and the active tab from every runtime
// realization: the targets of a link, and the shared targets a link's are
// compared against when one is written.
func dropTabs(targets []revier.Target) {
	for _, t := range targets {
		if t.Runtime != nil {
			t.Runtime.Tabs, t.Runtime.Active = nil, ""
		}
	}
}
