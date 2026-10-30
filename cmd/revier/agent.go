package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	usecase "github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

const agentUsage = `revier agent - drive one agent from a script

usage:
  revier agent wait <agent> --until <status> [--timeout <seconds>]
  revier agent prompt <agent> [--] <text>
  revier agent read <agent> [--screen [--lines <n>]]
  revier agent send-keys <agent> [--] <key>..
  revier agent new [<name>] [-p <project>[:<target>] | --panel <id>] [--resume <id>] [--dir <path>] [--no-focus]
  revier agent focus <agent>
  revier agent exec -p <project> [--agent <name>] [--tag <tag>] [--resume <id>] [--dir <path>]

  <agent>    <project>:<target>:<panel>, the agent's address as
             revier list --json and agent new --no-focus give it; or
             <project> or <project>:<target>, for the only agent there, or
             the default agent among named ones
  --until    idle, running, attention, or stopped (idle or attention)
  --timeout  give up after this many seconds and exit 2; 0 waits for good
  --screen   print what the agent's panel shows, not what the agent said
  --lines    with --screen, the last <n> lines of the panel and its
             scrollback
  <name>     one of the project's named agents, the [[agent]] tables of its
             configuration; without it, the project's default agent
  <key>      esc, enter, tab, shift+tab, space, backspace, up, down, left,
             right, ctrl+c, or one character
  --panel    the open workspace that holds this panel; kitty's
             @active-kitty-window-id is the window a key was pressed in,
             and tmux's #{pane_id} the pane, which takes tmux as the
             runtime wherever the runtime list names it before none
  --agent    with exec, run this named agent, not the default agent
  --resume   start the agent on this conversation
  --dir      start the agent and its shell here, not in the project;
             without --dir, --panel starts them in the project too,
             not in the panel's directory
  --no-focus leave the focus where it is, and print the new agent's address

new opens a tab in an open workspace: the project's agent panel and its
shell, the tab a restore adds for an agent opened beside the workspace. With
<name> the tab's agent panel runs that named agent, and an address of the
project alone still names the default agent among them.
Without -p or --panel the project is the one --dir is in, else the one this
directory is in; a directory in no project is an error, which exits 3. It
goes to the new agent and raises its window; with --no-focus it does
neither, and prints the address a script reaches the agent by, which names
an agent once the harness in the tab has started.
With --panel in a window that is no project's open workspace, it opens what
[terminal] outside says: a plain shell tab beside the window, or nothing.

focus makes the agent's tab current and raises the window that holds it.

exec becomes the project's agent in this terminal. It is what the agent panel
of a link runs on the project's machine over ssh; --tag is that panel's name
for the agent, which a survey there reports it under.

wait prints the status it ended on. prompt types one line and submits it,
and returns once an idle agent has started on it, so
  revier agent prompt demo "..." && revier agent wait demo --until stopped
waits for that turn. It refuses a panel that is not an agent's, an agent
waiting for an answer, where the Enter would pick an option in its dialog,
and an agent whose state is unknown, as an opencode agent's is outside herdr,
since it may be showing such a dialog. send-keys reaches either.

read prints the last thing the agent said, where its harness keeps a
conversation revier can read: Claude Code's does. --screen prints the text
of the agent's panel, which every harness has, and the agent of a link too.

send-keys types the keys in order, whatever the agent is doing: it is how a
dialog is answered and a turn interrupted. Read the screen first; a key
lands on whatever the panel shows.
`

// errWaitTimeout is a wait that ran out of time. It has its own exit status,
// so a script can tell it from a failure.
var errWaitTimeout = errors.New("timed out")

// promptWait bounds `revier agent prompt` from the host probe to the watch
// for the turn to start. A wedged tmux would otherwise hold it for good. A
// variable, so a test need not wait this long for it.
var promptWait = commandWait

func cmdAgent(out io.Writer, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		_, _ = fmt.Fprint(out, agentUsage)
		return nil
	case "wait":
		return cmdAgentWait(out, args)
	case "prompt":
		return cmdAgentPrompt(out, args)
	case "read":
		return cmdAgentRead(out, args)
	case "send-keys":
		return cmdAgentKeys(out, args)
	case "new":
		return cmdAgentNew(out, args)
	case "focus":
		return cmdAgentFocus(out, args)
	case "exec":
		return cmdAgentExec(args)
	default:
		fmt.Fprint(os.Stderr, agentUsage)
		return fmt.Errorf("unknown agent command %q", sub)
	}
}

// cmdAgentWait waits as long as its caller says, which by default is for
// good: `revier agent wait` on a long turn is the point of it. The timeout
// covers the host probes and the lookup as well as the wait, since a host that
// never answers is a wait that never ends.
func cmdAgentWait(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent wait", flag.ContinueOnError)
	until := fs.String("until", "", "idle, running, attention, or stopped")
	timeout := fs.Float64("timeout", 0, "seconds before giving up; 0 waits for good")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *until == "" {
		return fmt.Errorf("usage: revier agent wait <agent> --until <status> [--timeout <seconds>]")
	}
	statuses, err := core.Until(*until)
	if err != nil {
		return err
	}
	if *timeout < 0 {
		return fmt.Errorf("--timeout must not be negative")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(*timeout*float64(time.Second)))
	}
	defer cancel()
	state, err := waitFor(ctx, out, pos[0], statuses)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w after %gs waiting for %s to be %s; it is %s", errWaitTimeout, *timeout, pos[0], *until, state.Status)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, state.Status)
	return nil
}

// waitFor finds the agent an address names and waits for one of the statuses.
// The state is the last one read, zero when the agent was never found.
func waitFor(ctx context.Context, out io.Writer, address string, until []revier.Status) (revier.AgentState, error) {
	a, err := newApp(ctx, out)
	if err != nil {
		return revier.AgentState{}, err
	}
	ag, err := a.agent(ctx, address)
	if err != nil {
		return revier.AgentState{}, err
	}
	state, err := a.core.Wait(ctx, ag, until, ag.Poll())
	if err != nil {
		return state, fmt.Errorf("%s: %w", address, err)
	}
	return state, nil
}

func cmdAgentPrompt(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent prompt", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: revier agent prompt <agent> [--] <text>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), promptWait)
	defer cancel()
	a, err := newApp(ctx, out)
	if err != nil {
		return err
	}
	ag, err := a.agent(ctx, pos[0])
	if err != nil {
		return err
	}
	state, err := a.core.Prompt(ctx, ag, pos[1], ag.Poll())
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	if state.Status == revier.StatusIdle {
		fmt.Fprintf(os.Stderr, "revier: warning: %s was still idle after the prompt was delivered; check the panel before waiting on it\n", pos[0])
	}
	return nil
}

// cmdAgentRead prints what an agent said last, or with --screen what its
// panel shows (decisions.md D116). Either is printed as text: what an agent
// wrote and what a tool printed are not instructions to the terminal that
// reads this (D108).
func cmdAgentRead(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent read", flag.ContinueOnError)
	screen := fs.Bool("screen", false, "what the agent's panel shows")
	lines := fs.Int("lines", 0, "with --screen, the last lines of the panel and its scrollback")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *lines < 0 || (*lines > 0 && !*screen) {
		return errors.New("usage: revier agent read <agent> [--screen [--lines <n>]]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	a, err := newApp(ctx, out)
	if err != nil {
		return err
	}
	ag, err := a.agent(ctx, pos[0])
	if err != nil {
		return err
	}
	if !*screen {
		said, err := a.core.Said(ctx, ag)
		if errors.Is(err, core.ErrNoMessage) {
			return fmt.Errorf("%s: %w; --screen prints what its panel shows", pos[0], err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", pos[0], err)
		}
		_, _ = fmt.Fprintln(out, strings.TrimRight(plainText(said.Message), "\n"))
		return nil
	}
	text, err := a.core.Screen(ctx, revier.AgentView{Ref: ag.Ref, Panel: ag.Panel.ID}, *lines > 0)
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	for _, line := range screenLines(text, *lines) {
		_, _ = fmt.Fprintln(out, line)
	}
	return nil
}

// plainText is s with every escape sequence and every control character but
// the newline and the tab taken off.
func plainText(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}

// screenLines is a panel's text as lines of plain text, without the space a
// terminal pads a row with and without the empty rows under the last line:
// all of them, or the last n.
func screenLines(text string, n int) []string {
	lines := strings.Split(plainText(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// cmdAgentKeys types named keys into an agent's panel (decisions.md D117).
func cmdAgentKeys(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent send-keys", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: revier agent send-keys <agent> [--] <key> [<key> ..]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), promptWait)
	defer cancel()
	a, err := newApp(ctx, out)
	if err != nil {
		return err
	}
	ag, err := a.agent(ctx, pos[0])
	if err != nil {
		return err
	}
	if err := a.core.SendKeys(ctx, ag, pos[1:], core.KeyGap); err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	return nil
}

// cmdAgentFocus brings one agent to the front.
func cmdAgentFocus(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent focus", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: revier agent focus <agent>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	a, err := newApp(ctx, out)
	if err != nil {
		return err
	}
	address := pos[0]
	start := time.Now()
	ag, err := a.agent(ctx, address)
	if err != nil {
		return err
	}
	name, _, _ := strings.Cut(address, ":")
	err = a.core.FocusAgent(ctx, revier.ProjectName(name), revier.AgentView{Ref: ag.Ref, Panel: ag.Panel.ID, State: ag.State})
	logging.Op("agent focus", start, err, "address", address, "ref", ag.Ref, "panel", ag.Panel.ID)
	return err
}

// cmdAgentNew adds an agent tab to an open workspace. It is what the kitty
// hotkey runs, so it prints nothing on success: there is no terminal to read
// it in. With --no-focus the caller is a script, and reads the address.
func cmdAgentNew(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent new", flag.ContinueOnError)
	project := projectFlag(fs)
	panel := fs.String("panel", "", "the open workspace holding this panel")
	resume := fs.String("resume", "", "the conversation to start the agent on")
	dir := fs.String("dir", "", "the directory the agent starts in")
	noFocus := fs.Bool("no-focus", false, "leave the focus where it is, and print the agent's address")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (*project != "" && *panel != "") {
		return errors.New("usage: revier agent new [<name>] [-p <project>[:<target>] | --panel <id>] [--resume <id>] [--dir <path>] [--no-focus]")
	}
	r := core.Resume{Session: revier.SessionID(*resume)}
	if len(pos) == 1 {
		r.Agent = pos[0]
	}
	if *dir != "" {
		if *dir, err = filepath.Abs(*dir); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	a, err := newAppAt(ctx, out, *panel)
	if err != nil {
		return err
	}
	if *noFocus {
		return a.addAgent(ctx, *project, *panel, *dir, r)
	}
	return a.newAgent(ctx, *project, *panel, *dir, r)
}

// newAgent opens the agent tab where newTab puts it, and goes to it.
func (a *app) newAgent(ctx context.Context, project, panel, dir string, r core.Resume) error {
	return a.agentTab(ctx, project, panel, dir, r, func(w core.Workspace, r core.Resume) (core.AgentOutcome, error) {
		return a.core.NewAgent(ctx, w, r)
	})
}

// addAgent opens the agent tab where newTab puts it, leaves the focus where
// it is, and prints the address the new agent answers to: the caller is a
// script, which has nothing else to find it by (decisions.md D118).
func (a *app) addAgent(ctx context.Context, project, panel, dir string, r core.Resume) error {
	return a.agentTab(ctx, project, panel, dir, r, func(w core.Workspace, r core.Resume) (core.AgentOutcome, error) {
		added, outcome, err := a.core.AddAgent(ctx, w, r)
		if added != "" {
			_, _ = fmt.Fprintln(a.out, a.core.AddressIn(w, added))
		}
		return outcome, err
	})
}

// agentTab opens an agent tab, started as r says, through open in the
// workspace newTab places it in.
func (a *app) agentTab(ctx context.Context, project, panel, dir string, r core.Resume, open func(core.Workspace, core.Resume) (core.AgentOutcome, error)) error {
	here := func(w core.Workspace, dir string) error {
		start := time.Now()
		r.Dir = dir
		outcome, err := open(w, r)
		logging.Op("agent new", start, err, "project", w.Project.Name, "target", w.Target, "ref", w.Ref, "agent", r.Agent, "session", r.Session, "dir", dir, "outcome", outcome.String())
		if err != nil {
			return err
		}
		if outcome != core.AgentResumed && r.Session != "" {
			fmt.Fprintf(os.Stderr, "revier: warning: %s was not resumed (%s); the agent started empty\n", r.Session, outcome)
		}
		return nil
	}
	return a.newTab(ctx, "agent new", revier.PanelAgent, project, panel, dir, a.core.AgentTarget, here)
}

// newTab opens a tab through here in the open workspace the core places it in.
// The place is the owner of --panel; or the project -p names, else the one --dir
// is in, else the one the working directory is in, with the target after -p's
// colon or the one pick chooses. A --panel that no project's workspace holds
// gets what [terminal] outside says, and here is not called. With --panel and
// no --dir, a shell tab's directory is the panel's own (decisions.md D153),
// and an agent tab's is the project's (D158).
func (a *app) newTab(ctx context.Context, what string, kind revier.PanelKind, project, panel, dir string, pick func(core.Project) (revier.TargetName, error), here func(core.Workspace, string) error) error {
	if panel != "" {
		return a.core.TabAtPanel(ctx, a.projects, revier.PanelID(panel), kind, dir, a.cfg.Outside(), func(w core.Workspace, dir string) error {
			return a.tabHere(what, w, panel, dir, here)
		})
	}
	place, err := a.tabPlace(ctx, project, dir, pick)
	if err != nil {
		return err
	}
	return a.tabHere(what, place, panel, dir, here)
}

// tabHere opens the tab through here in the workspace w. --dir is a path on
// this machine, so a link, whose tab starts on its host, drops it.
//
// A directory outside the project that holds --panel is dropped, and the tab
// opens where the project's own tab would: a panel's shell can sit anywhere.
func (a *app) tabHere(what string, w core.Workspace, panel, dir string, here func(core.Workspace, string) error) error {
	if w.Project.Remote != nil {
		dir = ""
	}
	if panel != "" && dir != "" {
		if p, ok := a.projectForPath(dir); !ok || p.Name != w.Project.Name {
			slog.Warn(what, "err", "the directory is outside the project of --panel; the tab opens in the project", "project", w.Project.Name, "dir", dir)
			dir = ""
		}
	}
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf("--dir %s: not a directory", dir)
		}
	}
	return here(w, dir)
}

func (a *app) tabPlace(ctx context.Context, project, dir string, pick func(core.Project) (revier.TargetName, error)) (core.Workspace, error) {
	name, sel, _ := strings.Cut(project, ":")
	var p core.Project
	var err error
	if name != "" {
		p, err = a.resolveProject(ctx, name)
	} else {
		p, err = a.dirProject(dir)
	}
	if err != nil {
		return core.Workspace{}, err
	}
	return a.core.TabIn(ctx, p, revier.TargetName(sel), pick)
}

// dirProject is the project --dir is in, else the one the working directory
// is in. A tab is opened from a terminal or a script, which has a directory:
// one in no project fails, and never opens the tab in the project of the
// focused window or the last one (decisions.md D160).
func (a *app) dirProject(dir string) (core.Project, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return core.Project{}, fmt.Errorf("working directory: %w", err)
		}
		dir = cwd
	}
	p, ok := a.projectForPath(dir)
	if !ok {
		return core.Project{}, dirInNoProject(dir)
	}
	slog.Info("resolve", "project", p.Name, "by", "directory", "dir", dir)
	return p, nil
}

// dirInNoProject is a directory a tab was asked for that no project holds. It
// is errNoProject, so it exits with that status, but says which directory
// and what to give instead.
type dirInNoProject string

func (d dirInNoProject) Error() string {
	return string(d) + " is in no project; name one with -p <project>"
}

func (d dirInNoProject) Unwrap() error { return errNoProject }

// agent finds the agent an address names: <project>, <project>:<target> or
// <project>:<target>:<panel>.
func (a *app) agent(ctx context.Context, address string) (core.Agent, error) {
	name, sel, _ := strings.Cut(address, ":")
	p, ok := a.project(revier.ProjectName(name))
	if !ok {
		return core.Agent{}, fmt.Errorf("no project named %q", name)
	}
	return a.core.Agent(ctx, p, sel)
}

// cmdAgentExec becomes the project's agent, for a terminal on another machine:
// it is what the agent panel of a link runs here over ssh (decisions.md D84).
func cmdAgentExec(args []string) error {
	fs := flag.NewFlagSet("agent exec", flag.ContinueOnError)
	project := projectFlag(fs)
	agent := fs.String("agent", "", "the named agent to run, else the project's default agent")
	tag := fs.String("tag", "", "the name the terminal that shows the agent knows it by")
	resume := fs.String("resume", "", "the conversation to start the agent on")
	dir := fs.String("dir", "", "the directory the agent starts in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *project == "" {
		return errors.New("usage: revier agent exec -p <project> [--agent <name>] [--tag <tag>] [--resume <id>] [--dir <path>]")
	}
	return serve(*project, *tag, func(c *core.Core, p core.Project) (core.Served, error) {
		// A checkout that is missing here is cloned by the agent's panel
		// alone: the shell's starts beside it, and two clones into one
		// directory fail each other. serve's core keeps no state, but the
		// clone's line still goes to the state record.
		if _, err := usecase.Clone(ledger.File{}, p.Project, os.Stderr); err != nil {
			return core.Served{}, err
		}
		s, err := c.ServeAgent(p, core.Resume{Session: revier.SessionID(*resume), Dir: *dir, Agent: *agent})
		if err == nil && *resume != "" && s.Outcome != core.AgentResumed {
			fmt.Fprintf(os.Stderr, "revier: warning: %s was not resumed (%s); the agent starts empty\n", *resume, s.Outcome)
		}
		return s, err
	})
}
