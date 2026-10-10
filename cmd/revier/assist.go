package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hk9890/revier/internal/adapter/claude"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/tui"
)

// errNoAssistant is returned when no coding agent is installed to brief. It
// has its own exit status, so the surface can say it after the hand-over: what
// this command printed left the screen with it.
var errNoAssistant = tui.ErrNoAssistant

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
- The format, with a worked example: https://raw.githubusercontent.com/hk9890/revier/main/docs/design/extending.md
- Usage: https://raw.githubusercontent.com/hk9890/revier/main/README.md
- Read the files that are there before you write one. They are the examples that work on this machine.

How to work:
- Run "%[1]s doctor" after every edit. It names each file and target that did not load whole, and exits 1 when there is one. Do not report a change as done while it reports a problem you caused.
- "%[1]s help", "%[1]s list --json", "%[1]s status" and "%[1]s keys status" only read. Use them freely.
- "%[1]s open", "go", "popup", "run", "shutdown", "session restore", "agent prompt", "agent send-keys" and "keys install" act on the user's desktop or on their agents. Run one only when the user asks for it.
- Keep the comments and the order of a file you edit. Change the smallest thing that does what was asked.
- When the cause is a fault in revier and not in the configuration, do not work around it. Write the user an issue report: what they did, what happened, the doctor output, and the log lines.
- The user reviews your changes in revier when you exit. End with the list of files you changed and what each change does.
`

// cmdAssist hands this terminal to a coding agent briefed to configure and
// diagnose revier, and returns when the agent exits.
//
// No app, for the reason doctor has none and one more: this is the command
// that repairs a configuration, so it must start on one that does not load.
// It reads no file at all. The agent starts in a directory of its own under
// the state root, so its conversations are found again there and nothing it
// keeps beside them lands among the user's configuration.
func cmdAssist(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("assist", flag.ContinueOnError)
	printBrief := fs.Bool("print-brief", false, "print what the agent is told, and start none")
	if rest, err := parseArgs(fs, args); err != nil || len(rest) != 0 {
		return fmt.Errorf("usage: revier assist [--print-brief]")
	}
	cfgRoot, err := config.Root()
	if err != nil {
		return err
	}
	stateRoot, err := state.Root()
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	brief := fmt.Sprintf(assistBrief, self, config.File(cfgRoot), filepath.Join(cfgRoot, "projects"), logging.Dir(stateRoot))
	if *printBrief {
		_, err := fmt.Fprint(out, brief)
		return err
	}

	argv := claude.AssistArgv(brief, cfgRoot)
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return errNoAssistant
	}
	dir := filepath.Join(stateRoot, "assist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
