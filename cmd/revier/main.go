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
	"strings"
	"time"

	"github.com/hk9890/revier/internal/build"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
)

const usage = `revier - a project-grouped control surface for running agents

usage:
  revier                        the TUI: every project, its agent state, its targets
  revier list [--json [--conversations]] [name..]
                                what the TUI shows, printed once, or for the
                                named projects; --conversations names the
                                conversation each agent holds
  revier open [name] [--attach] run-or-raise a project's workspace; an unknown name
                                becomes a new project for this directory, and a
                                missing directory is cloned from git_url; --attach
                                ends with this terminal on it (tmux, herdr)
  revier new [name]             write a project file for this directory
  revier link [host [project]] [--name name]
                                the ssh hosts other than this machine; a host's
                                projects; or a link to one, written here under
                                --name or the project's own
  revier go <target> [-p name] [--picker]
                                run-or-raise a target; pressing it again returns home;
                                --picker opens the popup when no project resolves
  revier popup                  the TUI in a kitty window of its own, or the one open
  revier run <action> [-p name] run a configured action in the project
  revier attach [-p name]       bind the focused window to a project
  revier status [-p name]       which project this directory resolves to
  revier doctor                 every project file that did not load whole, and
                                what to do about each problem
  revier assist [-p name] [--print-brief | --reference | --exec [--resume id]]
                                hand this terminal to Claude Code, briefed to
                                configure revier and to find what is wrong with
                                it, on the conversation it held last; -p tells
                                it which project you mean; --print-brief prints
                                the brief and --reference the configuration
                                format, and neither starts anything; --exec
                                becomes the agent, as the built-in project
                                revier-config-assistant runs it
  revier keys status [--json]   the desktop chords revier wants, and who holds them
  revier keys install [--force] [-n] [--json]
                                claim the chords; --force takes one something
                                else holds; -n prints the plan and changes nothing
  revier keys uninstall [-n] [--json]
                                release the chords revier holds
  revier term status [--json]   the terminal keys revier wants, and what the
                                terminal's configuration holds of them
  revier term install [-n] [--json]
                                write them into the terminal's configuration
  revier term uninstall [-n] [--json]
                                remove what revier wrote there
  revier agent wait <agent> --until <status> [--timeout s]
                                block until an agent reaches a status
  revier agent prompt <agent> <text>
                                type one line into an agent and submit it
  revier agent read <agent> [--screen [--lines n]]
                                print what an agent said last, or what its
                                panel shows
  revier agent send-keys <agent> <key>..
                                type keys into an agent, whatever it is doing
  revier agent focus <agent> [--ref instance]
                                go to an agent's tab and raise its window
  revier agent new [name] [-p name | --panel id] [--resume id] [--dir path] [--no-focus]
                                add an agent tab, with its shell, to an open
                                workspace: the default agent, or the named
                                agent name; for a link, the tab's agent runs
                                on the host and --dir is dropped; --no-focus
                                leaves the focus and prints the agent's address
  revier shell new [-p name | --panel id] [--dir path]
                                add a shell tab to an open workspace; for a
                                link, the tab's shell runs on the host
  revier agent exec -p name [--agent name] [--tag tag] [--resume id] [--dir path]
  revier shell exec -p name [--tag tag] [--dir path]
                                what a link's panels run on its host
  revier session save [--name label]
                                record the projects that are open now
  revier session restore [id|name] [--dry-run]
                                open what a saved session recorded; without an
                                argument, the newest one saved by hand, by a
                                full shutdown, or from what the TUI last saw
                                before an end with no shutdown
  revier session list [--json]  the saved sessions, newest first
  revier shutdown [project] [--agents | --targets] [--force] [--no-session-save] [--dry-run]
                                save the session when it changed, then close every
                                open target and agent, or one project's; --agents
                                closes only the agents, --targets only what holds
                                no agent; refuses while an agent is busy unless
                                --force
  revier each [--filter '<sh>'] [-n] -- <cmd>
                                run one command in every project's directory;
                                --filter only where a shell test exits 0
  revier each log [run [project]]
                                past runs of it, one run's results, or what
                                the command printed in one project
  revier events [--days n] [--local]
                                what revier did in each project in the last n
                                days (7), one JSON line per event, with what
                                the revier on every linked host recorded;
                                --local asks no host
  revier version

flags:
  -p, --project <name>          act on this project instead of the resolved one
  -h, --help                    the usage of the command, or of its family
`

// exitNoProject is returned when no project could be resolved. It is a
// normal outcome, not a failure: a script can tell it apart, and `go
// --picker`, which a desktop key runs, opens the popup instead.
const exitNoProject = 3

// exitKeysIncomplete is returned when `revier keys install` ran and the
// desktop is not fully what was asked for. Every reason is already printed
// against the key it belongs to, so this status carries no message of its own.
const exitKeysIncomplete = 4

// exitTimeout is returned when `revier agent wait` gave up. It is not 1, so a
// script can tell a wait that ran out of time from one that failed.
const exitTimeout = 2

// exitEachFailed is returned when `revier each` ran and the command failed in
// at least one project. It is not 1, so a script can tell a failed project from
// a revier that could not run at all.
const exitEachFailed = 5

// exitNoAssistant is returned when `revier assist` found no coding agent to
// start. It is not 1, so a script can tell a machine with no agent from an
// agent that failed.
const exitNoAssistant = 6

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
	logCommand(start, args, err, status)
	if say {
		fmt.Fprintln(os.Stderr, "revier:", err)
	}
	os.Exit(status)
}

// logCommand records a command that ended with err and status. A run that
// ended as it should is no failure, whatever its status: a surface a signal
// closed, and a doctor that found problems, which is its report.
func logCommand(start time.Time, args []string, err error, status int) {
	attrs := []any{"args", args, "exit", status}
	var ended signalEnd
	var found problemsFound
	switch {
	case errors.As(err, &ended):
		err, attrs = nil, append(attrs, "signal", ended.sig.String())
	case errors.As(err, &found):
		err, attrs = nil, append(attrs, "problems", found.files)
	}
	logging.Op("command", start, err, attrs...)
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
		events.Setup(root)
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
	var ended signalEnd
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
	case errors.Is(err, errNoAssistant):
		return exitNoAssistant, true
	// The surface a signal ended has no terminal left to say it on.
	case errors.As(err, &ended):
		return 128 + int(ended.sig), false
	// `revier doctor` is its own report; a line here would add nothing to it.
	case errors.As(err, new(problemsFound)):
		return 1, false
	}
	return 1, true
}

func run(out io.Writer, args []string) error {
	// Before anything starts a process: a command run in a directory that was
	// removed under the shell must not fail every probe and every git call
	// for that reason (decisions.md D92). The surface leaves its start
	// directory outright, and does it after reading the project there.
	leaveLostStartDir()

	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	// Before any command runs: a usage request must not probe the desktop,
	// open a window, or contact a host.
	if asksHelp(cmd, args) {
		if text := commandHelp(cmd); text != "" {
			_, _ = fmt.Fprint(out, text)
			return nil
		}
	}

	switch cmd {
	case "version", "--version":
		_, _ = fmt.Fprintf(out, "revier %s (%s, %s)\n", build.Version, build.Commit, build.Date)
		return nil
	case "help", "--help", "-h":
		_, _ = fmt.Fprint(out, usage)
		return nil
	case "new":
		return cmdNew(out, args)
	case "doctor":
		// No app: what is wrong with the configuration is a question about
		// files, and probing the desktop for hosts would be a second way for
		// the command that diagnoses failures to fail.
		return cmdDoctor(out, args)
	case "assist":
		// No app either: see cmdAssist.
		return cmdAssist(out, args)
	case "events":
		// No app, for the reason each has none: the file and the hosts the
		// project files name are all it reads.
		return cmdEvents(out, args)
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
	}

	// A keypress command gets long enough for a detached launch's wait
	// (core.BindWait) on top of the host calls around it.
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
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
		// Refused before the launch: an argument must not open a window.
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
	case "term":
		return cmdTerm(ctx, a, args)
	case "shutdown":
		return cmdShutdown(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// familyUsage is the usage of each command that has its own, in full: the
// families, and the commands whose output wants a page of its own.
var familyUsage = map[string]string{
	"agent":   agentUsage,
	"shell":   shellUsage,
	"session": sessionUsage,
	"keys":    keysUsage,
	"term":    termUsage,
	"each":    eachUsage,
	"events":  eventsUsage,
}

// commandHelp is what `revier <cmd> --help` prints: the family's own usage,
// else the lines of usage that describe cmd. It is empty for a command that
// does not exist.
func commandHelp(cmd string) string {
	if text, ok := familyUsage[cmd]; ok {
		return text
	}
	var b strings.Builder
	in := false
	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(line, "  revier ") {
			// The command is the word after revier. The TUI's line has none,
			// and the first word of its description is no command.
			in = line == "  revier "+cmd || strings.HasPrefix(line, "  revier "+cmd+" ")
		} else if !strings.HasPrefix(line, "   ") {
			in = false
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "usage:\n" + b.String()
}

// asksHelp reports whether args ask cmd for its usage. A help flag counts
// anywhere before `--`, which is what the flag parser reads it as. A first
// argument of help counts only where nothing names a project, a target or an
// action: in a family, and in link, whose first argument is a host. Given
// nothing, session, keys and term answer with their usage too, before the app
// their dispatch builds; agent and shell answer it themselves.
func asksHelp(cmd string, args []string) bool {
	_, family := familyUsage[cmd]
	if len(args) == 0 {
		return cmd == "session" || cmd == "keys" || cmd == "term"
	}
	if args[0] == "help" && (family || cmd == "link") {
		return true
	}
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "-help", "--h", "--help":
			return true
		}
	}
	return false
}

// commandWait bounds a command that has no bound of its own.
var commandWait = core.BindWait + 30*time.Second

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
//
// The flag set prints nothing: main prints a parse error once, and run answers
// a help request before any command parses its flags. Everything after `--` is
// positional, so a prompt or a key may start with a dash.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if read := len(args) - len(rest); read > 0 && args[read-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
