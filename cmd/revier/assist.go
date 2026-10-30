package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	userguide "github.com/hk9890/revier/docs/user-guide"
	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
)

// errNoAssistant is returned when no coding agent is installed to brief. It
// has its own exit status.
var errNoAssistant = errors.New("no coding agent to brief: install Claude Code, so that `claude` is on PATH")

// resumeGrace is how soon a continued agent must fail for the failure to be
// the continuing: Claude Code ends at once, with status 1, when it finds no
// conversation to continue in the files HasConversation saw. An agent that
// ran longer was used, and how it ended is its own.
const resumeGrace = 5 * time.Second

// assistBrief is what the agent is told on top of its own system prompt. It
// names commands and paths rather than describing the format: the commands
// answer for the installed version, and a description here would not.
const assistBrief = `You were started by revier, a project-grouped control surface for coding agents.
Your job in this session: change revier's configuration as the user asks, and find out why revier does not do what they expect.

Where things are:
- %[1]s is the revier binary that started you. Run this path, not a revier from PATH.
- %[2]s is the global configuration: hosts, theme, keys, actions, and the [[target]] entries every project shares.
- %[3]s holds one TOML file per project. A project file overrides fields of a shared target and adds targets of its own.
- %[4]s holds the daily logs. Every revier process appends to the day's file.
- "%[1]s assist --reference" prints the format of both, with worked examples, as this version of revier reads it. Read it before your first edit.
- Read the files that are there before you write one. They are the examples that work on this machine.

How to work:
- Run "%[1]s doctor" after every edit. It names each file and target that did not load whole, and exits 1 when there is one. Do not report a change as done while it reports a problem you caused.
- "%[1]s help", "%[1]s list --json", "%[1]s status", "%[1]s keys status" and "%[1]s term status" only read. Use them freely.
- "%[1]s open", "go", "popup", "run", "each", "attach", "shutdown", "session restore", "rename", "agent new", "agent close", "agent prompt", "agent send-keys", "shell new", "keys install", "keys uninstall", "term install" and "term uninstall" act on the user's desktop, on their projects, on their agents or on their terminal's configuration. Run one only when the user asks for it.
- Keep the comments and the order of a file you edit. Change the smallest thing that does what was asked.
- When the cause is a fault in revier and not in the configuration, do not work around it. Write the user an issue report: what they did, what happened, the doctor output, and the log lines.
- End with the list of files you changed and what each change does.
`

// assistReturn is how the brief ends for the agent the surface handed its
// terminal to. The one beside the surface returns to nothing, and its user
// reads the changes in the surface whenever they look at it.
const assistReturn = `- The user reviews your changes in revier when you exit: say that /exit returns to revier, where the changes are on the screen.
`

const assistUsage = "usage: revier assist [-p name] [--print-brief | --reference | --exec [--resume id]]"

// cmdAssist hands this terminal to a coding agent briefed to configure and
// diagnose revier, and returns when the agent exits. With --exec it becomes
// the agent instead, as the built-in project's panel runs it.
//
// No app, for the reason doctor has none and one more: this is the command
// that repairs a configuration, so it must start on one that does not load.
// It reads the configuration only to tell the agent what is wrong with it,
// and a configuration that does not load is the first thing it tells. The
// agent starts in a directory of its own under the state root, so the
// conversation held there is the one the next start continues, and nothing
// the agent keeps beside it lands among the user's configuration.
func cmdAssist(out io.Writer, args []string) (err error) {
	fs := flag.NewFlagSet("assist", flag.ContinueOnError)
	project := projectFlag(fs)
	printBrief := fs.Bool("print-brief", false, "print what the agent is told, and start none")
	reference := fs.Bool("reference", false, "print the format of the configuration")
	beside := fs.Bool("exec", false, "become the agent, beside the surface: the built-in project's panel runs this")
	session := fs.String("resume", "", "with --exec, the conversation to resume, as a session restore names it")
	rest, err := parseArgs(fs, args)
	modes := 0
	for _, on := range []bool{*printBrief, *reference, *beside} {
		if on {
			modes++
		}
	}
	// A panel's own arguments can fail too, and its window closes on them.
	if *beside || slices.Contains(args, "--exec") {
		defer func() { holdOpen(err) }()
	}
	if err != nil || len(rest) != 0 || modes > 1 || *session != "" && !*beside {
		return fmt.Errorf("%s", assistUsage)
	}
	if *reference {
		_, err := fmt.Fprint(out, assistReference())
		return err
	}
	cfgRoot, err := config.Root()
	if err != nil {
		return err
	}
	stateRoot, err := state.Root()
	if err != nil {
		return err
	}
	// The agent starts in a directory of its own, where a root given relative
	// to this one would name another place.
	if cfgRoot, err = filepath.Abs(cfgRoot); err != nil {
		return err
	}
	if stateRoot, err = filepath.Abs(stateRoot); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	now, err := assistContext(cfgRoot, *project)
	if err != nil {
		return err
	}
	brief := fmt.Sprintf(assistBrief, self, config.File(cfgRoot), filepath.Join(cfgRoot, "projects"), logging.Dir(stateRoot))
	if !*beside {
		brief += assistReturn
	}
	brief += now
	if *printBrief {
		_, err := fmt.Fprint(out, brief)
		return err
	}

	dir := filepath.Join(stateRoot, "assist")
	agent := claude.Assist{
		Brief:   brief,
		Subject: *project,
		Dirs:    []string{cfgRoot},
		Session: *session,
		Beside:  *beside,
	}
	// A second agent there - one more in the built-in, or this command in a
	// shell beside it - starts a conversation of its own: two that continue
	// the newest would write one transcript.
	if *session == "" && claude.HasConversation(dir) {
		agent.Resume = !claude.InUse(context.Background(), dir)
	}
	path, err := exec.LookPath(agent.Argv()[0])
	if err != nil {
		return errNoAssistant
	}
	// The configuration root is made with the agent's own directory: a new
	// installation has none, and --add-dir names a directory that is there.
	for _, d := range []string{dir, cfgRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if *beside {
		return becomeAgent(path, agent.Argv(), dir)
	}
	start := time.Now()
	err = runAgent(agent.Argv(), dir)
	if err != nil && agent.Resume && time.Since(start) < resumeGrace {
		agent.Resume = false
		err = runAgent(agent.Argv(), dir)
	}
	return err
}

// becomeAgent replaces this process with the agent, in dir. The panel's
// probe reads the program the panel runs, and a revier waiting on its child
// would be that program, with the agent hidden under it.
func becomeAgent(path string, argv []string, dir string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	// The exec replaces the process, so the command's own line is never
	// written: this one stands for it.
	slog.Info("assist", "exec", path, "dir", dir)
	return syscall.Exec(path, argv, os.Environ())
}

// holdOpen keeps a failure of --exec on the screen. The panel closes when the
// command ends, and with it the one place its reason was written; nothing
// else ran it to read why. A panel with no terminal has nobody to wait for.
func holdOpen(err error) {
	if err == nil || !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}
	fmt.Fprintf(os.Stderr, "revier assist: %v\n\nPress Enter to close.\n", err)
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// runAgent gives the terminal to the agent, in dir, until it exits.
func runAgent(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The surface keeps this command's stderr to read why it failed. The
	// agent draws on a terminal, so it gets the one stdout is, where stdout
	// is one.
	if !term.IsTerminal(int(os.Stderr.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		cmd.Stderr = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		// The agent is named: the line revier prints would otherwise be an
		// exit status of nobody's.
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}

// assistContext is what the brief says about the moment the agent was started
// in: the project the user was on with what does not load of it, and what
// does not load of config.toml. What does not load of any other project is
// doctor's to name. It is in the brief and not a first prompt, so the user
// still speaks first, and "fix this" then means something.
func assistContext(cfgRoot, project string) (string, error) {
	cfg, projects, err := config.Load(cfgRoot)
	if err != nil {
		return fmt.Sprintf("\nWhat is wrong now:\n- The configuration does not load, so no revier command but assist runs: %v\n", err), nil
	}
	var b strings.Builder
	if project != "" {
		i := slices.IndexFunc(projects, func(p core.Project) bool { return string(p.Name) == project })
		if i < 0 {
			return "", fmt.Errorf("no project named %q", project)
		}
		p := projects[i]
		fmt.Fprintf(&b, "\nWhere the user started you:\n- On the project %q, the file %s. Take \"this project\" and \"here\" to mean it.\n", p.Name, p.File)
		for _, problem := range config.Problems(p) {
			fmt.Fprintf(&b, "- It does not load whole: %v\n", problem)
		}
	}
	if len(cfg.Problems) > 0 {
		b.WriteString("\nWhat is wrong now:\n")
		for _, problem := range cfg.Problems {
			fmt.Fprintf(&b, "- config.toml does not load whole: %v\n", problem)
		}
	}
	return b.String(), nil
}

// assistReference is the configuration format as this build reads it: the
// reference pages of the user guide, in the order the guide reads them.
func assistReference() string {
	return userguide.Configuration + "\n" + userguide.Status + "\n" + userguide.Probes
}
