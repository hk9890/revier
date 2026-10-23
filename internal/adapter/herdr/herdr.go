// Package herdr implements a Runtime host over herdr, a terminal multiplexer
// for coding agents (https://github.com/herdrdev/herdr), and the probe that
// reads herdr's own agent detection.
//
// An instance is a herdr workspace, the way a tmux instance is a session: its
// tabs are the tabs, and the panes of every tab are the panels a probe reads.
// One herdr server holds every workspace, and the host speaks its JSON API on
// the server's Unix socket, one request per connection.
package herdr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

// nameToken is the workspace metadata token that holds the name revier gave
// the workspace. A user can rename the workspace label in herdr; the token
// stays, so the instance keeps the title its realization's Match finds. A
// workspace with no such token, one revier did not open or one whose name
// herdr cannot keep as a token, is known by its label.
const nameToken = "revier_name"

// Host is a herdr Runtime. Socket selects the server's API socket; an empty
// Socket uses the one herdr's own CLI would use from this environment.
type Host struct {
	Socket string

	// call sends a request and returns the server's pid and the raw result.
	// Tests replace it; production leaves it nil and dials the socket.
	call func(ctx context.Context, method string, params any) (int, []byte, error)

	// procRoot replaces /proc in tests.
	procRoot string
}

func (h *Host) Name() string { return "herdr" }

func (h *Host) Capabilities() revier.Capabilities {
	return revier.Capabilities{}
}

// Probe reports herdr usable when the binary exists. It does not require a
// running server.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := exec.LookPath("herdr"); err != nil {
		return fmt.Errorf("herdr not on PATH: %w", err)
	}
	return nil
}

// Instances lists every workspace on the server, with the panes of all its
// tabs.
//
// One request, whatever the project count: session.snapshot carries every
// workspace, tab and pane. herdr names no pane's process, so one read of /proc
// finds them, and each pane's foreground process is then read as tmux's is.
// A pane moved to another workspace has a new id, and its process still
// carries the old one, so herdr is asked for that pane's process alone: one
// request per moved pane, not per pane.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	pid, snap, err := h.snapshot(ctx)
	if err != nil {
		if errors.Is(err, errNoServer) {
			return nil, nil
		}
		return nil, err
	}

	byWorkspace := map[string]*revier.Instance{}
	instances := make([]revier.Instance, len(snap.Workspaces))
	for i, w := range snap.Workspaces {
		title := titleOf(w)
		instances[i] = revier.Instance{
			Ref:   revier.TargetRef{Host: h.Name(), ID: refID(pid, w.ID), Title: title},
			Title: title,
		}
		byWorkspace[w.ID] = &instances[i]
	}

	processes := paneProcesses(h.root(), h.socket(), pid)
	for _, p := range snap.Panes {
		inst, ok := byWorkspace[p.WorkspaceID]
		if !ok {
			continue
		}
		shell, ok := processes[p.ID]
		if !ok {
			if shell, err = h.shellPID(ctx, p.ID); err != nil {
				return nil, err
			}
		}
		// A pane whose process was not found has no foreground: proc reads
		// none for pid 0.
		fg, command, _ := proc.ForegroundIn(h.root(), shell)
		inst.Panels = append(inst.Panels, revier.Panel{
			ID:      revier.PanelID(p.ID),
			Kind:    kindOf(command),
			Title:   p.TerminalTitle,
			Vars:    vars(p),
			PID:     fg,
			Command: command,
			Tab:     p.TabID,
		})
	}
	return instances, nil
}

// vars is a pane's tokens, and the agent herdr detected in it with its state,
// which the herdr probe reads.
func vars(p pane) map[string]string {
	if p.Agent == "" {
		return p.Tokens
	}
	out := map[string]string{agentVar: p.Agent, statusVar: p.AgentStatus}
	for k, v := range p.Tokens {
		out[k] = v
	}
	return out
}

// Focused is the workspace the server shows by default: the one the client
// that last moved went to, or the one a focus request named. Each client
// keeps a view of its own, so with two attached terminals on different
// workspaces this is the last one either moved to, not the one the user
// reads.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	pid, snap, err := h.snapshot(ctx)
	if err != nil {
		if errors.Is(err, errNoServer) {
			return revier.TargetRef{}, nil
		}
		return revier.TargetRef{}, err
	}
	for _, w := range snap.Workspaces {
		if w.ID == snap.FocusedWorkspaceID {
			return revier.TargetRef{Host: h.Name(), ID: refID(pid, w.ID), Title: titleOf(w)}, nil
		}
	}
	return revier.TargetRef{}, nil
}

// shellPID asks herdr for the process it started in a pane. A pane it cannot
// answer for has none: one pane must not fail the listing of every project.
// A caller that gave up does fail it: the panes left unasked would list with
// no process, and their agents would read as gone.
func (h *Host) shellPID(ctx context.Context, pane string) (int, error) {
	var res processInfoResult
	if _, err := h.request(ctx, "pane.process_info", map[string]string{"pane_id": pane}, &res); err != nil {
		if ctx.Err() != nil {
			return 0, err
		}
		return 0, nil
	}
	return res.ProcessInfo.ShellPID, nil
}

func (h *Host) snapshot(ctx context.Context) (int, snapshot, error) {
	var res snapshotResult
	pid, err := h.request(ctx, "session.snapshot", struct{}{}, &res)
	return pid, res.Snapshot, err
}

func titleOf(w workspace) string {
	if name := w.Tokens[nameToken]; name != "" {
		return name
	}
	return w.Label
}

// refID is an instance id: the workspace id, behind the pid of the server that
// assigned it. herdr numbers workspaces per server, so after a restart a bare
// workspace id can name an unrelated workspace, and a binding kept in state
// would raise it. The core trusts a runtime binding without re-checking it,
// so the id has to stay unique across servers.
func refID(serverPID int, workspace string) string {
	return strconv.Itoa(serverPID) + "/" + workspace
}

// kindOf classifies a pane by its foreground command: a shell or a tool. A
// pane with no command read is a shell, as the kitty and proc hosts report
// one.
func kindOf(argv []string) revier.PanelKind {
	if len(argv) == 0 || revier.IsShell(argv[0]) {
		return revier.PanelShell
	}
	return revier.PanelTool
}

func (h *Host) root() string {
	if h.procRoot != "" {
		return h.procRoot
	}
	return "/proc"
}
