// Command revier is the CLI, and with no arguments the TUI.
//
// Every command here is one process per invocation: a keybinding spawns it,
// does one thing, and exits. There is no daemon, which is why the commands
// avoid work they do not need - `go` never surveys every project, only the one
// it acts on. The TUI is the one long-lived process, and it refreshes through
// the same Survey `list` prints once.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/hk9890/revier/internal/build"
	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

const usage = `revier - a project-grouped control surface for running agents

usage:
  revier                        the TUI: every project, its agent state, its targets
  revier list [--json]          the same, printed once
  revier open [name]            run-or-raise a project's workspace; an unknown name
                                becomes a new project for this directory, and a
                                missing directory is cloned from git_url
  revier new [name]             write a project file for this directory
  revier go <target> [-p name]  run-or-raise a target; pressing it again returns home
  revier run <action> [-p name] run a configured action in the project
  revier attach [-p name]       bind the focused window to a project
  revier status                 which project this directory resolves to
  revier keys status [--json]   the desktop chords revier wants, and who holds them
  revier agent wait <agent> --until <status> [--timeout s]
                                block until an agent reaches a status
  revier agent prompt <agent> <text>
                                type one line into an agent and submit it
  revier each -- <cmd>          run one command in every project's directory
  revier each log [run]         past runs of it, or one run's results
  revier version

flags:
  -p, --project <name>          act on this project instead of the resolved one
`

// exitNoProject is returned when no project could be resolved. It is a
// normal outcome, not a failure: contrib/gnome/revier-go turns it into the
// picker.
const exitNoProject = 3

// exitKeysIncomplete is returned when `revier keys install` ran and the
// desktop is not fully what was asked for. Every reason is already printed
// against the key it belongs to, so this status carries no message of its own.
const exitKeysIncomplete = 4

// exitTimeout is returned when `revier agent wait` gave up. It is the status
// the shell tool's wait used, so scripts written against it keep their branch.
const exitTimeout = 2

// exitEachFailed is returned when `revier each` ran and the command failed in
// at least one project. It is not 1, so a script can tell a failed project from
// a revier that could not run at all.
const exitEachFailed = 5

func main() {
	if err := run(os.Args[1:]); err != nil {
		// An action's own exit status passes through, so whatever bound the
		// key sees the failure the command reported and not a generic one.
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		// A window that belongs to no project is a normal outcome with its
		// own status, so a desktop binding can offer the picker instead.
		if errors.Is(err, errNoProject) {
			fmt.Fprintln(os.Stderr, "revier:", err)
			os.Exit(exitNoProject)
		}
		// The keys command has already said, key by key, what did not happen.
		// A line here would repeat the summary it just printed.
		if errors.Is(err, errKeysIncomplete) {
			os.Exit(exitKeysIncomplete)
		}
		if errors.Is(err, errWaitTimeout) {
			fmt.Fprintln(os.Stderr, "revier:", err)
			os.Exit(exitTimeout)
		}
		// `revier each` has already named every project it failed in.
		if errors.Is(err, errEachFailed) {
			os.Exit(exitEachFailed)
		}
		fmt.Fprintln(os.Stderr, "revier:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
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
	case "new":
		return cmdNew(args)
	case "each":
		// No app: a run in every project needs the project list and the state
		// root, and no host. Probing the desktop would be work, and a way to
		// fail, that has nothing to do with running a command in directories.
		// No deadline either, for the reason runAction has none.
		return cmdEach(os.Stdout, args)
	}

	ctx, cancel := commandContext(cmd)
	defer cancel()

	a, err := newApp(ctx)
	if err != nil {
		return err
	}

	switch cmd {
	case "":
		return cmdTUI(a)
	case "list":
		return cmdList(ctx, a, args)
	case "open":
		return cmdOpen(ctx, a, args)
	case "go":
		return cmdGo(ctx, a, args)
	case "run":
		return cmdRun(ctx, a, args)
	case "attach":
		return cmdAttach(ctx, a, args)
	case "status":
		return cmdStatus(ctx, a, args)
	case "keys":
		return cmdKeys(ctx, a, args)
	case "agent":
		return cmdAgent(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// commandContext bounds a command. A keypress command gets long enough for a
// detached launch's wait (bindWait) on top of the host calls around it. An
// agent command waits as long as its caller says, which by default is for
// good: `revier agent wait` on a long turn is the point of it.
func commandContext(cmd string) (context.Context, context.CancelFunc) {
	if cmd == "agent" {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), bindWait+30*time.Second)
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

// cmdTUI runs the surface. Without a terminal - `revier | grep` - it prints
// the table instead, so a script sees what it always saw.
func cmdTUI(a *app) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return cmdList(context.Background(), a, nil)
	}
	th, err := a.cfg.Theme()
	if err != nil {
		return err
	}
	// A project the working directory does not resolve to is not an error
	// here: the TUI opens on the first row instead.
	start := revier.ProjectName("")
	if p, err := a.resolveProject(context.Background(), ""); err == nil {
		start = p.Name
	}
	m := tui.New(a.core, a.projects, a.stateRoot, a.cfg.Actions, time.Second, th, start)
	// Cell motion reports the wheel and clicks, and takes plain drag-to-select
	// from the terminal; shift-drag still selects in kitty and most others.
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func cmdList(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the view as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}

	report, err := a.core.Survey(ctx, a.projects, a.state.Bound)
	if err != nil {
		return err
	}
	views := report.Views

	// Drop attachments whose windows are gone, so state does not accumulate
	// refs to closed windows forever. An attachment is always a window-host
	// ref, so the window listing is the live set.
	if a.core.Window != nil {
		live := map[string]bool{}
		for _, inst := range report.Instances {
			live[state.Key(inst.Ref)] = true
		}
		if a.state.Prune(live) {
			a.commit(a.state.Current, nil)
		}
	}

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
	_, _ = fmt.Fprintln(w, "PROJECT\tSTATE\tAGENT\tTARGETS")
	for _, v := range views {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
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
	// A name revier does not know, typed in a directory, is a project being
	// started: the file is written and the workspace opened in one step, as
	// `os open` does. Without a name there is nothing to call it, and the
	// usual resolution applies.
	if _, known := a.project(revier.ProjectName(name)); name != "" && !known {
		p, err := createProject(a.cfgRoot, a.projects, revier.ProjectName(name))
		if err != nil {
			return err
		}
		a.projects = append(a.projects, p)
	}
	p, err := a.resolveProject(ctx, name)
	if err != nil {
		return err
	}
	home, ok := p.Home()
	if !ok {
		return fmt.Errorf("project %q has no home target", p.Name)
	}
	cloned, err := checkout.Ensure(p.Project, os.Stderr)
	if err != nil {
		return err
	}
	if cloned {
		// The clone ran without a deadline. The host calls still need one,
		// and the one set at startup may have been spent waiting for git.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), bindWait+30*time.Second)
		defer cancel()
	}
	ref, err := a.goTarget(ctx, p, home.Name)
	if err != nil {
		return err
	}
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
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	ref, err := a.goTarget(ctx, p, revier.TargetName(pos[0]))
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", p.Name, describe(ref))
	return nil
}

func cmdRun(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return fmt.Errorf("usage: revier run <action> [-p project]")
	}
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	argv, err := a.action(p, pos[0])
	if err != nil {
		return err
	}
	a.launchedAction(p.Name)
	return runAction(p, argv)
}

// action renders the named action's argv against the project. An unknown name
// is an error naming it: a key bound to nothing must say so, not do nothing.
func (a *app) action(p core.Project, name string) ([]string, error) {
	for _, act := range a.cfg.Actions {
		if act.Name != name {
			continue
		}
		argv, err := core.RenderArgv(p.Project, act.Run)
		if err != nil {
			return nil, fmt.Errorf("action %q: %w", name, err)
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("action %q has an empty run argv", name)
		}
		return argv, nil
	}
	return nil, fmt.Errorf("no action named %q", name)
}

// runAction executes an argv in the project directory with the terminal
// attached, and returns the command's own error so its exit status survives.
// No shell: the argv is a list, so there is nothing to quote and nothing to
// inject into. No context either: the command's 30s deadline is for host
// calls, and an action - an editor, a long pull - runs as long as it runs.
func runAction(p core.Project, argv []string) error {
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = p.Path
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
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
	p, err := a.resolveProject(ctx, *project)
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
	a.commit(p.Name, func(s *state.State) { s.Attach(p.Name, ref) })
	fmt.Printf("%s: attached %s\n", p.Name, describe(ref))
	return nil
}

func cmdStatus(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	project := projectFlag(fs)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	p, err := a.resolveProject(ctx, *project)
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
		return "launching"
	}
	if ref.Title == "" {
		return ref.Host + "/" + ref.ID
	}
	return fmt.Sprintf("%s (%s/%s)", ref.Title, ref.Host, ref.ID)
}
