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
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/hk9890/revier/internal/build"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
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
  revier go <target> [-p name] [--picker]
                                run-or-raise a target; pressing it again returns home;
                                --picker opens the popup when no project resolves
  revier popup                  the TUI in a kitty window of its own, or the one open
  revier run <action> [-p name] run a configured action in the project
  revier attach [-p name]       bind the focused window to a project
  revier status                 which project this directory resolves to
  revier doctor                 every project file that did not load whole, and
                                what to do about each problem
  revier keys status [--json]   the desktop chords revier wants, and who holds them
  revier agent wait <agent> --until <status> [--timeout s]
                                block until an agent reaches a status
  revier agent prompt <agent> <text>
                                type one line into an agent and submit it
  revier agent new [-p name | --panel id] [--resume id] [--dir path]
                                add an agent tab, with its shell, to an open
                                workspace; for a link, the tab's agent runs
                                on the host and --dir is dropped
  revier shell new [-p name | --panel id] [--dir path]
                                add a shell tab to an open workspace; for a
                                link, the tab's shell runs on the host
  revier session save [--name label]
                                record the projects that are open now
  revier session restore [id|name] [--dry-run]
                                open what a saved session recorded; the newest
                                without an argument
  revier session list [--json]  the saved sessions, newest first
  revier shutdown [project] [--agents | --targets] [--force] [--no-session-save] [--dry-run]
                                save the session when it changed, then close every
                                open target and agent, or one project's; --agents
                                closes only the agents, --targets only what holds
                                no agent; refuses while an agent is busy unless
                                --force
  revier each -- <cmd>          run one command in every project's directory
  revier each log [run]         past runs of it, or one run's results
  revier version

flags:
  -p, --project <name>          act on this project instead of the resolved one
`

// exitNoProject is returned when no project could be resolved. It is a
// normal outcome, not a failure: a script can tell it apart, and `go
// --picker`, which a desktop key runs, opens the popup instead.
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
	err := run(os.Stdout, args)
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
	invoked = cmd
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

// invoked is the command this process is running, as openLog read it. Only
// the configuration warning reads it, and only to stay out of `revier list`.
var invoked string

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
	// status, so a script can tell it from a failure.
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
	// `revier doctor` is its own report; a line here would add nothing to it.
	case errors.Is(err, errSilent):
		return 1, false
	}
	return 1, true
}

func run(out io.Writer, args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "version", "--version":
		fmt.Fprintf(out, "revier %s (%s, %s)\n", build.Version, build.Commit, build.Date)
		return nil
	case "help", "--help", "-h":
		fmt.Fprint(out, usage)
		return nil
	case "new":
		return cmdNew(out, args)
	case "doctor":
		// No app: what is wrong with the configuration is a question about
		// files, and probing the desktop for hosts would be a second way for
		// the command that diagnoses failures to fail.
		return cmdDoctor(out, args)
	case "each":
		// No app: a run in every project needs the project list and the state
		// root, and no host. Probing the desktop would be work, and a way to
		// fail, that has nothing to do with running a command in directories.
		// No deadline either, for the reason runAction has none.
		return cmdEach(out, args)
	case "agent":
		// Its own app: how long an agent command may take is one of its
		// flags, and the hosts are probed and listed inside that bound.
		return cmdAgent(out, args)
	case "shell":
		// Its own app, as agent new has: the hosts are probed and listed
		// inside the command's own bound.
		return cmdShell(out, args)
	case "session":
		// Before the app, as the other families answer it: a usage request
		// must not probe the desktop first.
		if len(args) == 0 || slices.Contains([]string{"help", "--help", "-h"}, args[0]) {
			fmt.Fprint(out, sessionUsage)
			return nil
		}
	}

	// A keypress command gets long enough for a detached launch's wait
	// (core.BindWait) on top of the host calls around it.
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	a, err := newApp(ctx, out)
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
	case "popup":
		// Refused before the launch: `revier popup --help` must not open a window.
		if len(args) != 0 {
			return fmt.Errorf("usage: revier popup")
		}
		return cmdPopup(ctx, a)
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
	case "shutdown":
		return cmdShutdown(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// commandTimeout bounds a command that has no bound of its own.
const commandTimeout = core.BindWait + 30*time.Second

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
