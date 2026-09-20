// Package tmux implements a Runtime host over tmux.
//
// It exists first among the runtime adapters because it is the one that can be
// driven with no display at all: `tmux -L <socket>` starts a private server,
// and every query here works with no client attached. That makes it the
// substrate the live test layer runs against, and the reason most of revier
// can be verified without touching a desktop session. See docs/TESTING.md.
//
// An instance is a tmux session, the way a kitty instance is an OS window: its
// windows are the tabs, and the panes of every window are the panels a probe
// reads. A session of its own per instance is what lets two terminals attach
// two workspaces of one server without switching each other.
package tmux

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
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
// field absorb any separators it contains. That is why session names and pane
// titles are fetched by two separate calls rather than one: a single line
// cannot have two free-text fields at the end.
const sep = "|"

// nameOption is the session option that holds the name Open gave the
// session. The session name cannot carry it: tmux 3.4 turns ':' and '.' in a
// session name into '_', so "session:demo" would not be found by the match
// it was opened for. A session revier did not open has no such option and is
// known by its session name.
const nameOption = "@revier-name"

// titleFormat is the format of a session's name as an instance title.
const titleFormat = "#{?#{" + nameOption + "}," + "#{" + nameOption + "},#{session_name}}"

// focusOption is the server option Focus leaves the focused session in. With
// no client attached tmux has no current session of its own - it answers with
// the newest one - so on a server nobody is attached to, this is the answer
// Focused gives.
const focusOption = "@revier-focus"

// Host is a tmux Runtime. Socket selects a private tmux server; an empty
// Socket uses the user's default server.
type Host struct {
	Socket string
}

func (h *Host) Name() string { return "tmux" }

func (h *Host) Capabilities() revier.Capabilities {
	return revier.Capabilities{Layout: true, Persistent: true}
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

// Instances lists every session on the server, with the panes of all its
// windows.
//
// Two calls, both bulk: one for session names and one for panes. The cost is
// constant, not per project, which is the property the hot path actually needs.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	sessions, named, err := h.sessions(ctx)
	if err != nil || len(sessions) == 0 {
		return nil, err
	}

	// pane_title is free text and comes last, so SplitN gives it whatever it
	// contains.
	format := strings.Join([]string{
		"#{session_id}", "#{window_id}", "#{pane_id}", "#{pane_pid}", "#{pane_current_command}",
		"#{@" + varsOption + "}", "#{pane_title}",
	}, sep)

	out, err := h.run(ctx, "list-panes", "-a", "-F", format)
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}

	var lines [][]string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		// A malformed line is skipped, never fatal. Panes belonging to other
		// tools share this server, and one odd line must not blank every
		// project revier knows about.
		if f := strings.SplitN(line, sep, 7); len(f) == 7 {
			lines = append(lines, f)
		}
	}
	// list-panes -a lists a window's panes once for every session the window
	// is linked into - a session group, link-window - and a pane listed twice
	// is one agent, not two. It belongs to the session revier opened, else to
	// the oldest: a view grouped onto a workspace is listed first whenever its
	// name sorts first, and must not take the workspace's panes.
	owner := map[string]string{}
	for _, f := range lines {
		session, pane := f[0], f[2]
		if cur, ok := owner[pane]; !ok || owns(named, session, cur) {
			owner[pane] = session
		}
	}

	bySession := map[string]*revier.Instance{}
	for i := range sessions {
		bySession[sessionOf(sessions[i].Ref.ID)] = &sessions[i]
	}
	for _, f := range lines {
		sessionID, windowID, paneID, panePID, paneCmd, paneVars, paneTitle := f[0], f[1], f[2], f[3], f[4], f[5], f[6]
		inst, ok := bySession[sessionID]
		if !ok || owner[paneID] != sessionID {
			continue
		}
		pid, _ := strconv.Atoi(panePID)
		inst.Panels = append(inst.Panels, revier.Panel{
			ID:      revier.PanelID(paneID),
			Kind:    kindOf(paneCmd),
			Title:   paneTitle,
			Vars:    parseVars(paneVars),
			PID:     pid,
			Command: []string{paneCmd},
			Tab:     windowID,
		})
	}
	return sessions, nil
}

// owns reports whether session a, rather than b, owns a pane both list: the
// one revier named, and between two alike the older, by its lower id.
func owns(named map[string]bool, a, b string) bool {
	if named[a] != named[b] {
		return named[a]
	}
	na, _ := strconv.Atoi(strings.TrimPrefix(a, "$"))
	nb, _ := strconv.Atoi(strings.TrimPrefix(b, "$"))
	return na < nb
}

// sessions lists every session as an instance with no panels yet, and which
// sessions revier named. The title is free text, so it is the last field of
// its own query.
func (h *Host) sessions(ctx context.Context) ([]revier.Instance, map[string]bool, error) {
	format := "#{pid}" + sep + "#{session_id}" + sep + "#{?#{" + nameOption + "},1,0}" + sep + titleFormat
	out, err := h.run(ctx, "list-sessions", "-F", format)
	if err != nil {
		if noServer(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var instances []revier.Instance
	named := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.SplitN(line, sep, 4)
		if len(f) != 4 {
			continue
		}
		named[f[1]] = f[2] == "1"
		instances = append(instances, revier.Instance{
			Ref:   revier.TargetRef{Host: h.Name(), ID: refID(f[0], f[1]), Title: f[3]},
			Title: f[3],
		})
	}
	return instances, named, nil
}

// refID is an instance id: the session id, behind the pid of the server that
// assigned it. tmux numbers sessions per server from $0, so after a restart a
// bare session id names an unrelated session, and a binding kept in state
// would raise it. The core trusts a runtime binding without re-checking it
// (decisions.md D25), so the id has to stay unique across servers.
func refID(serverPID, session string) string { return serverPID + "/" + session }

// sessionOf is the tmux session id an instance id carries.
func sessionOf(id string) string { return id[strings.LastIndex(id, "/")+1:] }

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

// Open creates a session for the realization, starting the server when none
// runs yet, and leaves r.Name on it where Instances reads the title. With
// panels, the first panel is the session's first pane and every later one is
// split into it, side by side; without, the session runs r.Launch alone.
// r.Vars go into the first pane's @revier option, which Instances reports
// back as the panel's Vars, as OpenTab's do for a tab.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" {
		return revier.TargetRef{}, fmt.Errorf("tmux: realization has no name to give the session")
	}
	panels := r.PanelSpecs()

	out, err := h.newSession(ctx, r.Name, panels[0])
	if err != nil {
		return revier.TargetRef{}, err
	}
	f := strings.SplitN(strings.TrimSpace(out), sep, 4)
	if len(f) != 4 {
		if len(f) > 1 && strings.HasPrefix(f[1], "$") {
			_, _ = h.run(ctx, "kill-session", "-t", f[1])
		}
		return revier.TargetRef{}, fmt.Errorf("tmux new-session: unexpected %q", out)
	}
	serverPID, session, window, pane := f[0], f[1], f[2], f[3]
	if err := h.name(ctx, session, window, pane, r, panels); err != nil {
		// Best effort: the error that failed the session is the one worth
		// reporting. Left running, a session with no name or no shell would be
		// matched, or would hold its name against the next Open.
		_, _ = h.run(ctx, "kill-session", "-t", session)
		return revier.TargetRef{}, err
	}
	return revier.TargetRef{Host: h.Name(), ID: refID(serverPID, session), Title: r.Name}, nil
}

// newSession starts a detached session running the panel, named after the
// realization. The session name is only what a terminal's status line shows -
// @revier-name is the identity - so a name another session already has, which
// tmux 3.4 makes of "a:b" and "a.b" alike, is left to tmux to choose.
func (h *Host) newSession(ctx context.Context, name string, first revier.PanelSpec) (string, error) {
	format := "#{pid}" + sep + "#{session_id}" + sep + "#{window_id}" + sep + "#{pane_id}"
	args := []string{"new-session", "-d", "-P", "-F", format}
	out, err := h.run(ctx, append(append(args, "-s", literal(name)), start(first)...)...)
	if err != nil && strings.Contains(err.Error(), "duplicate session") {
		return h.run(ctx, append(args, start(first)...)...)
	}
	return out, err
}

// name leaves the realization's name on a new session and fills its window.
func (h *Host) name(ctx context.Context, session, window, pane string, r revier.Realization, panels []revier.PanelSpec) error {
	if _, err := h.run(ctx, "set-option", "-t", session, nameOption, r.Name); err != nil {
		return err
	}
	if err := h.setVars(ctx, pane, r.Vars); err != nil {
		// The mark is best effort: an unmarked panel falls back to the guess
		// it replaces (decisions.md D100). Failing here would kill a session
		// whose panes are already running, over a mark nothing depends on.
		slog.Warn("panel mark", "host", h.Name(), "name", r.Name, "panel", pane, "err", err)
	}
	return h.fill(ctx, window, pane, panels)
}

// start is the part of a new-session, new-window or split-window that starts
// a panel: its directory and its command.
func start(p revier.PanelSpec) []string {
	var args []string
	if p.Dir != "" {
		args = append(args, "-c", literal(p.Dir))
	}
	return append(args, command(p.Command)...)
}

// fill titles the window's first pane and splits every later panel into the
// window beside it.
func (h *Host) fill(ctx context.Context, window, first string, panels []revier.PanelSpec) error {
	if err := h.title(ctx, first, panels[0].Title); err != nil {
		return err
	}
	for _, p := range panels[1:] {
		args := append([]string{"split-window", "-h", "-P", "-F", "#{pane_id}", "-t", window}, start(p)...)
		out, err := h.run(ctx, args...)
		if err != nil {
			return err
		}
		if err := h.title(ctx, strings.TrimSpace(out), p.Title); err != nil {
			return err
		}
	}
	if len(panels) > 1 {
		if _, err := h.run(ctx, "select-layout", "-t", window, "even-horizontal"); err != nil {
			return err
		}
	}
	return nil
}

// OpenTab opens a window after the last one of the session: r.Launch alone,
// or r.Panels with every later one split into the first. It goes last, not
// into the lowest free index, so the panels keep their order in the listing a
// save records agents by. The vars go into the first pane's @revier option,
// which Instances reports back as the panel's Vars. The window opens without
// becoming current; the core focuses the panel it wants.
//
// A tab that fails is killed whole: the core names its agent as not added,
// and it must not be running without its shell.
func (h *Host) OpenTab(ctx context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	if err := h.own(ref); err != nil {
		return "", err
	}
	panels := r.PanelSpecs()
	args := []string{"new-window", "-d", "-a", "-P", "-F", "#{window_id}" + sep + "#{pane_id}", "-t", sessionOf(ref.ID) + ":{end}"}
	out, err := h.run(ctx, append(args, start(panels[0])...)...)
	if err != nil {
		return "", err
	}
	f := strings.SplitN(strings.TrimSpace(out), sep, 2)
	window := f[0]
	if len(f) != 2 {
		if strings.HasPrefix(window, "@") {
			_, _ = h.run(ctx, "kill-window", "-t", window)
		}
		return "", fmt.Errorf("tmux new-window: unexpected %q", out)
	}
	pane := f[1]
	if err := h.tab(ctx, window, pane, panels, vars); err != nil {
		// Best effort: the error that failed the tab is the one worth reporting.
		_, _ = h.run(ctx, "kill-window", "-t", window)
		return "", err
	}
	return revier.PanelID(pane), nil
}

func (h *Host) tab(ctx context.Context, window, pane string, panels []revier.PanelSpec, vars map[string]string) error {
	// A tab's mark is its whole identity (decisions.md D64): a tab that
	// carries none is found by nothing, and the next press opens another. It
	// fails the tab, which OpenTab then kills.
	if err := h.setVars(ctx, pane, vars); err != nil {
		return err
	}
	return h.fill(ctx, window, pane, panels)
}

// setVars leaves the variables on the pane, packed into its @revier option,
// where Instances reads them back.
func (h *Host) setVars(ctx context.Context, pane string, vars map[string]string) error {
	if len(vars) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(vars))
	for name, value := range vars {
		pairs = append(pairs, name+"="+value)
	}
	sort.Strings(pairs)
	_, err := h.run(ctx, "set-option", "-p", "-t", pane, "@"+varsOption, strings.Join(pairs, " "))
	return err
}

// FocusPanel makes the pane current in its window and its window current in
// the instance's session. The window is named inside that session: a pane
// alone names a window of whichever session tmux resolves it to, which in a
// session group need not be this one. Which session a terminal shows is
// Focus's.
func (h *Host) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	if err := h.own(ref); err != nil {
		return err
	}
	out, err := h.run(ctx, "display-message", "-p", "-t", panel.String(), "#{window_id}")
	if err != nil {
		return err
	}
	if _, err := h.run(ctx, "select-window", "-t", sessionOf(ref.ID)+":"+strings.TrimSpace(out)); err != nil {
		return err
	}
	_, err = h.run(ctx, "select-pane", "-t", panel.String())
	return err
}

// FocusedPanel reports the current pane of the session's current window.
func (h *Host) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	if err := h.own(ref); err != nil {
		return "", err
	}
	out, err := h.run(ctx, "display-message", "-p", "-t", sessionOf(ref.ID)+":", "#{pane_id}")
	if err != nil {
		return "", err
	}
	return revier.PanelID(strings.TrimSpace(out)), nil
}

// own refuses a ref this host did not produce.
func (h *Host) own(ref revier.TargetRef) error {
	if ref.Host != h.Name() || ref.ID == "" {
		return fmt.Errorf("tmux: %s/%s is not a tmux session", ref.Host, ref.ID)
	}
	return nil
}

// literal escapes s for an argument tmux expands as a format, which -c and -T
// both are. Unescaped, a pane titled "C#" is titled "C", and a directory
// holding a '#' starts the pane in $HOME. "##" is tmux's '#', except that tmux
// keeps a run of '#' directly before '[' as written, so that run is passed
// through unchanged. Checked on tmux 3.4 and 3.7.
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

// Focus makes the session the focused one, recorded in the server option, and
// switches the terminal that shows it:
//
//   - run in a pane of this server - a key pressed there - the terminal
//     attached to that pane's session, and none when nobody is attached to it;
//   - run from outside, the one terminal attached to the server, and none
//     when there are several, since which of them is not tmux's to know.
//
// A terminal is named with -c: without it tmux picks one by its own rules,
// which moves a terminal the command has nothing to do with.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	if err := h.own(ref); err != nil {
		return err
	}
	session := sessionOf(ref.ID)
	if _, err := h.run(ctx, "set-option", "-s", focusOption, session); err != nil {
		return err
	}
	client, err := h.terminal(ctx)
	if err != nil || client == "" {
		return err
	}
	_, err = h.run(ctx, "switch-client", "-c", client, "-t", session)
	return err
}

// client is one terminal attached to the server, and the session it shows.
type client struct {
	name, session string
	activity      int
}

// clients lists the terminals attached to the server, the most recently
// active first. client_name is a tty path, free text, so it comes last.
func (h *Host) clients(ctx context.Context) ([]client, error) {
	out, err := h.run(ctx, "list-clients", "-F", "#{client_activity}"+sep+"#{session_id}"+sep+"#{client_name}")
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var clients []client
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.SplitN(line, sep, 3)
		if len(f) != 3 {
			continue
		}
		activity, _ := strconv.Atoi(f[0])
		clients = append(clients, client{name: f[2], session: f[1], activity: activity})
	}
	sort.SliceStable(clients, func(i, j int) bool { return clients[i].activity > clients[j].activity })
	return clients, nil
}

// terminal is the client Focus switches, as Focus describes, or "" for none.
func (h *Host) terminal(ctx context.Context) (string, error) {
	clients, err := h.clients(ctx)
	if err != nil || len(clients) == 0 {
		return "", err
	}
	pane, err := h.callerPane(ctx)
	if err != nil {
		return "", err
	}
	if pane == "" {
		if len(clients) == 1 {
			return clients[0].name, nil
		}
		return "", nil
	}
	out, err := h.run(ctx, "display-message", "-p", "-t", pane, "#{session_id}")
	if err != nil {
		return "", err
	}
	for _, c := range clients {
		if c.session == strings.TrimSpace(out) {
			return c.name, nil
		}
	}
	return "", nil
}

// callerPane is the pane this process runs in when that pane is on this
// host's server, and "" otherwise: $TMUX names the socket of the server the
// pane belongs to, and $TMUX_PANE the pane.
func (h *Host) callerPane(ctx context.Context) (string, error) {
	socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
	pane := os.Getenv("TMUX_PANE")
	if socket == "" || pane == "" {
		return "", nil
	}
	out, err := h.run(ctx, "display-message", "-p", "#{socket_path}")
	if err != nil || strings.TrimSpace(out) != socket {
		return "", err
	}
	return pane, nil
}

// Focused reports the session the one attached terminal shows. With none, or
// with several, it is the session Focus recorded, and with no record a zero
// ref.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	session, err := h.focusedSession(ctx)
	if err != nil || session == "" {
		return revier.TargetRef{}, err
	}
	out, err := h.run(ctx, "display-message", "-p", "-t", session, "#{pid}"+sep+"#{session_id}"+sep+titleFormat)
	if err != nil {
		if noServer(err) || strings.Contains(err.Error(), "can't find session") {
			return revier.TargetRef{}, nil
		}
		return revier.TargetRef{}, err
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), sep, 3)
	if len(f) != 3 {
		return revier.TargetRef{}, fmt.Errorf("tmux display-message: unexpected %q", out)
	}
	// A session that has closed since the focus was recorded is answered
	// with empty fields on tmux 3.7, not with an error.
	if f[1] != session {
		return revier.TargetRef{}, nil
	}
	return revier.TargetRef{Host: h.Name(), ID: refID(f[0], f[1]), Title: f[2]}, nil
}

func (h *Host) focusedSession(ctx context.Context) (string, error) {
	clients, err := h.clients(ctx)
	if err != nil {
		return "", err
	}
	if len(clients) == 1 {
		return clients[0].session, nil
	}
	out, err := h.run(ctx, "show-options", "-s", "-v", "-q", focusOption)
	if err != nil {
		if noServer(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// AttachCommand is the argv that puts the calling terminal onto the session.
// It goes to the server this host speaks to, so a private socket is carried
// along. The caller execs it; nothing here takes over a terminal.
func (h *Host) AttachCommand(ref revier.TargetRef) ([]string, error) {
	if err := h.own(ref); err != nil {
		return nil, err
	}
	argv := []string{"tmux", "-u"}
	if h.Socket != "" {
		argv = append(argv, "-L", h.Socket)
	}
	return append(argv, "attach-session", "-t", sessionOf(ref.ID)), nil
}

// SendText types text into a pane. -l sends it as characters, so no word of
// it is read as a key name, and "--" keeps a leading dash from reading as a
// flag. A pane id is unique across the server, so the instance is not needed.
func (h *Host) SendText(ctx context.Context, _ revier.TargetRef, panel revier.PanelID, text string) error {
	_, err := h.run(ctx, "send-keys", "-t", panel.String(), "-l", "--", text)
	return err
}

// Close kills the session and every pane in it. The server that assigned the
// id has to be the server answering now: tmux numbers sessions per server
// from $0, so after a restart the same id names an unrelated session, and a
// kill is not an operation to make on the wrong one.
func (h *Host) Close(ctx context.Context, ref revier.TargetRef) error {
	if err := h.own(ref); err != nil {
		return err
	}
	session := sessionOf(ref.ID)
	out, err := h.run(ctx, "display-message", "-p", "-t", session, "#{pid}"+sep+"#{session_id}")
	if err != nil {
		return err
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), sep, 2)
	if len(f) != 2 || refID(f[0], f[1]) != ref.ID {
		return fmt.Errorf("tmux: session %s is gone; %q answers for it now", ref.ID, strings.TrimRight(out, "\n"))
	}
	_, err = h.run(ctx, "kill-session", "-t", session)
	return err
}

// ClosePanel kills one pane. A pane id is unique across the server, so the
// instance is not needed; a window left with no pane goes with it.
func (h *Host) ClosePanel(ctx context.Context, _ revier.TargetRef, panel revier.PanelID) error {
	_, err := h.run(ctx, "kill-pane", "-t", panel.String())
	return err
}

// noServer reports the "no server running" family of tmux errors, which mean
// "nothing is open" rather than "something went wrong".
func noServer(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting") ||
		strings.Contains(s, "no current session")
}
