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

func cmdAgent(ctx context.Context, a *app, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "help", "--help", "-h":
		fmt.Print(agentUsage)
		return nil
	case "wait":
		return cmdAgentWait(ctx, a, args)
	case "prompt":
		return cmdAgentPrompt(ctx, a, args)
	default:
		fmt.Fprint(os.Stderr, agentUsage)
		return fmt.Errorf("unknown agent command %q", sub)
	}
}

func cmdAgentWait(ctx context.Context, a *app, args []string) error {
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
	ag, err := a.agent(ctx, pos[0])
	if err != nil {
		return err
	}
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeout*float64(time.Second)))
		defer cancel()
	}
	state, err := a.core.Wait(ctx, ag, statuses, core.AgentPoll)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w after %gs waiting for %s to be %s; it is %s", errWaitTimeout, *timeout, pos[0], *until, state.Status)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	fmt.Println(state.Status)
	return nil
}

func cmdAgentPrompt(ctx context.Context, a *app, args []string) error {
	fs := flag.NewFlagSet("agent prompt", flag.ContinueOnError)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: revier agent prompt <agent> [--] <text>")
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
