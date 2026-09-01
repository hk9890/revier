// Package tmux implements a Runtime host over tmux.
//
// It exists first among the runtime adapters because it is the one that can be
// driven with no display at all: `tmux -L <socket>` starts a private server,
// and select-window and the #{window_active} format both work with no client
// attached. That makes it the substrate the live test layer runs against, and
// the reason most of revier can be verified without touching a desktop
// session. See docs/TESTING.md.
//
// An instance is a tmux window; its panes are the panels a probe reads.
package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// sep separates fields in tmux format output.
//
// It is a printable character on purpose. tmux escapes non-printable bytes in
// format output on some versions and passes them through on others - 3.4 turns
// a raw \x1f into the literal text "\037" while 3.7 emits the byte - so a
// control character is not a portable delimiter.
//
// Free text can still contain this character, so every query below places its
// one free-text field LAST and splits with a fixed field count, letting that
// field absorb any separators it contains. That is why window names and pane
// titles are fetched by two separate calls rather than one: a single line
// cannot have two free-text fields at the end.
const sep = "|"

// Host is a tmux Runtime. Socket selects a private tmux server; an empty
// Socket uses the user's default server.
type Host struct {
	Socket string

	// Session is the tmux session new instances are created in. Empty uses
	// "revier".
	Session string
}

func (h *Host) Name() string { return "tmux" }

func (h *Host) Capabilities() revier.Capabilities {
	return revier.Capabilities{Layout: true, Persistent: true}
}

func (h *Host) session() string {
	if h.Session != "" {
		return h.Session
	}
	return "revier"
}

func (h *Host) cmd(ctx context.Context, args ...string) *exec.Cmd {
	full := []string{}
	if h.Socket != "" {
		full = append(full, "-L", h.Socket)
	}
	return exec.CommandContext(ctx, "tmux", append(full, args...)...)
}

func (h *Host) run(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	c := h.cmd(ctx, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Probe reports tmux usable when the binary exists. It does not require a
// running server: Open starts one.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not on PATH: %w", err)
	}
	return nil
}

// Instances lists every window on the server, panes included.
//
// Two calls, both bulk: one for window names and one for panes. The cost is
// constant, not per project, which is the property the hot path actually needs.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	names, err := h.windowNames(ctx)
	if err != nil {
		return nil, err
	}
	if names == nil {
		return nil, nil
	}

	// pane_title is free text and comes last, so SplitN gives it whatever it
	// contains.
	format := strings.Join([]string{
		"#{window_id}", "#{pane_id}", "#{pane_pid}", "#{pane_current_command}", "#{pane_title}",
	}, sep)

	out, err := h.run(ctx, "list-panes", "-a", "-F", format)
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}

	order := []string{}
	byWindow := map[string]*revier.Instance{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 5)
		if len(f) != 5 {
			// A malformed line is skipped, never fatal. Panes belonging to
			// other tools share this server, and one odd line must not blank
			// every project revier knows about.
			continue
		}
		winID, paneID, panePID, paneCmd, paneTitle := f[0], f[1], f[2], f[3], f[4]

		inst, ok := byWindow[winID]
		if !ok {
			name := names[winID]
			inst = &revier.Instance{
				Ref:   revier.TargetRef{Host: h.Name(), ID: winID, Title: name},
				Title: name,
			}
			byWindow[winID] = inst
			order = append(order, winID)
		}
		pid, _ := strconv.Atoi(panePID)
		inst.Panels = append(inst.Panels, revier.Panel{
			ID:      revier.PanelID(paneID),
			Kind:    kindOf(paneCmd),
			Title:   paneTitle,
			PID:     pid,
			Command: []string{paneCmd},
		})
	}

	instances := make([]revier.Instance, 0, len(order))
	for _, id := range order {
		instances = append(instances, *byWindow[id])
	}
	return instances, nil
}

// windowNames maps window id to window name. window_name is free text, so it
// is the last field of its own query.
func (h *Host) windowNames(ctx context.Context) (map[string]string, error) {
	out, err := h.run(ctx, "list-windows", "-a", "-F", "#{window_id}"+sep+"#{window_name}")
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	names := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 2)
		if len(f) != 2 {
			continue
		}
		names[f[0]] = f[1]
	}
	return names, nil
}

// kindOf classifies a pane by its foreground command. The agent kind is what
// the survey probes; everything else is a shell or a tool.
func kindOf(cmd string) revier.PanelKind {
	switch cmd {
	case "claude", "claude-code", "opencode", "aider":
		return revier.PanelAgent
	case "bash", "zsh", "sh", "fish":
		return revier.PanelShell
	}
	return revier.PanelTool
}

// Open creates a window named r.Name, starting the server and the session when
// neither exists yet.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" {
		return revier.TargetRef{}, fmt.Errorf("tmux: realization has no name to give the window")
	}

	args := []string{"new-window", "-P", "-F", "#{window_id}", "-n", r.Name}
	if !h.hasSession(ctx) {
		args = []string{"new-session", "-d", "-P", "-F", "#{window_id}", "-s", h.session(), "-n", r.Name}
	} else {
		args = append(args, "-t", h.session()+":")
	}
	if r.Dir != "" {
		args = append(args, "-c", r.Dir)
	}
	args = append(args, r.Launch...)

	out, err := h.run(ctx, args...)
	if err != nil {
		return revier.TargetRef{}, err
	}
	id := strings.TrimSpace(out)
	return revier.TargetRef{Host: h.Name(), ID: id, Title: r.Name}, nil
}

func (h *Host) hasSession(ctx context.Context) bool {
	_, err := h.run(ctx, "has-session", "-t", h.session())
	return err == nil
}

// Focus selects the window. select-window works with no client attached, which
// is what makes the live test layer possible without a terminal.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	_, err := h.run(ctx, "select-window", "-t", ref.ID)
	return err
}

// Focused reports the session's current window.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	out, err := h.run(ctx, "display-message", "-p", "-t", h.session()+":",
		"#{window_id}"+sep+"#{window_name}")
	if err != nil {
		if noServer(err) {
			return revier.TargetRef{}, nil
		}
		return revier.TargetRef{}, err
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), sep, 2)
	if len(f) != 2 {
		return revier.TargetRef{}, fmt.Errorf("tmux display-message: unexpected %q", out)
	}
	return revier.TargetRef{Host: h.Name(), ID: f[0], Title: f[1]}, nil
}

// noServer reports the "no server running" family of tmux errors, which mean
// "nothing is open" rather than "something went wrong".
func noServer(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting") ||
		strings.Contains(s, "no current session")
}
