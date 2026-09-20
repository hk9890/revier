// Package kitty implements a Runtime host over kitty's remote control.
//
// kitty is the terminal this machine actually uses; tmux is the runtime host
// that exists for headless testing. An instance is a kitty OS window, and the
// kitty windows inside it, across all of its tabs, are the panels a probe
// reads.
//
// Three facts about kitty shape this adapter, all verified on kitty 0.48:
//
//   - `kitten @ ls` reports no OS window title. The identity revier gives an OS
//     window is therefore its WM instance name (`--os-window-name`, read back
//     as `wm_name`), and the same value is set as the OS window title so a
//     window host sees it too. A window kitty opened from a session file
//     carries the default name "kitty" and is not recognisable to revier.
//   - Each kitty process listens on its own socket, and with the documented
//     `listen_on unix:@kitty` setting that socket is `@kitty-<pid>`. The
//     windows revier cares about may span several processes - the shell
//     implementation started one per session - so Instances queries every
//     `@kitty-<pid>` socket, concurrently. That is one call per kitty process,
//     which does not grow with the project count.
//   - `kitten @ focus-window` focuses inside kitty but does not raise the OS
//     window under GNOME on Wayland: the compositor refuses a focus request
//     that did not come from user input. Raising is the window host's job,
//     which the core arranges because Capabilities reports OSWindows.
//
// Layout is built with `kitten @ launch` sequences, never `kitty --session`:
// a session file handed to a running kitty starts a new process, not a new
// window. The "Sessions don't start when kitty already running" entry in the
// shell implementation's README records the day that cost.
package kitty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// socketPattern is the socket kitty creates for `listen_on unix:@kitty`, and
// the one this host asks for when it starts kitty itself.
var socketPattern = regexp.MustCompile(`^@kitty-\d+$`)

// Host is a kitty Runtime. The zero value uses the real kitten binary and
// discovers sockets; tests inject recorders through export_test.go.
type Host struct {
	run       func(ctx context.Context, socket, stdin string, args ...string) ([]byte, error)
	sockets   func() []string
	start     func(ctx context.Context, args ...string) error
	ownSocket func() string
	parent    func(pid int) (int, bool)
}

func (h *Host) Name() string { return "kitty" }

func (h *Host) Capabilities() revier.Capabilities {
	return revier.Capabilities{Layout: true, Persistent: false, OSWindows: true}
}

// The subset of `kitten @ ls` this host reads.
type osWindow struct {
	ID        int    `json:"id"`
	WMName    string `json:"wm_name"`
	WMClass   string `json:"wm_class"`
	IsFocused bool   `json:"is_focused"`
	Tabs      []tab  `json:"tabs"`
}

type tab struct {
	ID       int      `json:"id"`
	IsActive bool     `json:"is_active"`
	Windows  []window `json:"windows"`
}

type window struct {
	ID         int               `json:"id"`
	PID        int               `json:"pid"`
	Title      string            `json:"title"`
	IsActive   bool              `json:"is_active"`
	UserVars   map[string]string `json:"user_vars"`
	Foreground []process         `json:"foreground_processes"`
}

type process struct {
	PID     int      `json:"pid"`
	Cmdline []string `json:"cmdline"`
}

// listing is one socket's answer to ls.
type listing struct {
	socket  string
	windows []osWindow
}

// Probe reports kitty usable when kitten can be found and either a control
// socket answers or no kitty is running at all, in which case Open starts one.
// A kitty running without remote control has no socket and looks like no
// kitty; Open then starts a second process, which is the best that can be done
// without the setting.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := kittenPath(); err != nil {
		return err
	}
	socks := h.socketList()
	if len(socks) == 0 {
		if _, err := exec.LookPath("kitty"); err != nil {
			return fmt.Errorf("kitty not on PATH: %w", err)
		}
		return nil
	}
	var errs []error
	for _, s := range socks {
		if _, err := h.kitten(ctx, s, "ls"); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}
	return fmt.Errorf("kitty is running but no control socket answers: %w", errors.Join(errs...))
}

// kittenPath finds the kitten binary. Since kitty 0.44 it is not always on
// PATH, but it always sits next to kitty.
func kittenPath() (string, error) {
	if p, err := exec.LookPath("kitten"); err == nil {
		return p, nil
	}
	kitty, err := exec.LookPath("kitty")
	if err != nil {
		return "", fmt.Errorf("kitten not on PATH and kitty not on PATH: %w", err)
	}
	if real, err := filepath.EvalSymlinks(kitty); err == nil {
		kitty = real
	}
	p := filepath.Join(filepath.Dir(kitty), "kitten")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("kitten not on PATH and not beside kitty at %s", p)
	}
	return p, nil
}

func (h *Host) kitten(ctx context.Context, socket string, args ...string) ([]byte, error) {
	return h.kittenIn(ctx, socket, "", args...)
}

// kittenIn is kitten with stdin, for the one command that reads it.
func (h *Host) kittenIn(ctx context.Context, socket, stdin string, args ...string) ([]byte, error) {
	if h.run != nil {
		return h.run(ctx, socket, stdin, args...)
	}
	bin, err := kittenPath()
	if err != nil {
		return nil, err
	}
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, bin, append([]string{"@", "--to", socket}, args...)...)
	c.Stdout, c.Stderr = &out, &errb
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("kitten @ --to %s %s: %w: %s", socket, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// ls lists one socket's OS windows.
func (h *Host) ls(ctx context.Context, socket string) ([]osWindow, error) {
	raw, err := h.kitten(ctx, socket, "ls")
	if err != nil {
		return nil, err
	}
	var windows []osWindow
	if err := json.Unmarshal(raw, &windows); err != nil {
		return nil, fmt.Errorf("kitten @ --to %s ls: %w", socket, err)
	}
	return windows, nil
}

// launch runs `kitten @ launch` and returns the id of the window it made.
func (h *Host) launch(ctx context.Context, socket string, args ...string) (int, error) {
	out, err := h.kitten(ctx, socket, append([]string{"launch"}, args...)...)
	if err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("kitty: launch reported %q, not a window id", strings.TrimSpace(string(out)))
	}
	return id, nil
}

// FindPanel reports the OS window that holds a kitty window id. A window id is
// one kitty process's, and KITTY_LISTEN_ON names the kitty a command runs in:
// a window's environment carries it, and a key's background launch gets it
// with --copy-env (verified on kitty 0.48; without it the variable is empty).
// A command started outside kitty, or from a kitty that has exited, names no
// process that answers; then the id must be held in exactly one kitty.
func (h *Host) FindPanel(instances []revier.Instance, panel revier.PanelID) (revier.TargetRef, error) {
	own := "unix:" + strings.TrimPrefix(h.here(), "unix:")
	var mine []revier.Instance
	for _, inst := range instances {
		if socket, _, err := parseRef(inst.Ref.ID); err == nil && socket == own {
			mine = append(mine, inst)
		}
	}
	if len(mine) > 0 {
		instances = mine
	}
	var found []revier.TargetRef
	for _, inst := range instances {
		for _, p := range inst.Panels {
			if p.ID == panel {
				found = append(found, inst.Ref)
			}
		}
	}
	switch len(found) {
	case 0:
		return revier.TargetRef{}, nil
	case 1:
		return found[0], nil
	}
	return revier.TargetRef{}, fmt.Errorf("kitty: window %s is in %d kitty processes; run the command from inside the kitty you mean", panel, len(found))
}

// here is the control socket of the kitty this process was started from.
func (h *Host) here() string {
	if h.ownSocket != nil {
		return h.ownSocket()
	}
	return os.Getenv("KITTY_LISTEN_ON")
}

func (h *Host) socketList() []string {
	if h.sockets != nil {
		return h.sockets()
	}
	return discover()
}

// discover lists the control sockets to query: the one this process was
// started inside, first, then every `@kitty-<pid>` abstract socket the kernel
// knows about. KITTY_LISTEN_ON comes first so a launch from inside kitty lands
// in that same process.
func discover() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, "unix:"+name)
	}
	add(strings.TrimPrefix(os.Getenv("KITTY_LISTEN_ON"), "unix:"))

	var found []string
	if b, err := os.ReadFile("/proc/net/unix"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 8 {
				continue
			}
			if name := f[len(f)-1]; socketPattern.MatchString(name) && !seen[name] {
				found = append(found, name)
			}
		}
	}
	sort.Strings(found)
	for _, name := range found {
		add(name)
	}
	return out
}

// list queries every socket concurrently. A socket that does not answer is a
// kitty that is exiting, or a stale kernel entry, and holds nothing revier can
// reach, so it is skipped; only every socket failing is reported.
func (h *Host) list(ctx context.Context) ([]listing, error) {
	socks := h.socketList()
	results := make([]listing, len(socks))
	errs := make([]error, len(socks))
	var wg sync.WaitGroup
	for i, s := range socks {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			windows, err := h.ls(ctx, s)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = listing{socket: s, windows: windows}
		}(i, s)
	}
	wg.Wait()

	out := make([]listing, 0, len(socks))
	var failed []error
	for i := range socks {
		if errs[i] != nil {
			failed = append(failed, errs[i])
			continue
		}
		out = append(out, results[i])
	}
	if len(out) == 0 && len(failed) > 0 {
		return nil, errors.Join(failed...)
	}
	return out, nil
}

// Instances lists every OS window of every kitty process, panels included.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	listings, err := h.list(ctx)
	if err != nil {
		return nil, err
	}
	var out []revier.Instance
	for _, l := range listings {
		out = append(out, h.decode(l)...)
	}
	return out, nil
}

// decode turns one socket's listing into instances. The instance id carries
// the socket, because OS window ids are per process and Focus must find its
// way back.
func (h *Host) decode(l listing) []revier.Instance {
	pid := pidOf(l.socket)
	parent := h.parents()
	out := make([]revier.Instance, 0, len(l.windows))
	for _, w := range l.windows {
		// An OS window kitty opened for itself carries the default instance
		// name, which identifies nothing: every such window has it. Reporting
		// it as no title at all is the fact the core needs to know that this
		// window has to be identified some other way (decisions.md D22).
		name := w.WMName
		if name == defaultWMName {
			name = ""
		}
		inst := revier.Instance{
			Ref:   revier.TargetRef{Host: "kitty", ID: refID(l.socket, w.ID), Title: name},
			Title: name,
			Class: w.WMClass,
			PID:   pid,
		}
		for _, t := range w.Tabs {
			// A tab the listing gives no id names no tab. Reporting "0" for
			// each of them would make the whole OS window one tab, so a tab
			// target's close would hold every agent in the window and never
			// count as closed.
			tab := ""
			if t.ID != 0 {
				tab = strconv.Itoa(t.ID)
			}
			for _, win := range t.Windows {
				panel := panelOf(win, parent)
				panel.Tab = tab
				inst.Panels = append(inst.Panels, panel)
			}
		}
		out = append(out, inst)
	}
	return out
}

// defaultWMName is what kitty calls an OS window that was given no name of
// its own: `kitty`, the same as its class. Verified with `kitten @ ls` against
// a window the shell session tool opened.
const defaultWMName = "kitty"

func refID(socket string, id int) string {
	return strings.TrimPrefix(socket, "unix:") + "/" + strconv.Itoa(id)
}

// parseRef splits an instance id back into its socket and OS window id.
func parseRef(id string) (string, int, error) {
	i := strings.LastIndex(id, "/")
	if i < 0 {
		return "", 0, fmt.Errorf("kitty: malformed ref %q", id)
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("kitty: malformed ref %q", id)
	}
	return "unix:" + id[:i], n, nil
}

// pidOf reads the kitty process id out of its socket name, which is the only
// place ls does not report it. A window host reports the same pid for every OS
// window of that process; it is a filter, not an identity.
func pidOf(socket string) int {
	name := strings.TrimPrefix(socket, "unix:")
	if !socketPattern.MatchString(name) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(name, "@kitty-"))
	return n
}

func panelOf(w window, parent func(pid int) (int, bool)) revier.Panel {
	kind, cmd, pid := classify(w.Foreground, w.PID, parent)
	if pid == 0 {
		pid = w.PID
	}
	return revier.Panel{
		ID:      revier.PanelID(strconv.Itoa(w.ID)),
		Kind:    kind,
		Title:   w.Title,
		Vars:    w.UserVars,
		PID:     pid,
		Command: cmd,
	}
}

// classify reads the foreground process group of a window whose own process
// is root. The panel's command is the program nearest root that is neither
// kitty's `run-shell` wrapper nor a shell: a `--hold` window wraps its command
// in both, and a program the panel runs stays the command while it runs a
// tool of its own. Whether that program is an agent is the probes' to say, so
// no harness is named here - a probe declared in config is for one this
// adapter was not written with. With nothing but wrappers and shells, the
// panel is a shell.
//
// Nearness is by parent, not by kitty's order. kitty lists the group by pid,
// and a pid says nothing about who started whom once pids wrap. A process
// that has left root's tree is not the panel's at all: `wl-copy`, which Claude
// Code starts to hold the clipboard, forks, lets its parent exit, and stays in
// the group, often with a lower pid than the agent. Taken as the command, it
// hid the agent from every probe. A process whose parents cannot be read -
// gone between the listing and the read - keeps kitty's order, after the ones
// placed in the tree.
func classify(fg []process, root int, parent func(pid int) (int, bool)) (revier.PanelKind, []string, int) {
	var own []process
	best, bestRank := -1, 0
	for _, p := range fg {
		r := rank(p.PID, root, parent)
		if r == detached {
			continue
		}
		own = append(own, p)
		if len(p.Cmdline) == 0 || isRunShell(p.Cmdline) || isShell(base(p.Cmdline[0])) {
			continue
		}
		if best < 0 || r < bestRank {
			best, bestRank = len(own)-1, r
		}
	}
	if best >= 0 {
		return revier.PanelTool, own[best].Cmdline, own[best].PID
	}
	if len(own) == 0 {
		return revier.PanelShell, nil, 0
	}
	last := own[len(own)-1]
	return revier.PanelShell, last.Cmdline, last.PID
}

// Ranks of a process that is not placed by its depth under the window's root.
const (
	unknown  = 1 << 20 // its parents could not be read: after every placed one
	detached = -1      // its parents lead somewhere else: not the panel's
)

// rank is how many parents up from pid the window's root is, unknown when a
// parent cannot be read, and detached when the chain ends without reaching
// root - at init or a subreaper.
func rank(pid, root int, parent func(int) (int, bool)) int {
	for depth := 0; depth < 64; depth++ {
		if pid == root {
			return depth
		}
		next, ok := parent(pid)
		if !ok {
			return unknown
		}
		if next <= 1 {
			return detached
		}
		pid = next
	}
	return detached
}

// parents is parentOf remembered for one listing. Every foreground process of
// every window is walked up to its window's root, and the shells and
// wrappers those walks share would otherwise be read from /proc again for
// each one.
func (h *Host) parents() func(pid int) (int, bool) {
	type answer struct {
		ppid int
		ok   bool
	}
	seen := map[int]answer{}
	return func(pid int) (int, bool) {
		if a, ok := seen[pid]; ok {
			return a.ppid, a.ok
		}
		ppid, ok := h.parentOf(pid)
		seen[pid] = answer{ppid, ok}
		return ppid, ok
	}
}

// parentOf reads a process's parent from /proc. kitty is a host on this
// machine, so its pids are this machine's.
func (h *Host) parentOf(pid int) (int, bool) {
	if h.parent != nil {
		return h.parent(pid)
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// The command name in parentheses may hold spaces and parentheses of its
	// own; the fields after its last ')' are fixed: state, then ppid.
	rest := stat[bytes.LastIndexByte(stat, ')')+1:]
	fields := strings.Fields(string(rest))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

func base(arg string) string { return arg[strings.LastIndex(arg, "/")+1:] }

func isRunShell(cmdline []string) bool {
	return base(cmdline[0]) == "kitten" && len(cmdline) > 1 && cmdline[1] == "run-shell"
}

func isShell(cmd string) bool {
	switch cmd {
	case "bash", "zsh", "sh", "fish":
		return true
	}
	return false
}

// Open creates an OS window named r.Name holding r.Panels, or r.Launch alone
// when there are no panels, as a `kitten @ launch` sequence: the first panel
// opens the OS window and every later one splits into its first tab. With no
// kitty answering it starts one, on a socket that discovery finds again.
// r.Vars become user vars of that first window, which ls reports back as the
// panel's Vars, as OpenTab's do for a tab.
//
// A launch's --match selects a tab, so the OS window is named by the first
// panel's window as window_id; id would be a tab id, which equals the window
// id only in a kitty that has opened nothing else.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" {
		return revier.TargetRef{}, fmt.Errorf("kitty: realization has no name to give the OS window")
	}
	panels := r.PanelSpecs()

	socket, first, err := h.openFirst(ctx, r, panels[0])
	if err != nil {
		return revier.TargetRef{}, err
	}
	if err := h.title(ctx, socket, first, panels[0].Title); err != nil {
		return revier.TargetRef{}, err
	}
	if err := h.setVars(ctx, socket, first, r.Vars); err != nil {
		return revier.TargetRef{}, err
	}
	for _, p := range panels[1:] {
		args := []string{"--type=window", "--match", "window_id:" + strconv.Itoa(first), "--hold"}
		if p.Dir != "" {
			args = append(args, "--cwd", p.Dir)
		}
		args = append(args, p.Command...)
		id, err := h.launch(ctx, socket, args...)
		if err != nil {
			return revier.TargetRef{}, err
		}
		if err := h.title(ctx, socket, id, p.Title); err != nil {
			return revier.TargetRef{}, err
		}
	}
	// The launch reports a window id; the instance is the OS window around it.
	windows, err := h.ls(ctx, socket)
	if err != nil {
		return revier.TargetRef{}, err
	}
	for _, w := range windows {
		for _, t := range w.Tabs {
			for _, win := range t.Windows {
				if win.ID == first {
					return revier.TargetRef{Host: h.Name(), ID: refID(socket, w.ID), Title: r.Name}, nil
				}
			}
		}
	}
	return revier.TargetRef{}, fmt.Errorf("kitty: launched window %d is not in any OS window", first)
}

// setVars marks a window kitty has already opened with its user vars. A
// launch takes them as --var, but the kitty process started for the first
// window of a new OS window takes no such option, so both paths set them
// here, in one call per variable.
func (h *Host) setVars(ctx context.Context, socket string, id int, vars map[string]string) error {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := h.kitten(ctx, socket, "set-user-vars", "--match", "id:"+strconv.Itoa(id), name+"="+vars[name]); err != nil {
			return err
		}
	}
	return nil
}

// title gives a new window its panel title without taking the title away from
// the program inside. `launch --title` pins a title for good, and a pinned
// title is exactly what the Claude probe must not see: it reads the live title
// the agent keeps repainting.
func (h *Host) title(ctx context.Context, socket string, id int, title string) error {
	if title == "" {
		return nil
	}
	_, err := h.kitten(ctx, socket, "set-window-title", "--temporary", "--match", "id:"+strconv.Itoa(id), title)
	return err
}

// openFirst opens the OS window with its first panel and returns the socket it
// lives on and the id of that panel's window.
func (h *Host) openFirst(ctx context.Context, r revier.Realization, p revier.PanelSpec) (string, int, error) {
	if socket, ok := h.liveSocket(ctx); ok {
		// The class is set explicitly so the window is a kitty window whatever
		// process it lands in: a kitty started as `--class revier-popup` would
		// otherwise pass that class, and its window rule, on to the workspace.
		args := []string{"--type=os-window",
			"--os-window-name", r.Name, "--os-window-title", r.Name, "--os-window-class", "kitty", "--hold"}
		if p.Dir != "" {
			args = append(args, "--cwd", p.Dir)
		}
		args = append(args, p.Command...)
		id, err := h.launch(ctx, socket, args...)
		if err != nil {
			return "", 0, err
		}
		return socket, id, nil
	}
	return h.startKitty(ctx, r, p)
}

// liveSocket is the first socket that answers. KITTY_LISTEN_ON leads the list,
// and it outlives its kitty in every process started from it - a tmux server,
// a shell restored by a session manager - so the first socket can be dead, and
// a launch into it would fail every time.
func (h *Host) liveSocket(ctx context.Context) (string, bool) {
	for _, s := range h.socketList() {
		if _, err := h.kitten(ctx, s, "ls"); err == nil {
			return s, true
		}
	}
	return "", false
}

// startKitty starts a kitty process for the first panel and waits for its
// control socket. `{kitty_pid}` is expanded by kitty, so the socket matches the
// pattern discovery looks for.
func (h *Host) startKitty(ctx context.Context, r revier.Realization, p revier.PanelSpec) (string, int, error) {
	before := map[string]bool{}
	for _, s := range h.socketList() {
		before[s] = true
	}
	args := []string{"--detach", "--listen-on", "unix:@kitty-{kitty_pid}",
		"-o", "allow_remote_control=socket-only",
		"--name", r.Name, "--title", r.Name, "--hold"}
	if p.Dir != "" {
		args = append(args, "--directory", p.Dir)
	}
	args = append(args, p.Command...)
	if err := h.startProcess(ctx, args...); err != nil {
		return "", 0, err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range h.socketList() {
			if before[s] {
				continue
			}
			windows, err := h.ls(ctx, s)
			if err != nil {
				continue
			}
			for _, w := range windows {
				if w.WMName != r.Name || len(w.Tabs) == 0 || len(w.Tabs[0].Windows) == 0 {
					continue
				}
				return s, w.Tabs[0].Windows[0].ID, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", 0, fmt.Errorf("kitty: started for %q but no control socket answered within 5s", r.Name)
}

func (h *Host) startProcess(ctx context.Context, args ...string) error {
	if h.start != nil {
		return h.start(ctx, args...)
	}
	c := exec.CommandContext(ctx, "kitty", args...)
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Run(); err != nil {
		return fmt.Errorf("kitty: start: %w", err)
	}
	return nil
}

// Focus makes the OS window's active window current inside kitty. Under GNOME
// on Wayland this does not raise the OS window; the core raises it through the
// window host.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	socket, win, err := h.active(ctx, ref)
	if err != nil {
		return err
	}
	_, err = h.kitten(ctx, socket, "focus-window", "--match", "id:"+strconv.Itoa(win))
	return err
}

// active finds the socket an instance lives on and the window current in it.
func (h *Host) active(ctx context.Context, ref revier.TargetRef) (string, int, error) {
	socket, id, err := parseRef(ref.ID)
	if err != nil {
		return "", 0, err
	}
	windows, err := h.ls(ctx, socket)
	if err != nil {
		return "", 0, err
	}
	for _, w := range windows {
		if w.ID != id {
			continue
		}
		win, ok := activeWindow(w)
		if !ok {
			return "", 0, fmt.Errorf("kitty: os window %s has no windows", ref.ID)
		}
		return socket, win, nil
	}
	return "", 0, fmt.Errorf("kitty: os window %s not found", ref.ID)
}

// OpenTab opens a new last tab of the OS window: r.Launch alone, or r.Panels
// with the first as the tab and every later one split into it. `launch
// --match` names the OS window through the window current in it, and the tab
// goes last so the panels keep their order in the listing a save records
// agents by. The vars become user vars of the tab's first window, which ls
// reports back as the panel's Vars.
//
// A launch or a title that fails closes the windows the tab already opened:
// the core names the agent of a failed tab as not added, and it must not be
// running in a tab without its shell.
func (h *Host) OpenTab(ctx context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	socket, win, err := h.active(ctx, ref)
	if err != nil {
		return "", err
	}
	var opened []int
	panel, err := h.openTab(ctx, socket, win, r, vars, &opened)
	if err != nil {
		for _, id := range opened {
			// Best effort: the launch error is the one worth reporting.
			_, _ = h.kitten(ctx, socket, "close-window", "--match", "id:"+strconv.Itoa(id))
		}
		return "", err
	}
	return panel, nil
}

// openTab is OpenTab's launches into the OS window whose current window is
// win, with every window it opens added to opened.
func (h *Host) openTab(ctx context.Context, socket string, win int, r revier.Realization, vars map[string]string, opened *[]int) (revier.PanelID, error) {
	panels := r.PanelSpecs()

	args := []string{"--type=tab", "--location=last", "--match", "window_id:" + strconv.Itoa(win), "--hold"}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--var", name+"="+vars[name])
	}
	first := 0
	for i, p := range panels {
		if i > 0 {
			args = []string{"--type=window", "--match", "window_id:" + strconv.Itoa(first), "--hold"}
		}
		if p.Dir != "" {
			args = append(args, "--cwd", p.Dir)
		}
		args = append(args, p.Command...)
		id, err := h.launch(ctx, socket, args...)
		if err != nil {
			return "", err
		}
		*opened = append(*opened, id)
		if err := h.title(ctx, socket, id, p.Title); err != nil {
			return "", err
		}
		if i == 0 {
			first = id
		}
	}
	return revier.PanelID(strconv.Itoa(first)), nil
}

// FocusPanel makes one kitty window current, switching to its tab. Under GNOME
// on Wayland this does not raise the OS window; the core raises it.
func (h *Host) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	socket, _, err := parseRef(ref.ID)
	if err != nil {
		return err
	}
	_, err = h.kitten(ctx, socket, "focus-window", "--match", "id:"+panel.String())
	return err
}

// FocusedPanel reports the window current in the OS window: the active window
// of its active tab.
func (h *Host) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	_, win, err := h.active(ctx, ref)
	if err != nil {
		return "", err
	}
	return revier.PanelID(strconv.Itoa(win)), nil
}

// SendText types text into one kitty window, on the socket of the process the
// instance lives in. It goes through stdin because kitty reads an argument
// for Python escapes, which would mangle a backslash, and sends stdin as it
// is. kitty reports success even when --match finds nothing, so success here
// means delivered, not received.
func (h *Host) SendText(ctx context.Context, ref revier.TargetRef, panel revier.PanelID, text string) error {
	socket, _, err := parseRef(ref.ID)
	if err != nil {
		return err
	}
	_, err = h.kittenIn(ctx, socket, text, "send-text", "--match", "id:"+panel.String(), "--stdin")
	return err
}

// Close closes every kitty window of the OS window, across its tabs, in one
// call; kitty closes an OS window whose last window closed. kitten has no
// command that names an OS window, so its windows are read from ls first.
func (h *Host) Close(ctx context.Context, ref revier.TargetRef) error {
	socket, id, err := parseRef(ref.ID)
	if err != nil {
		return err
	}
	windows, err := h.ls(ctx, socket)
	if err != nil {
		return err
	}
	var match []string
	for _, w := range windows {
		if w.ID != id {
			continue
		}
		for _, t := range w.Tabs {
			for _, win := range t.Windows {
				match = append(match, "id:"+strconv.Itoa(win.ID))
			}
		}
	}
	if len(match) == 0 {
		return fmt.Errorf("kitty: os window %s not found", ref.ID)
	}
	_, err = h.kitten(ctx, socket, "close-window", "--match", strings.Join(match, " or "))
	return err
}

// ClosePanel closes one kitty window, on the socket of the process the
// instance lives in. A tab left with no window goes with it, which is how a
// tab target closes whole.
func (h *Host) ClosePanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	socket, _, err := parseRef(ref.ID)
	if err != nil {
		return err
	}
	_, err = h.kitten(ctx, socket, "close-window", "--match", "id:"+panel.String())
	return err
}

// activeWindow is the window of the active tab that has the tab's focus, or
// the first window at all when kitty marks none.
func activeWindow(w osWindow) (int, bool) {
	for _, t := range w.Tabs {
		if !t.IsActive {
			continue
		}
		for _, win := range t.Windows {
			if win.IsActive {
				return win.ID, true
			}
		}
	}
	for _, t := range w.Tabs {
		if len(t.Windows) > 0 {
			return t.Windows[0].ID, true
		}
	}
	return 0, false
}

// Focused reports the OS window that holds keyboard focus, across every kitty
// process. kitty tracks OS focus from the compositor, so a zero ref means no
// kitty window is focused, which is the answer the core needs to be able to
// trust.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	listings, err := h.list(ctx)
	if err != nil {
		return revier.TargetRef{}, err
	}
	for _, l := range listings {
		for _, w := range l.windows {
			if w.IsFocused {
				return revier.TargetRef{Host: h.Name(), ID: refID(l.socket, w.ID), Title: w.WMName}, nil
			}
		}
	}
	return revier.TargetRef{}, nil
}
