package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

const agentUsage = `revier agent - drive one agent from a script

usage:
  revier agent wait <agent> --until <status> [--timeout <seconds>]
  revier agent prompt <agent> [--] <text>

  <agent>    <project>, for the project's only agent, or <project>:<target>
             or <project>:<panel> for one of several
  --until    idle, running, attention, or stopped (idle or attention)
  --timeout  give up after this many seconds and exit 2; 0 waits for good

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

// remoteFor returns the remote that drives the agent an address names, and
// the address as the host knows it; nil when the project is on this machine
// (decisions.md D41).
func (a *app) remoteFor(address string) (revier.Remote, string, error) {
	name, sel, hasSel := strings.Cut(address, ":")
	p, ok := a.project(revier.ProjectName(name))
	if !ok {
		return nil, "", fmt.Errorf("no project named %q", name)
	}
	r, err := a.core.RemoteOf(p)
	if err != nil || r == nil {
		return nil, "", err
	}
	// The project has its own name on the host; what follows the colon is
	// the host's to resolve.
	there := string(p.Remote.Project)
	if hasSel {
		there += ":" + sel
	}
	return r, there, nil
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
