// Command revier is the CLI.
//
// The TUI is not built yet, so `revier` with no arguments lists projects. Every
// command here is one process per invocation: a keybinding spawns it, does one
// thing, and exits. There is no daemon, which is why the commands avoid work
// they do not need - `go` never surveys every project, only the one it acts on.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hk9890/revier/internal/build"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

const usage = `revier - a project-grouped control surface for running agents

usage:
  revier [list] [--json]        list projects, agent state, and targets
  revier open [name]            run-or-raise a project's workspace
  revier go <target> [-p name]  run-or-raise a target; pressing it again returns home
  revier attach [-p name]       bind the focused window to a project
  revier status                 which project this directory resolves to
  revier version

flags:
  -p, --project <name>          act on this project instead of the resolved one
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "revier:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "list"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "version", "--version":
		fmt.Printf("revier %s (%s, %s)\n", build.Version, build.Commit, build.Date)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a, err := newApp(ctx)
	if err != nil {
		return err
	}

	switch cmd {
	case "list":
		return cmdList(ctx, a, args)
	case "open":
		return cmdOpen(ctx, a, args)
	case "go":
		return cmdGo(ctx, a, args)
	case "attach":
		return cmdAttach(ctx, a, args)
	case "status":
		return cmdStatus(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// projectFlag registers -p/--project on a flag set.
func projectFlag(fs *flag.FlagSet) *string {
	var p string
	fs.StringVar(&p, "project", "", "act on this project")
	fs.StringVar(&p, "p", "", "act on this project (shorthand)")
	return &p
}

// parseArgs parses flags that appear anywhere, returning the positional
// arguments in order.
//
// flag.Parse stops at the first non-flag argument, so `revier go editor -p
// other` would leave -p unparsed and act on the resolved project instead of
// the requested one - the wrong project, silently. Every command here takes a
// positional before its flags, so parsing has to continue past them.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func cmdList(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the view as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	views, err := a.core.Survey(ctx, a.projects)
	if err != nil {
		return err
	}

	// Drop attachments whose windows are gone, so state does not accumulate
	// refs to closed windows forever.
	live := map[string]bool{}
	for _, v := range views {
		for _, t := range v.Targets {
			if !t.Ref.IsZero() {
				live[state.Key(t.Ref)] = true
			}
		}
	}
	a.state.Prune(live)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(views)
	}

	if len(views) == 0 {
		root, _ := configRootForMessage()
		fmt.Printf("no projects. add one under %s/projects/<name>.toml\n", root)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tSTATE\tAGENT\tTARGETS")
	for _, v := range views {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			v.Project.Name, runState(v), agentSummary(v), targetSummary(v))
	}
	return w.Flush()
}

func runState(v revier.ProjectView) string {
	if v.Running {
		return "running"
	}
	return "-"
}

// agentSummary shows the worst state across the project's agents, because the
// list exists to answer "which one needs me" at a glance.
func agentSummary(v revier.ProjectView) string {
	if len(v.Agents) == 0 {
		return "-"
	}
	worst := revier.StatusUnknown
	activity := ""
	for _, ag := range v.Agents {
		if ag.State.Status > worst {
			worst, activity = ag.State.Status, ag.State.Activity
		}
	}
	if activity == "" {
		return worst.String()
	}
	return worst.String() + ": " + activity
}

func targetSummary(v revier.ProjectView) string {
	out := ""
	for _, t := range v.Targets {
		mark := " "
		switch {
		case !t.Available:
			mark = "x" // no host on this machine can realize it
		case !t.Ref.IsZero():
			mark = "*" // running
		}
		if out != "" {
			out += " "
		}
		out += mark + string(t.Name)
	}
	return out
}

func cmdOpen(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	p, err := a.resolveProject(name)
	if err != nil {
		return err
	}
	home, ok := p.Home()
	if !ok {
		return fmt.Errorf("project %q has no home target", p.Name)
	}
	ref, err := a.core.Go(ctx, p, home.Name)
	if err != nil {
		return err
	}
	a.remember(p.Name)
	fmt.Printf("%s: %s\n", p.Name, describe(ref))
	return nil
}

func cmdGo(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("go", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return fmt.Errorf("usage: revier go <target> [-p project]")
	}
	p, err := a.resolveProject(*project)
	if err != nil {
		return err
	}
	ref, err := a.core.Go(ctx, p, revier.TargetName(pos[0]))
	if err != nil {
		return err
	}
	a.remember(p.Name)
	fmt.Printf("%s: %s\n", p.Name, describe(ref))
	return nil
}

func cmdAttach(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	project := projectFlag(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if a.core.Window == nil {
		return fmt.Errorf("attach needs a window host; none is available here")
	}
	p, err := a.resolveProject(*project)
	if err != nil {
		return err
	}
	ref, err := a.core.Window.Focused(ctx)
	if err != nil {
		return err
	}
	if ref.IsZero() {
		return fmt.Errorf("no window is focused")
	}
	a.state.Attach(p.Name, ref)
	a.remember(p.Name)
	fmt.Printf("%s: attached %s\n", p.Name, describe(ref))
	return nil
}

func cmdStatus(_ context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	project := projectFlag(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	p, err := a.resolveProject(*project)
	if err != nil {
		return err
	}
	fmt.Printf("project   %s\npath      %s\n", p.Name, p.Path)
	fmt.Printf("runtime   %s\nwindow    %s\n", hostName(a.core.Runtime), hostName(a.core.Window))
	if attached := a.state.Attached[p.Name]; len(attached) > 0 {
		fmt.Printf("attached  %d window(s)\n", len(attached))
	}
	return nil
}

func hostName(h revier.Host) string {
	if h == nil {
		return "-"
	}
	return h.Name()
}

func describe(ref revier.TargetRef) string {
	if ref.IsZero() {
		return "launched (no window yet)"
	}
	if ref.Title == "" {
		return ref.Host + "/" + ref.ID
	}
	return fmt.Sprintf("%s (%s/%s)", ref.Title, ref.Host, ref.ID)
}
