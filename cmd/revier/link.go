package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/sshconfig"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdLink is the link dialog as a command (decisions.md D45): with nothing,
// the hosts the ssh configuration names; with a host, the projects the
// revier there has; with a host and a project, a link to it written here.
func cmdLink(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	name := fs.String("name", "", "the link's name here; the project's own name by default")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	switch len(pos) {
	case 0:
		path, err := sshconfig.Path()
		if err != nil {
			return err
		}
		hosts, err := sshconfig.Hosts(path)
		if err != nil {
			return err
		}
		if len(hosts) == 0 {
			fmt.Printf("no hosts in %s\n", path)
			return nil
		}
		for _, h := range hosts {
			fmt.Println(h)
		}
		return nil
	case 1:
		views, err := a.core.ProjectsOn(ctx, pos[0])
		if err != nil {
			return err
		}
		return printRemote(a, pos[0], views)
	case 2:
		p, err := a.link(ctx, pos[0], revier.ProjectName(pos[1]), revier.ProjectName(*name))
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", p.File)
		return nil
	}
	return fmt.Errorf("usage: revier link [host [project]] [--name name]")
}

// printRemote is the table of a host's projects, with the link here that
// already points at each, so a second link to one is not written unaware.
func printRemote(a *app, host string, views []revier.ProjectView) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROJECT\tPATH\tSTATE\tAGENT\tLINKED AS")
	for _, v := range views {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			v.Project.Name, v.Project.Path, runState(v), agentSummary(v), a.linkedAs(host, v.Project.Name))
	}
	return w.Flush()
}

// linkedAs is the name of the link here to a project on a host, or "-".
func (a *app) linkedAs(host string, project revier.ProjectName) string {
	for _, p := range a.projects {
		if p.Remote != nil && p.Remote.Host == host && p.Remote.Project == project {
			return string(p.Name)
		}
	}
	return "-"
}

// link writes a link to a project on a host, under name, or under the
// project's own name. The host is asked first, so a name it does not have
// is refused before a file is written; a name already taken here is refused
// as `revier new` refuses it.
func (a *app) link(ctx context.Context, host string, project, name revier.ProjectName) (core.Project, error) {
	if name == "" {
		name = project
	}
	if p, ok := a.project(name); ok {
		return core.Project{}, fmt.Errorf("project %q already exists: %s", name, p.File)
	}
	views, err := a.core.ProjectsOn(ctx, host)
	if err != nil {
		return core.Project{}, err
	}
	var on revier.Project
	found := false
	for _, v := range views {
		if v.Project.Name == project {
			on, found = v.Project, true
		}
	}
	if !found {
		return core.Project{}, fmt.Errorf("%s has no project named %q; `revier link %s` lists what it has", host, project, host)
	}
	p, err := config.CreateLink(a.cfgRoot, name, host, on)
	if err != nil {
		return core.Project{}, err
	}
	a.projects = append(a.projects, p)
	return p, nil
}
