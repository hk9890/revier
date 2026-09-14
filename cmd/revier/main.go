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
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/hk9890/revier/internal/build"
	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

const usage = `revier - a project-grouped control surface for running agents

usage:
  revier                        the TUI: every project, its agent state, its targets
  revier list [--json] [name..] the same, printed once, or for the named projects
  revier open [name] [--attach] run-or-raise a project's workspace; an unknown name
                                becomes a new project for this directory, and a
                                missing directory is cloned from git_url; --attach
                                ends with this terminal on it (tmux)
  revier new [name]             write a project file for this directory
  revier link [host [project]]  the ssh hosts; a host's projects; or a link to one,
                                written here under --name or the project's own
  revier go <target> [-p name]  run-or-raise a target; pressing it again returns home
  revier run <action> [-p name] run a configured action in the project
  revier attach [-p name]       bind the focused window to a project
  revier status                 which project this directory resolves to
  revier keys status [--json]   the desktop chords revier wants, and who holds them
  revier agent wait <agent> --until <status> [--timeout s]
                                block until an agent reaches a status
  revier agent prompt <agent> <text>
                                type one line into an agent and submit it
  revier agent new [-p name | --panel id] [--resume id] [--dir path]
                                add an agent tab, with its shell, to an open
                                workspace (kitty)
  revier session save [--name label]
                                record the projects that are open now
  revier session restore [id|name] [--dry-run]
                                open what a saved session recorded; the newest
                                without an argument
  revier session list [--json]  the saved sessions, newest first
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
	start := time.Now()
	args := os.Args[1:]
	openLog(args)
	err := run(args)
	if err == nil {
		logging.Op("command", start, nil, "args", args, "exit", 0)
		return
	}
	status, say := outcome(err)
	logging.Op("command", start, err, "args", args, "exit", status)
	if say {
		fmt.Fprintln(os.Stderr, "revier:", err)
	}
	os.Exit(status)
}

// openLog starts the day's log for this process. A log that cannot be opened
// costs the record, not the command.
//
// `list` records only what went wrong. A surface with a linked project runs
// `revier list` on that host every refresh (decisions.md D40), one process a
// second, so its successes would bury the host's own log.
func openLog(args []string) {
	cmd := "tui"
	if len(args) > 0 {
		cmd = args[0]
	}
	level := slog.LevelInfo
	if cmd == "list" {
		level = slog.LevelWarn
	}
	root, err := state.Root()
	if err == nil {
		err = logging.Setup(root, cmd, level)
	}
	if err != nil {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		fmt.Fprintf(os.Stderr, "revier: warning: no log: %v\n", err)
	}
}

// outcome is the exit status a failed command ends with, and whether its
// message is still to be printed.
func outcome(err error) (status int, say bool) {
	var exit *exec.ExitError
	switch {
	// An action's own exit status passes through, so whatever bound the key
	// sees the failure the command reported and not a generic one. The action
	// has said why on the terminal it was given. A tool a host drives fails
	// with a status too, and that one is revier's failure: it is printed.
	case errors.Is(err, errActionFailed) && errors.As(err, &exit):
		return exit.ExitCode(), false
	// A window that belongs to no project is a normal outcome with its own
	// status, so a desktop binding can offer the picker instead.
	case errors.Is(err, errNoProject):
		return exitNoProject, true
	// The keys command has already said, key by key, what did not happen. A
	// line here would repeat the summary it just printed.
	case errors.Is(err, errKeysIncomplete):
		return exitKeysIncomplete, false
	case errors.Is(err, errWaitTimeout):
		return exitTimeout, true
	// `revier each` has already named every project it failed in.
	case errors.Is(err, errEachFailed):
		return exitEachFailed, false
	}
	return 1, true
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
	case "agent":
		// Its own app: how long an agent command may take is one of its
		// flags, and the hosts are probed and listed inside that bound.
		return cmdAgent(args)
	}

	// A keypress command gets long enough for a detached launch's wait
	// (bindWait) on top of the host calls around it.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
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
	case "link":
		return cmdLink(ctx, a, args)
	case "status":
		return cmdStatus(ctx, a, args)
	case "session":
		return cmdSession(ctx, a, args)
	case "keys":
		return cmdKeys(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// commandTimeout bounds a command that has no bound of its own.
const commandTimeout = bindWait + 30*time.Second

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
	m := tui.New(a.core, a.projects, a.stateRoot, a.cfg, time.Second, th, start).
		WithRuntimes(append(slices.Clone(defaultRuntimeOrder), hostNone), func(ctx context.Context, want []string) (revier.Runtime, error) {
			return selectRuntime(ctx, want, runtimeAdapters())
		})
	// All motion reports the pointer with no button held, which the hover
	// needs, as well as the wheel and clicks. It takes plain drag-to-select
	// from the terminal; shift-drag still selects in kitty and most others.
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func cmdList(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the view as JSON")
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
			v.Project.Label(), runState(v), agentSummary(v), targetSummary(v))
	}
	return w.Flush()
}

func runState(v revier.ProjectView) string {
	switch {
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

func cmdOpen(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	attach := fs.Bool("attach", false, "end with this terminal on the workspace")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: revier open [name] [--attach]")
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
	// A remote project's checkout is its host's to clone: the pane opened
	// here runs `revier open` there, and that one clones (decisions.md D40).
	if p.Remote == nil {
		cloned, err := checkout.Ensure(p.Project, os.Stderr)
		if err != nil {
			return err
		}
		if cloned {
			// The clone ran without a deadline. The host calls still need
			// one, and the one set at startup may have been spent waiting
			// for git.
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()
		}
	}
	ref, err := a.goTarget(ctx, p, home.Name)
	if err != nil {
		return err
	}
	if *attach {
		return a.attach(ref)
	}
	fmt.Printf("%s: %s\n", p.Name, describe(ref))
	return nil
}

// attach ends the command with this terminal on the instance: the process
// becomes the runtime's attach, so the pane that ran `revier open --attach`
// is the workspace from here on. It is what the ssh pane of a remote
// project runs on the host (decisions.md D40). Only a runtime that can
// attach a terminal offers it; a runtime whose instances are windows of
// their own has been raised already, and there is nothing to become.
func (a *app) attach(ref revier.TargetRef) error {
	att, ok := a.core.Runtime.(revier.Attacher)
	if !ok {
		name := "none"
		if a.core.Runtime != nil {
			name = a.core.Runtime.Name()
		}
		return fmt.Errorf("--attach: the %s runtime cannot put a terminal on a workspace; it takes tmux", name)
	}
	if ref.IsZero() {
		return errors.New("--attach: the workspace has not come up yet, so there is nothing to attach to")
	}
	argv, err := att.AttachCommand(ref)
	if err != nil {
		return err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	// The exec replaces the process, so the command's own line is never
	// written: this one stands for it.
	slog.Info("attach", "ref", ref, "argv", argv)
	return syscall.Exec(path, argv, os.Environ())
}

func cmdGo(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("go", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
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
	if len(pos) != 1 {
		return fmt.Errorf("usage: revier run <action> [-p project]")
	}
	p, err := a.resolveProject(ctx, *project)
	if err != nil {
		return err
	}
	argv, err := a.actionArgv(p, pos[0])
	if err != nil {
		return err
	}
	a.launchedAction(p.Name)
	return runAction(p, argv)
}

// actionArgv is what runs for the named action: the action rendered against
// the project, or, for a project on another machine, the ssh that runs the
// action there (decisions.md D40).
func (a *app) actionArgv(p core.Project, name string) ([]string, error) {
	r, err := a.core.RemoteOf(p)
	if err != nil {
		return nil, err
	}
	if r != nil {
		return r.RunCommand(p.Remote.Project, name), nil
	}
	return a.action(p, name)
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

// errActionFailed marks an action's own failure, whose exit status revier
// passes on as its own.
var errActionFailed = errors.New("the action failed")

// runAction executes an argv in the project directory with the terminal
// attached, and returns the command's own error so its exit status survives.
// No shell: the argv is a list, so there is nothing to quote and nothing to
// inject into. No context either: the command's 30s deadline is for host
// calls, and an action - an editor, a long pull - runs as long as it runs.
func runAction(p core.Project, argv []string) error {
	c := exec.Command(argv[0], argv[1:]...)
	if p.Remote == nil {
		c.Dir = p.Path // a remote project's path is on its host, where the action runs
	}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	start := time.Now()
	err := c.Run()
	logging.Op("action", start, err, "project", p.Name, "argv", argv)
	if err != nil {
		return fmt.Errorf("%w: %w", errActionFailed, err)
	}
	return nil
}

func cmdAttach(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	project := projectFlag(fs)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier attach [-p project]")
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
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("usage: revier status [-p project]")
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
