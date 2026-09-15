package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

const agentUsage = `revier agent - drive one agent from a script

usage:
  revier agent wait <agent> --until <status> [--timeout <seconds>]
  revier agent prompt <agent> [--] <text>
  revier agent new [-p <project>[:<target>] | --panel <id>] [--resume <id>] [--dir <path>]
  revier agent focus <agent> [--ref <instance>]

  <agent>    <project>, for the project's only agent, or <project>:<target>
             or <project>:<panel> for one of several
  --until    idle, running, attention, or stopped (idle or attention)
  --timeout  give up after this many seconds and exit 2; 0 waits for good
  --panel    the open workspace that holds this panel; kitty's
             @active-kitty-window-id is the window a key was pressed in
  --resume   start the agent on this conversation
  --dir      start the agent and its shell here, not in the project
  --ref      the instance holding <project>:<panel>, as the survey reports
             it: a panel id is unique only within one kitty process

new opens a tab in an open workspace: the project's agent panel and its
shell, the tab a restore adds for an agent opened beside the workspace.
Without -p or --panel the project is the one --dir is in, else the one this
directory resolves to.

focus makes the agent's tab current and raises the window that holds it.

wait prints the status it ended on. prompt types one line and submits it,
and returns once an idle agent has started on it, so
  revier agent prompt demo "..." && revier agent wait demo --until stopped
waits for that turn. It refuses a panel that is not an agent's, and an agent
waiting for an answer, where the Enter would pick an option in its dialog.
`

// errWaitTimeout is a wait that ran out of time. It has its own exit status,
// the one the shell tool used, so a script can tell it from a failure.
var errWaitTimeout = errors.New("timed out")

// promptTimeout bounds `revier agent prompt` from the host probe to the watch
// for the turn to start. A wedged tmux would otherwise hold it for good. A
// variable, so a test need not wait this long for it.
var promptTimeout = commandTimeout

func cmdAgent(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		fmt.Print(agentUsage)
		return nil
	case "wait":
		return cmdAgentWait(args)
	case "prompt":
		return cmdAgentPrompt(args)
	case "new":
		return cmdAgentNew(args)
	case "focus":
		return cmdAgentFocus(args)
	default:
		fmt.Fprint(os.Stderr, agentUsage)
		return fmt.Errorf("unknown agent command %q", sub)
	}
}

// cmdAgentWait waits as long as its caller says, which by default is for
// good: `revier agent wait` on a long turn is the point of it. The timeout
// covers the host probes and the lookup as well as the wait, since a host that
// never answers is a wait that never ends.
func cmdAgentWait(args []string) error {
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
	state, err := waitFor(ctx, pos[0], *until, statuses)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w after %gs waiting for %s to be %s; it is %s", errWaitTimeout, *timeout, pos[0], *until, state.Status)
	}
	if err != nil {
		return err
	}
	fmt.Println(state.Status)
	return nil
}

// waitFor finds the agent an address names and waits for one of the statuses.
// The state is the last one read, zero when the agent was never found. An
// agent of a remote project is waited on by the revier on its host, which
// answers with the status it ended on.
func waitFor(ctx context.Context, address, name string, until []revier.Status) (revier.AgentState, error) {
	a, err := newApp(ctx)
	if err != nil {
		return revier.AgentState{}, err
	}
	r, there, err := a.remoteFor(address)
	if err != nil {
		return revier.AgentState{}, err
	}
	if r != nil {
		status, err := r.Wait(ctx, there, name)
		if err != nil {
			return revier.AgentState{Status: status}, fmt.Errorf("%s: %w", address, err)
		}
		return revier.AgentState{Status: status}, nil
	}
	ag, err := a.agent(ctx, address)
	if err != nil {
		return revier.AgentState{}, err
	}
	state, err := a.core.Wait(ctx, ag, until, core.AgentPoll)
	if err != nil {
		return state, fmt.Errorf("%s: %w", address, err)
	}
	return state, nil
}

func cmdAgentPrompt(args []string) error {
	fs := flag.NewFlagSet("agent prompt", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: revier agent prompt <agent> [--] <text>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), promptTimeout)
	defer cancel()
	a, err := newApp(ctx)
	if err != nil {
		return err
	}
	r, there, err := a.remoteFor(pos[0])
	if err != nil {
		return err
	}
	if r != nil {
		// The remote revier makes every refusal and prints its own warning.
		return r.Prompt(ctx, there, pos[1])
	}
	ag, err := a.agent(ctx, pos[0])
	if err != nil {
		return err
	}
	state, err := a.core.Prompt(ctx, ag, pos[1], core.AgentPoll)
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	if state.Status == revier.StatusIdle {
		fmt.Fprintf(os.Stderr, "revier: warning: %s was still idle after the prompt was delivered; check the panel before waiting on it\n", pos[0])
	}
	return nil
}

// cmdAgentFocus brings one agent to the front: on the host of a remote
// project, through that revier, which is how the TUI reaches an agent there.
// With --ref the address names a panel of that instance, so a panel id two
// kitty processes share still names one agent.
func cmdAgentFocus(args []string) error {
	fs := flag.NewFlagSet("agent focus", flag.ContinueOnError)
	instance := fs.String("ref", "", "the instance holding the panel")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: revier agent focus <agent> [--ref <instance>]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	a, err := newApp(ctx)
	if err != nil {
		return err
	}
	address := pos[0]
	r, there, err := a.remoteFor(address)
	if err != nil {
		return err
	}
	start := time.Now()
	if r != nil {
		// The host knows the instance by its own runtime: only the id goes.
		err := r.FocusAgent(ctx, there, revier.TargetRef{ID: *instance})
		logging.Op("agent focus", start, err, "address", address, "host", r.Name(), "ref", *instance)
		return err
	}
	var ref revier.TargetRef
	var panel revier.PanelID
	switch {
	case *instance == "":
		ag, err := a.agent(ctx, address)
		if err != nil {
			return err
		}
		ref, panel = ag.Ref, ag.Panel.ID
	case a.core.Runtime == nil:
		return fmt.Errorf("%w: no runtime holds panels", core.ErrNoHost)
	default:
		_, sel, _ := strings.Cut(address, ":")
		ref, panel = revier.TargetRef{Host: a.core.Runtime.Name(), ID: *instance}, revier.PanelID(sel)
	}
	err = a.core.FocusAgent(ctx, ref, panel)
	logging.Op("agent focus", start, err, "address", address, "ref", ref, "panel", panel)
	return err
}

// cmdAgentNew adds an agent tab to an open workspace. It is what the kitty
// hotkey runs, so it prints nothing on success: there is no terminal to read
// it in.
func cmdAgentNew(args []string) error {
	fs := flag.NewFlagSet("agent new", flag.ContinueOnError)
	project := projectFlag(fs)
	panel := fs.String("panel", "", "the open workspace holding this panel")
	resume := fs.String("resume", "", "the conversation to start the agent on")
	dir := fs.String("dir", "", "the directory the agent starts in")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*project != "" && *panel != "") {
		return errors.New("usage: revier agent new [-p <project>[:<target>] | --panel <id>] [--resume <id>] [--dir <path>]")
	}
	if *dir != "" {
		if *dir, err = filepath.Abs(*dir); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	a, err := newApp(ctx)
	if err != nil {
		return err
	}
	return a.newAgent(ctx, *project, *panel, *dir, revier.SessionID(*resume))
}

// newAgent opens the agent tab where newTab puts it.
func (a *app) newAgent(ctx context.Context, project, panel, dir string, resume revier.SessionID) error {
	there := func(r revier.Remote, address string) error { return r.NewAgent(ctx, address, resume) }
	here := func(w core.Workspace) error {
		start := time.Now()
		outcome, err := a.core.NewAgent(ctx, w, core.Resume{Session: resume, Dir: dir})
		logging.Op("agent new", start, err, "project", w.Project.Name, "target", w.Target, "ref", w.Ref, "session", resume, "dir", dir, "outcome", outcome.String())
		if err != nil {
			return err
		}
		if resume != "" && outcome != core.AgentResumed {
			fmt.Fprintf(os.Stderr, "revier: warning: no probe here can resume %s; the agent started empty\n", resume)
		}
		return nil
	}
	return a.newTab(ctx, "agent new", project, panel, dir, a.core.AgentTarget, there, here)
}

// newTab opens a tab where the core places it: through there on the host of a
// remote project, or through here in an open workspace on this machine. The
// place is the owner of --panel; or the project -p names, else the one --dir
// is in, else the one resolved as for any command, with the target after -p's
// colon or the one pick chooses. --dir is a path on this machine, so it has
// to be a directory only where the tab opens here.
func (a *app) newTab(ctx context.Context, what, project, panel, dir string, pick func(core.Project) (revier.TargetName, error), there func(revier.Remote, string) error, here func(core.Workspace) error) error {
	place, err := a.tabPlace(ctx, project, panel, dir, pick)
	if err != nil {
		return err
	}
	if place.Remote != nil {
		start := time.Now()
		err := there(place.Remote, place.Address)
		logging.Op(what, start, err, "project", place.Workspace.Project.Name, "host", place.Remote.Name(), "address", place.Address)
		return err
	}
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf("--dir %s: not a directory", dir)
		}
	}
	return here(place.Workspace)
}

func (a *app) tabPlace(ctx context.Context, project, panel, dir string, pick func(core.Project) (revier.TargetName, error)) (core.TabPlace, error) {
	if panel != "" {
		return a.core.TabAt(ctx, a.projects, a.state.Bound, revier.PanelID(panel))
	}
	name, sel, _ := strings.Cut(project, ":")
	p, inDir := a.projectForPath(dir)
	if name != "" || dir == "" || !inDir {
		var err error
		if p, err = a.resolveProject(ctx, name); err != nil {
			return core.TabPlace{}, err
		}
	}
	return a.core.TabIn(ctx, p, revier.TargetName(sel), a.state.Bound[p.Name], pick)
}

// remoteFor returns the remote that drives the agent an address names, and
// the address as the host knows it; nil when the project is on this machine
// (decisions.md D41).
func (a *app) remoteFor(address string) (revier.Remote, string, error) {
	name, sel, _ := strings.Cut(address, ":")
	p, ok := a.project(revier.ProjectName(name))
	if !ok {
		return nil, "", fmt.Errorf("no project named %q", name)
	}
	// The project has its own name on the host; what follows the colon is
	// the host's to resolve.
	return a.core.RemoteAt(p, sel)
}

// agent finds the agent an address names: <project>, or <project>:<target>,
// or <project>:<panel>.
func (a *app) agent(ctx context.Context, address string) (core.Agent, error) {
	name, sel, _ := strings.Cut(address, ":")
	p, ok := a.project(revier.ProjectName(name))
	if !ok {
		return core.Agent{}, fmt.Errorf("no project named %q", name)
	}
	return a.core.Agent(ctx, p, sel, a.state.Bound[p.Name])
}
