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

// cmd runs tmux with -u. Without it tmux decides from LANG and LC_* whether
// its output may carry UTF-8, and where they name no UTF-8 locale - cron, a
// container, ssh without locale forwarding - it writes every non-ASCII
// character as '_': a Claude spinner glyph and a project name like "münchen"
// then stop matching anything.
func (h *Host) cmd(ctx context.Context, args ...string) *exec.Cmd {
	full := []string{"-u"}
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
		"#{pid}", "#{window_id}", "#{pane_id}", "#{pane_pid}", "#{pane_current_command}",
		"#{@" + varsOption + "}", "#{pane_title}",
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
	// list-panes -a lists a window's panes once for every session the window
	// is linked into - a session group, link-window - and a pane listed twice
	// is one agent, not two.
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 7)
		if len(f) != 7 {
			// A malformed line is skipped, never fatal. Panes belonging to
			// other tools share this server, and one odd line must not blank
			// every project revier knows about.
			continue
		}
		serverPID, winID, paneID, panePID, paneCmd, paneVars, paneTitle := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
		if seen[paneID] {
			continue
		}
		seen[paneID] = true

		inst, ok := byWindow[winID]
		if !ok {
			name := names[winID]
			inst = &revier.Instance{
				Ref:   revier.TargetRef{Host: h.Name(), ID: refID(serverPID, winID), Title: name},
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
			Vars:    parseVars(paneVars),
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

// refID is an instance id: the window id, behind the pid of the server that
// assigned it. tmux numbers windows per server from @0, so after a restart a
// bare window id names an unrelated window, and a binding kept in state would
// raise it. The core trusts a runtime binding without re-checking it
// (decisions.md D25), so the id has to stay unique across servers.
func refID(serverPID, window string) string { return serverPID + "/" + window }

// windowOf is the tmux window id an instance id carries.
func windowOf(id string) string { return id[strings.LastIndex(id, "/")+1:] }

// kindOf classifies a pane by its foreground command. The agent kind is what
// the survey probes; everything else is a shell or a tool.
// varsOption is the pane option a program leaves its panel variables in:
// `tmux set -p @revier 'NAME=value OTHER=value'`.
//
// kitty reports every user variable a pane set, because `kitten @ ls` carries
// them as a map. tmux has no such map: a format can name an option but cannot
// enumerate them, and asking pane by pane would be one call per pane, which is
// the cost rule this host exists to respect. So revier claims one option and
// the program writing it packs the pairs, which keeps this host free of any
// knowledge of which variables a probe reads.
const varsOption = "revier"

// parseVars reads space-separated NAME=value pairs. A value containing a space
// or the field separator cannot survive the round trip and is not supported;
// the variables revier reads are tokens.
func parseVars(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Fields(s) {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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
// neither exists yet. With panels, the first panel is the window's pane and
// every later one is split into it, side by side; without, the window runs
// r.Launch alone.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" {
		return revier.TargetRef{}, fmt.Errorf("tmux: realization has no name to give the window")
	}
	panels := r.Panels
	if len(panels) == 0 {
		panels = []revier.PanelSpec{{Command: r.Launch}}
	}

	format := "#{pid}" + sep + "#{window_id}" + sep + "#{pane_id}"
	args := []string{"new-window", "-P", "-F", format, "-n", literal(r.Name)}
	if !h.hasSession(ctx) {
		args = []string{"new-session", "-d", "-P", "-F", format, "-s", h.session(), "-n", literal(r.Name)}
	} else {
		args = append(args, "-t", h.session()+":")
	}
	if r.Dir != "" {
		args = append(args, "-c", literal(r.Dir))
	}
	args = append(args, command(panels[0].Command)...)

	out, err := h.run(ctx, args...)
	if err != nil {
		return revier.TargetRef{}, err
	}
	f := strings.SplitN(strings.TrimSpace(out), sep, 3)
	if len(f) != 3 {
		return revier.TargetRef{}, fmt.Errorf("tmux new-window: unexpected %q", out)
	}
	serverPID, window, pane := f[0], f[1], f[2]
	if err := h.title(ctx, pane, panels[0].Title); err != nil {
		return revier.TargetRef{}, err
	}

	for _, p := range panels[1:] {
		args := []string{"split-window", "-h", "-P", "-F", "#{pane_id}", "-t", window}
		if r.Dir != "" {
			args = append(args, "-c", literal(r.Dir))
		}
		args = append(args, command(p.Command)...)
		out, err := h.run(ctx, args...)
		if err != nil {
			return revier.TargetRef{}, err
		}
		if err := h.title(ctx, strings.TrimSpace(out), p.Title); err != nil {
			return revier.TargetRef{}, err
		}
	}
	if len(panels) > 1 {
		if _, err := h.run(ctx, "select-layout", "-t", window, "even-horizontal"); err != nil {
			return revier.TargetRef{}, err
		}
	}
	return revier.TargetRef{Host: h.Name(), ID: refID(serverPID, window), Title: r.Name}, nil
}

// literal escapes s for an argument tmux expands as a format, which -n, -c
// and -T all are. Unescaped, a project called "C#" names its window "C" and
// is never found again, and a directory holding a '#' starts the pane in
// $HOME. "##" is tmux's '#', except that tmux keeps a run of '#' directly
// before '[' as written, so that run is passed through unchanged. Checked on
// tmux 3.4 and 3.7.
func literal(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '#' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := i
		for end < len(s) && s[end] == '#' {
			end++
		}
		run := s[i:end]
		b.WriteString(run)
		if end == len(s) || s[end] != '[' {
			b.WriteString(run)
		}
		i = end
	}
	return b.String()
}

// command is a panel's argv as tmux has to be given it. tmux runs a single
// argument through default-shell -c as a shell string and execs only two or
// more directly, so a one-element argv breaks on a space, a '&' or a ';' in
// its path. env execs that one program, as every other host does.
func command(argv []string) []string {
	if len(argv) == 1 {
		return []string{"env", argv[0]}
	}
	return argv
}

// title sets a pane's initial title. The program inside may still repaint it,
// which is what the Claude probe reads.
func (h *Host) title(ctx context.Context, pane, title string) error {
	if title == "" {
		return nil
	}
	_, err := h.run(ctx, "select-pane", "-t", pane, "-T", literal(title))
	return err
}

func (h *Host) hasSession(ctx context.Context) bool {
	_, err := h.run(ctx, "has-session", "-t", h.session())
	return err == nil
}

// Focus selects the window. select-window works with no client attached, which
// is what makes the live test layer possible without a terminal.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	_, err := h.run(ctx, "select-window", "-t", windowOf(ref.ID))
	return err
}

// AttachCommand is the argv that puts the calling terminal onto the window.
// The window is the target: tmux attaches the session that holds it and
// makes it current, and instances are listed across the whole server, so a
// window matched in another session is reached the same way. It goes to the
// server this host speaks to, so a private socket is carried along. The
// caller execs it; nothing here takes over a terminal.
func (h *Host) AttachCommand(ref revier.TargetRef) ([]string, error) {
	if ref.Host != h.Name() || ref.ID == "" {
		return nil, fmt.Errorf("tmux: %s/%s is not a tmux window", ref.Host, ref.ID)
	}
	argv := []string{"tmux", "-u"}
	if h.Socket != "" {
		argv = append(argv, "-L", h.Socket)
	}
	return append(argv, "attach-session", "-t", windowOf(ref.ID)), nil
}

// SendText types text into a pane. -l sends it as characters, so no word of
// it is read as a key name, and "--" keeps a leading dash from reading as a
// flag. A pane id is unique across the server, so the instance is not needed.
func (h *Host) SendText(ctx context.Context, _ revier.TargetRef, panel revier.PanelID, text string) error {
	_, err := h.run(ctx, "send-keys", "-t", panel.String(), "-l", "--", text)
	return err
}

// Focused reports the session's current window.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	out, err := h.run(ctx, "display-message", "-p", "-t", h.session()+":",
		"#{pid}"+sep+"#{window_id}"+sep+"#{window_name}")
	if err != nil {
		if noServer(err) {
			return revier.TargetRef{}, nil
		}
		return revier.TargetRef{}, err
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), sep, 3)
	if len(f) != 3 {
		return revier.TargetRef{}, fmt.Errorf("tmux display-message: unexpected %q", out)
	}
	return revier.TargetRef{Host: h.Name(), ID: refID(f[0], f[1]), Title: f[2]}, nil
}

// noServer reports the "no server running" family of tmux errors, which mean
// "nothing is open" rather than "something went wrong".
func noServer(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting") ||
		strings.Contains(s, "no current session")
}
