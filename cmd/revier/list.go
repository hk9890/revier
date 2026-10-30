package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func cmdList(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the view as JSON")
	conversations := fs.Bool("conversations", false, "with --json, name the conversation each agent holds")
	match := fs.String("match", "", "only the projects this word finds")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *match != "" && len(pos) > 0 {
		return errors.New("usage: revier list --match <word> takes no project names")
	}
	// Named, the list is those projects alone. This is how a revier on
	// another machine is asked about the projects that live there
	// (decisions.md D40), and a name it does not know is an error, so a
	// missing project file there is reported, not shown as nothing. The
	// names it knows are listed all the same, for the person who asked about
	// several at once.
	projects := a.projects
	var unknown []string
	if len(pos) > 0 {
		projects = nil
		for _, name := range pos {
			p, ok := a.project(revier.ProjectName(name))
			if !ok {
				unknown = append(unknown, strconv.Quote(name))
				fmt.Fprintf(os.Stderr, "revier: no project named %q%s\n", name, didYouMean(a.projects, name))
				continue
			}
			projects = append(projects, p)
		}
		if len(projects) == 0 {
			return unknownProjects(unknown)
		}
	}

	report, err := a.core.Survey(ctx, projects)
	if err != nil {
		return err
	}
	if err := report.HostErr(); err != nil {
		fmt.Fprintf(os.Stderr, "revier: warning: %v\n", err)
	}

	// Drop attachments and bindings whose windows are gone, so state does not
	// accumulate refs to closed windows forever. A list is the first settle
	// of its process, so no window is new to it and it claims nothing.
	a.core.Settle(report, a.projects, time.Now())

	if *match != "" {
		report.Views = slices.DeleteFunc(report.Views, func(v revier.ProjectView) bool { return !core.Matches(v, *match) })
		if len(report.Views) == 0 {
			return fmt.Errorf("no project matches %q", *match)
		}
	}
	views := report.Views

	if *asJSON {
		// What a save on another machine records for its link to a project
		// here.
		if *conversations {
			a.core.NameConversations(ctx, report)
		}
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(views); err != nil {
			return err
		}
		return unknownProjects(unknown)
	}

	if err := a.table(views); err != nil {
		return err
	}
	// The built-in assistant is in no file and is no project of the user's,
	// so a list of it alone still says where to add one, under its row.
	if !slices.ContainsFunc(a.projects, func(p core.Project) bool { return p.File != "" }) {
		root, _ := configRootForMessage()
		_, _ = fmt.Fprintf(a.out, "no projects. add one under %s/projects/<name>.toml\n", root)
	}
	return unknownProjects(unknown)
}

// errNoProjectNamed means a list was asked for a name that is no project's.
// Each such name has had its line on stderr, so main prints none.
var errNoProjectNamed = errors.New("no project named")

// unknownProjects is what a list asked for the quoted names, which are no
// project's, ends with: nil for no name.
func unknownProjects(quoted []string) error {
	if len(quoted) == 0 {
		return nil
	}
	return fmt.Errorf("%w %s", errNoProjectNamed, strings.Join(quoted, ", "))
}

// didYouMean is what the line about a name that is no project's adds: the
// projects it probably meant, or nothing.
func didYouMean(projects []core.Project, name string) string {
	var names []string
	for _, n := range core.Closest(projects, name) {
		names = append(names, string(n))
	}
	if len(names) == 0 {
		return ""
	}
	return "; did you mean " + strings.Join(names, ", ") + "?"
}

// table is the list as a person at this machine reads it.
func (a *app) table(views []revier.ProjectView) error {
	if len(views) == 0 {
		return nil
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROJECT\tSTATE\tAGENT\tTARGETS")
	// The table is read by a person at this machine, and the JSON above by
	// the revier on another: the table says only what a panel here shows
	// (decisions.md D104), and the JSON says everything the survey found.
	for _, v := range a.core.Shown(views) {
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
	case v.Held():
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
		case t.Unknown != "":
			mark = "?" // its host could not list
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
