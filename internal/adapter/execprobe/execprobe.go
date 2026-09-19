// Package execprobe implements an AgentProbe as a subprocess, so a harness
// revier does not know can be supported without writing Go. The contract is
// docs/design/extending.md, "Level 2": one Panel as JSON on stdin, one
// AgentState as JSON on stdout, per panel per refresh.
//
// Only AgentProbe has this form. A Host is stateful and sits in the latency
// path of every keystroke, so it stays Go.
package execprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// DefaultTimeout bounds one probe run. It is well under the TUI's refresh
// interval, so a probe that hangs costs one panel its state for one refresh
// and never stalls the surface.
const DefaultTimeout = 500 * time.Millisecond

// Probe runs a declared binary. Construct it with New.
type Probe struct {
	name    string
	exec    string
	timeout time.Duration
}

// New returns a probe for the harness named name, run as exec. The name is
// also the foreground command the probe claims: a `[[probe]]` named "aider"
// reads panels whose command is aider, and no others, so an unrelated agent
// pane never costs a process.
func New(name, exec string) *Probe {
	return &Probe{name: name, exec: exec, timeout: DefaultTimeout}
}

// WithTimeout returns the probe with a different bound, for tests.
func (p *Probe) WithTimeout(d time.Duration) *Probe {
	out := *p
	out.timeout = d
	return &out
}

func (p *Probe) Name() string { return p.name }

// Match claims a panel whose foreground command is the harness name.
func (p *Probe) Match(panel revier.Panel) bool { return panel.Runs(p.name) }

// Inspect runs the binary once. A non-zero exit, malformed output, or the
// timeout is an error, which the core turns into StatusUnknown for this
// panel and nothing else: one broken probe cannot blank the dashboard.
func (p *Probe) Inspect(ctx context.Context, panel revier.Panel) (revier.AgentState, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	in, _ := json.Marshal(panel) // strings, ints, and a map: cannot fail
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, p.exec)
	c.Stdin, c.Stdout, c.Stderr = bytes.NewReader(in), &out, &errb
	// The probe learns the panel from stdin, not from where it runs, so it
	// runs in the home directory rather than in revier's own, which a removed
	// worktree can take away and fail every probe until revier is restarted.
	// A home that is unset or gone would fail every probe itself, and name
	// the probe as the missing file, so it leaves revier's own.
	c.Dir, _ = os.UserHomeDir()
	if _, err := os.Stat(c.Dir); err != nil {
		c.Dir = ""
	}
	// The timeout must end the whole process tree, not only the script: a
	// child it started holds the output pipe open, and Run would otherwise
	// wait on that pipe for as long as the child lives. So the probe runs in
	// its own process group, the group is killed on timeout, and the pipes are
	// abandoned shortly after.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 50 * time.Millisecond
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return revier.AgentState{}, fmt.Errorf("probe %s: timed out after %s", p.name, p.timeout)
		}
		return revier.AgentState{}, fmt.Errorf("probe %s: %w: %s", p.name, err, strings.TrimSpace(errb.String()))
	}
	var state revier.AgentState
	if err := json.Unmarshal(out.Bytes(), &state); err != nil {
		return revier.AgentState{}, fmt.Errorf("probe %s: decode state: %w", p.name, err)
	}
	if state.Harness == "" {
		state.Harness = p.name
	}
	return state, nil
}
