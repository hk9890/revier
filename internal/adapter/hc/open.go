package hc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/userunit"
	"github.com/hk9890/revier/pkg/revier"
)

// Open starts one terminal per panel, starting the core when none runs. The
// terminals share a new instance id as their revier.instance label, and carry
// r.Name as revier.name, which Instances reads as the title, the token of the
// instance's first tab and its place 0, their place in it as revier.panel,
// and r.Vars as labels of their own, which Instances reports back as the
// panel's Vars. Each is named rv-<id>-<place>, which the attach takes.
//
// A terminal has no parent, so it outlives the terminal revier runs in, and
// is not kept: a kept terminal hc brings back after a reboot is a bare shell
// under the agent's labels (decisions.md D173). A panel's own command ends
// to a finished terminal, so the window that shows it can show what it left,
// as the kitty runtime holds it (D187); a shell is removed when it exits.
// Instances lists live terminals alone, so an instance whose terminals all
// ended is never seen to be closed: sweep removes what no window took.
//
// An instance that fails is closed whole: left open, it would be matched.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" || !utf8.ValidString(r.Name) {
		return revier.TargetRef{}, fmt.Errorf("hc: realization has no name to give the instance")
	}
	if err := keeps(r.Vars); err != nil {
		return revier.TargetRef{}, err
	}
	panels := r.PanelSpecs()
	for _, p := range panels {
		if p.Dir != "" && !isDir(p.Dir) {
			// hc reports a missing directory as the program not starting.
			return revier.TargetRef{}, fmt.Errorf("hc: directory %q does not exist", p.Dir)
		}
	}
	s, err := h.dial(ctx, false)
	if errors.Is(err, errNoServer) {
		s, err = h.startCore(ctx)
	}
	if err != nil {
		return revier.TargetRef{}, err
	}
	defer s.close()
	s.sweep(time.Now())
	// The core starts each program on its own PATH, which can differ from
	// this process's: the core runs as a service of the user's service
	// manager, with the manager's PATH, or was started by hand. So the core
	// is started before a program is checked, and a core started here stays
	// up when a program is not on its PATH. /proc shows the environment the core was
	// started with: after "hc server restart" the core sets the environment
	// it saved, which /proc does not show, so the check can then use a stale
	// PATH.
	//
	// hc takes a bare first word that names one of the user's hc templates as
	// that template, and runs the template's command in place of the
	// program; a word with a '/' it never takes as one, so each program goes
	// as the file the core's PATH holds.
	commands, err := panelstart.CommandsOn(panels, proc.ServerPATH(h.root(), s.pid))
	if err != nil {
		return revier.TargetRef{}, err
	}

	id, err := instanceID()
	if err != nil {
		return revier.TargetRef{}, err
	}
	token, err := instanceID()
	if err != nil {
		return revier.TargetRef{}, err
	}
	tab := map[string]string{instanceLabel: id, nameLabel: r.Name, tabLabel: token, tabPlaceLabel: "0"}
	name := func(i int) string { return terminalName(id, i) }
	if _, err := start(s, tab, r.Vars, panels, commands, runEnv(h.root(), s.pid), name); err != nil {
		// It finds the terminals by the instance label, so one whose run was
		// not answered goes too.
		h.closeAfter(ctx, s.hello.Core.ID, map[string]string{instanceLabel: id})
		return revier.TargetRef{}, err
	}
	return revier.TargetRef{Host: h.Name(), ID: s.hello.Core.ID + "/" + id, Title: r.Name}, nil
}

// OpenTab starts the terminals of a new last tab of the instance: one per
// panel of r, or r.Launch in r.Dir alone. They carry the instance's labels, a
// new tab token, the place after the last tab the instance holds, their place
// in the tab, and vars as labels of their own, so a view draws them as one
// tab after the others, and two tabs of the same vars stay two (decisions.md
// D179). hc keeps no focus, so the focus is a view's.
//
// A tab whose terminal fails is closed whole, found by its token, as Open
// closes an instance: the core names the agent of a failed tab as not added.
// A terminal started by hand is no instance of revier's, and takes no tab.
func (h *Host) OpenTab(ctx context.Context, r revier.TargetRef, real revier.Realization, vars map[string]string) (revier.PanelID, error) {
	if err := keeps(vars); err != nil {
		return "", err
	}
	panels := real.PanelSpecs()
	for _, p := range panels {
		if p.Dir != "" && !isDir(p.Dir) {
			return "", fmt.Errorf("hc: directory %q does not exist", p.Dir)
		}
	}
	s, ref, err := h.session(ctx, r, false)
	if err != nil {
		return "", err
	}
	defer s.close()
	if ref.byHand() {
		return "", fmt.Errorf("hc: %s is a terminal started by hand, not an instance of revier's, and opens no tab", ref.key)
	}
	s.sweep(time.Now())
	var list listResult
	if err := s.call("terminal.list", map[string]any{"labels": map[string]string{instanceLabel: ref.key}}, &list); err != nil {
		return "", err
	}
	if len(list.Terminals) == 0 {
		return "", fmt.Errorf("hc: instance %s runs no terminal to open a tab beside", r.ID)
	}
	last := -1
	for _, t := range list.Terminals {
		last = max(last, place(t, tabPlaceLabel))
	}
	commands, err := panelstart.CommandsOn(panels, proc.ServerPATH(h.root(), s.pid))
	if err != nil {
		return "", err
	}
	token, err := instanceID()
	if err != nil {
		return "", err
	}
	tab := map[string]string{
		instanceLabel: ref.key, nameLabel: titleOf(list.Terminals[0]), tabLabel: token, tabPlaceLabel: strconv.Itoa(last + 1),
	}
	name := func(i int) string { return tabTerminalName(ref.key, token, i) }
	first, err := start(s, tab, vars, panels, commands, runEnv(h.root(), s.pid), name)
	if err != nil {
		h.closeAfter(ctx, ref.core, map[string]string{instanceLabel: ref.key, tabLabel: token})
		return "", err
	}
	return revier.PanelID(first), nil
}

// closeAfter closes the terminals that carry labels, live or finished, after
// a start that failed. The close has a bound and a session of its own: the
// end of ctx may be what failed the start.
func (h *Host) closeAfter(ctx context.Context, core string, labels map[string]string) {
	closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = h.closeLabelled(closing, core, labels)
}

// start runs the panels of one tab, each with its command and env, under the
// tab's labels, its place as revier.panel, and vars, and checks, in one
// listing, that each still runs: one that ended is finished, or missing when
// it was a shell, which hc removes as it exits. It returns the address of
// the tab's first terminal.
//
// The check sees only a program that ended before the listing: one that ends
// a moment later leaves a tab without that panel. A terminal whose
// revier.panel label names no place among the panels, as one a user labelled
// by hand, is none of them.
func start(s *session, tab, vars map[string]string, panels []revier.PanelSpec, commands [][]string, env map[string]string, name func(int) string) (string, error) {
	for i, p := range panels {
		labels := maps.Clone(tab)
		labels[panelLabel] = strconv.Itoa(i)
		maps.Copy(labels, vars)
		params := map[string]any{
			"labels": labels, "name": name(i), "no_parent": true, "keep": false, "rm": len(p.Command) == 0, "env": env,
		}
		if commands[i] != nil {
			params["argv"] = commands[i]
		}
		if p.Dir != "" {
			params["cwd"] = p.Dir
		}
		if err := s.call("terminal.run", params, nil); err != nil {
			return "", notStarted(err, p)
		}
	}

	var list listResult
	filter := map[string]string{instanceLabel: tab[instanceLabel], tabLabel: tab[tabLabel]}
	if err := s.call("terminal.list", map[string]any{"labels": filter, "all": true}, &list); err != nil {
		return "", err
	}
	running := make([]string, len(panels))
	for _, t := range list.Terminals {
		i, err := strconv.Atoi(t.Labels[panelLabel])
		if t.State == "live" && err == nil && i >= 0 && i < len(panels) {
			running[i] = t.Address
		}
	}
	for i, address := range running {
		if address == "" {
			return "", panelstart.ExitedAtOnce(panels[i])
		}
	}
	return running[0], nil
}

// notStarted words hc's spawn_failed as the panel's start failure. Its
// program was found on the core's PATH before the run, and the directory
// checked, so hc's own reason, such as an interpreter its first line names
// that is missing, is the one to give.
func notStarted(err error, p revier.PanelSpec) error {
	var e *apiError
	if errors.As(err, &e) && e.Data.Kind == "spawn_failed" {
		return panelstart.DidNotStart(p, err)
	}
	return err
}

// terminalName is the name of the terminal at place i of an instance's first
// tab. hc takes 1-32 of a-z, 0-9, '-', '_' and '.', and an instance id is 16
// hex digits.
func terminalName(id string, i int) string {
	return "rv-" + id + "-" + strconv.Itoa(i)
}

// tabTerminalName is the name of the terminal at place i of a tab OpenTab
// starts: the instance id and the first 8 hex digits of the tab's token, so
// two tabs never share a name and the name stays within 32 characters.
func tabTerminalName(id, token string, i int) string {
	return "rv-" + id + "-" + token[:8] + "-" + strconv.Itoa(i)
}

// instanceID is a new instance id: 64 random bits as 16 lowercase hex digits,
// which isInstanceID accepts.
func instanceID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("hc: instance id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// keeps refuses vars hc would not keep as written as labels. hc takes any
// key but an empty one or one with '=', and drops a label whose value is
// empty; a string that is not UTF-8 does not survive JSON. revier's own
// labels are not a var's to set.
func keeps(vars map[string]string) error {
	for k, v := range vars {
		switch {
		case k == "" || strings.Contains(k, "=") || !utf8.ValidString(k):
			return fmt.Errorf("hc: var %q is not a label key hc takes: it must be UTF-8, not empty, with no '='", k)
		case slices.Contains(ownLabels, k):
			return fmt.Errorf("hc: var %s is revier's own label", k)
		case v == "" || !utf8.ValidString(v):
			return fmt.Errorf("hc: var %s=%q is not kept as written: hc drops an empty label, and takes UTF-8 alone", k, v)
		}
	}
	return nil
}

// startWait is how long a core revier started may take to answer: hc's own
// CLI waits as long for one it starts.
const startWait = 15 * time.Second

// coreUnit is the service the user's service manager runs the core on socket
// as: revier-hc- and the first 8 hex digits of the socket path's SHA-256. One
// name per socket makes a second start while it runs fail, so two presses at
// once start one core, and names it to stop. The core of another socket, as
// another hc session's, is a unit of another name: this socket's start
// neither fails on it nor takes it for its own.
func coreUnit(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return "revier-hc-" + hex.EncodeToString(sum[:4])
}

// startCore starts the core with "hc server start", which waits for the core
// to answer, and returns a session with it. What the start says on stderr is
// the reason a start that fails gives.
//
// The core hands every program it starts its starter's environment, for its
// whole life: started from a shell, it would hand that shell's venv or
// credentials to every agent. So the start runs as a service of the user's
// service manager, with the session's login environment and the socket this
// host dials (decisions.md D167). The start forks the core and exits, which
// the service takes as its start. Where there is no service manager, it runs
// detached, in a session of its own with no terminal to write to, and with
// coreEnv.
func (h *Host) startCore(ctx context.Context) (*session, error) {
	bin, err := Program()
	if err != nil {
		return nil, err
	}
	// stderr is a file, not a pipe: a core that keeps the start's stderr
	// then holds neither the wait open nor a pipe whose reader went away,
	// which would fail its next write.
	stderr, err := startLog()
	if err != nil {
		return nil, fmt.Errorf("hc: %s server start: %w", bin, err)
	}
	defer func() {
		_ = syscall.Flock(int(stderr.Fd()), syscall.LOCK_UN)
		_ = stderr.Close()
	}()
	service := userunit.Unit{
		Name:       coreUnit(h.socket()),
		Properties: []string{"Type=forking", "StandardOutput=null", "StandardError=append:" + stderr.Name()},
		Env:        []string{"HC_SOCKET=" + h.socket()},
	}
	// The manager's environment names no session: the core is told the
	// session this host's socket is for.
	if session := os.Getenv("HC_SESSION"); session != "" {
		service.Env = append(service.Env, "HC_SESSION="+session)
	}
	detached := func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, bin, "server", "start")
		cmd.Env = coreEnv(os.Environ())
		if h.Socket != "" {
			cmd.Env = append(cmd.Env, "HC_SOCKET="+h.Socket)
		}
		cmd.Stderr = stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		return cmd.Run()
	}
	// The manager's PATH need not hold the one program found on.
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("hc: %w", err)
	}
	if err := userunit.Start(ctx, service, []string{path, "server", "start"}, detached); err != nil {
		said, _ := io.ReadAll(io.NewSectionReader(stderr, 0, 64<<10))
		return nil, fmt.Errorf("hc: %s server start: %w: %s", bin, err, strings.TrimSpace(string(said)))
	}
	s, err := h.waitCore(ctx)
	if errors.Is(err, errNoServer) {
		// hc's config.toml can move the socket; the started core then
		// answers there, where revier does not look.
		return nil, fmt.Errorf("hc: a core was started, but none answers on %s within %s; if hc's config.toml sets socket, set HC_SOCKET to the same path", h.socket(), startWait)
	}
	return s, err
}

// startLogName is the file under revier's state directory that holds what
// the last "hc server start" revier ran wrote on stderr, and what the core it
// started wrote there since. Each start truncates it, which bounds it.
const startLogName = "hc-server-start.log"

// startLog opens the start's stderr file for writing and for reading back,
// locked and truncated. The lock keeps a start that another revier process
// runs at the same time from truncating this start's output before it is
// read back; the caller unlocks it by hand, since a core that keeps the
// start's stderr keeps the file open.
func startLog() (*os.File, error) {
	root, err := state.Root()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, startLogName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// coreEnv is proc.WithoutAgentSession of env, without foreignVars too, which
// hold the variables hc sets in each of its terminals and its CLI reads. env
// itself is left as it was.
func coreEnv(env []string) []string {
	return proc.Without(proc.WithoutAgentSession(env), foreignVars)
}

// runEnv is the env each run gives its program, for the core with process
// pid, from the /proc tree at root: each of foreignVars the core holds, set
// empty, as proc.Cleared does, and every agent session variable set empty
// whether the core holds it or not. Claude Code reads an empty one as unset,
// and /proc does not show what a core holds after "hc server restart": it
// sets the environment it saved, which a core started from inside a Claude
// Code session holds that session's in.
func runEnv(root string, pid int) map[string]string {
	env := proc.Cleared(root, pid, foreignVars)
	for _, name := range proc.AgentSessionVars {
		env[name] = ""
	}
	return env
}

// waitCore waits up to startWait for a core just started to answer.
func (h *Host) waitCore(ctx context.Context) (*session, error) {
	deadline := time.Now().Add(startWait)
	for {
		s, err := h.dial(ctx, false)
		if !errors.Is(err, errNoServer) || time.Now().After(deadline) {
			return s, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// foreignVars name what a program must not inherit from whatever started the
// core: the terminal, the multiplexer, the agent session and the revier
// command that process ran in. An agent that carried them would act on a
// kitty window, a tmux pane, a herdr pane, a Claude Code session or a revier
// tag that is not its own. hc replaces the terminal identity it knows, TERM
// and its kin, and sets its own HC_TERMINAL_ID, HC_TERMINAL_SOCKET and
// HC_SCOPE over a run's env, but none of the others. Each run sets empty
// those the core holds, and the agent session's always (runEnv): a core
// revier started holds none, and one started by hand from a terminal holds
// that terminal's.
var foreignVars = slices.Concat(proc.TerminalVars, proc.CommandVars, proc.AgentSessionVars)

// closeLabelled kills and removes every terminal that carries labels, live
// or finished, on the core called core, all at once: hc's kill can take a
// second a terminal, and a plain connection answers one call after another,
// so each terminal is ended on a connection of its own. Each connection
// checks that core still answers: a core started on the socket in between
// gives the same addresses to terminals of its own.
func (h *Host) closeLabelled(ctx context.Context, core string, labels map[string]string) error {
	dial := func() (*session, error) {
		s, err := h.dial(ctx, false)
		if err != nil {
			return nil, err
		}
		if err := (ref{core: core, key: labels[instanceLabel]}).current(s); err != nil {
			s.close()
			return nil, err
		}
		return s, nil
	}
	s, err := dial()
	if err != nil {
		return err
	}
	var list listResult
	err = s.call("terminal.list", map[string]any{"labels": labels, "all": true}, &list)
	s.close()
	if err != nil {
		return err
	}
	errs := make([]error, len(list.Terminals))
	var wg sync.WaitGroup
	for i, t := range list.Terminals {
		wg.Go(func() {
			s, err := dial()
			if err != nil {
				errs[i] = err
				return
			}
			defer s.close()
			errs[i] = s.end(t.Address)
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// end kills a terminal and removes its record. hc's kill sends HUP, then TERM
// after half a second, then KILL after another: a program that saves on
// neither gets about a second, and hc has no gentler close.
//
// A terminal hc says not_found of is ended already: a shell Open started is
// removed when it exits, which can be before the kill, or the kill itself,
// and the window that held a terminal removes it.
func (s *session) end(address string) error {
	params := map[string]string{"terminal": address}
	for _, method := range []string{"terminal.kill", "terminal.rm"} {
		if err := s.call(method, params, nil); err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}
