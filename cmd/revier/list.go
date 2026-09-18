package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"text/tabwriter"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

func cmdList(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the view as JSON")
	conversations := fs.Bool("conversations", false, "with --json, name the conversation each agent holds")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	// Named, the list is those projects alone. This is how a revier on
	// another machine is asked about the projects that live there
	// (decisions.md D40), and a name it does not know is an error, so a
	// missing project file there is reported, not shown as nothing.
	projects := a.projects
	if len(pos) > 0 {
		projects = nil
		for _, name := range pos {
			p, ok := a.project(revier.ProjectName(name))
			if !ok {
				return fmt.Errorf("no project named %q", name)
			}
			projects = append(projects, p)
		}
	}

	// What the survey can judge: a ref written after this, by another
	// process, is to a window the listing may have missed. A state that
	// cannot be read is nil, and a nil state lets nothing be pruned.
	before, err := state.Load(a.stateRoot)
	if err != nil {
		slog.Warn("state load, nothing pruned", "err", err)
	}
	report, err := a.core.Survey(ctx, projects, a.state.Bound, a.state.Attached)
	if err != nil {
		return err
	}
	views := report.Views

	// Drop attachments and bindings whose windows are gone, so state does not
	// accumulate refs to closed windows forever. The prune is made again on
	// the state as it is on disk: another process may have written it since.
	if a.state.Prune(report.Hosts, report.Instances, before) {
		a.update(func(s *state.State) { s.Prune(report.Hosts, report.Instances, before) })
	}

	if *asJSON {
		// What a save on another machine records for its link to a project
		// here (decisions.md D84).
		if *conversations {
			a.core.NameConversations(ctx, report)
		}
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(views)
	}

	if len(views) == 0 {
		root, _ := configRootForMessage()
		_, _ = fmt.Fprintf(a.out, "no projects. add one under %s/projects/<name>.toml\n", root)
		return nil
	}

	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROJECT\tSTATE\tAGENT\tTARGETS")
	for _, v := range views {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			v.Project.Label(), runState(v), agentSummary(v), targetSummary(v))
	}
	return w.Flush()
}

func runState(v revier.ProjectView) string {
	switch {
	case v.Invalid != "":
		return "invalid"
	case v.Unreachable != "":
		return "unreachable"
	case v.Running:
		return "running"
	}
	return "-"
}

// agentSummary shows the worst state across the project's agents, because the
// list exists to answer "which one needs me" at a glance.
func agentSummary(v revier.ProjectView) string {
	worst, ok := core.Worst(v.Agents)
	if !ok {
		return "-"
	}
	if worst.Activity == "" {
		return worst.Status.String()
	}
	return worst.Status.String() + ": " + worst.Activity
}

func targetSummary(v revier.ProjectView) string {
	out := ""
	for _, t := range v.Targets {
		mark := " "
		switch {
		case !t.Available && t.Reason != "":
			mark = "!" // its own configuration refused it
		case !t.Available:
			mark = "x" // no host on this machine can realize it
		case !t.Ref.IsZero():
			mark = "*" // running
		}
		name := string(t.Name)
		if t.Attached {
			name = "(" + t.Ref.Title + ")" // bound at runtime, so it has no name
		}
		if out != "" {
			out += " "
		}
		out += mark + name
	}
	return out
}
