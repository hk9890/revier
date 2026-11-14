package hc

import (
	"context"
	"crypto/rand"
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
	"github.com/hk9890/revier/pkg/revier"
)

// Open starts one terminal per panel, starting the core when none runs. The
// terminals share a new instance id as their revier.instance label, and carry
// r.Name as revier.name, which Instances reads as the title, their place as
// revier.panel, and r.Vars as labels of their own, which Instances reports
// back as the panel's Vars. Each is named rv-<id>-<place>, which the attach
// takes.
//
// A terminal has no parent, so it outlives the terminal revier runs in, and
// is not kept: a kept terminal hc brings back after a reboot is a bare shell
// under the agent's labels (decisions.md D173). It is removed when its
// program exits: Instances lists live terminals alone, so an instance whose
// terminals all exited is never seen to be closed, and hc would hold their
// records and screens for a day.
//
// An instance that fails is closed whole, as tmux kills a session that fails:
// left open, it would be matched.
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
	// The core starts each program on its own PATH, which can differ from
	// this process's: "hc server start" can hand the core to a systemd user
	// unit, which gives it the PATH of the user's manager. So the core runs
	// before a program is checked, and a core started here stays up when a
	// program is not on its PATH. /proc shows the environment the core was
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
	if err := start(s, id, r, panels, commands, runEnv(h.root(), s.pid)); err != nil {
		// The close has a bound and a session of its own: the end of ctx may
		// be what failed the instance. It finds the terminals by the instance
		// label, so one whose run was not answered goes too.
		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = h.closeInstance(closing, s.hello.Core.ID, id)
		return revier.TargetRef{}, err
	}
	return revier.TargetRef{Host: h.Name(), ID: s.hello.Core.ID + "/" + id, Title: r.Name}, nil
}

// start runs the panels of instance id, each with its command and env, and
// checks, in one listing, that each still runs. hc removes a terminal when
// its program exits, so one that exited is missing from the listing.
//
// The check sees only a program that ended before the listing: one that ends
// a moment later leaves an instance without that panel. A terminal whose
// revier.panel label names no place among the panels, as one a user labelled
// by hand, is none of them.
func start(s *session, id string, r revier.Realization, panels []revier.PanelSpec, commands [][]string, env map[string]string) error {
	for i, p := range panels {
		labels := map[string]string{instanceLabel: id, nameLabel: r.Name, panelLabel: strconv.Itoa(i)}
		maps.Copy(labels, r.Vars)
		params := map[string]any{
			"labels": labels, "name": terminalName(id, i), "no_parent": true, "keep": false, "rm": true, "env": env,
		}
		if commands[i] != nil {
			params["argv"] = commands[i]
		}
		if p.Dir != "" {
			params["cwd"] = p.Dir
		}
		if err := s.call("terminal.run", params, nil); err != nil {
			return notStarted(err, p)
		}
	}

	var list listResult
	if err := s.call("terminal.list", map[string]any{"labels": map[string]string{instanceLabel: id}, "all": true}, &list); err != nil {
		return err
	}
	running := make([]bool, len(panels))
	for _, t := range list.Terminals {
		i, err := strconv.Atoi(t.Labels[panelLabel])
		if t.State == "live" && err == nil && i >= 0 && i < len(panels) {
			running[i] = true
		}
	}
	for i, live := range running {
		if !live {
			return panelstart.ExitedAtOnce(panels[i])
		}
	}
	return nil
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

// terminalName is the name of an instance's terminal at place i. hc takes
// 1-32 of a-z, 0-9, '-', '_' and '.', and an instance id is 16 hex digits.
func terminalName(id string, i int) string {
	return "rv-" + id + "-" + strconv.Itoa(i)
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
		case k == instanceLabel || k == nameLabel || k == panelLabel:
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

// startCore starts the core with "hc server start", which waits for the core
// to answer, and returns a session with it. It runs in a session of its own,
// with no terminal to write to; what it says on stderr is the reason a start
// that fails gives.
//
// The core hands every program it starts its starter's environment, for its
// whole life, so the core is started with coreEnv.
func (h *Host) startCore(ctx context.Context) (*session, error) {
	bin, err := program()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "server", "start")
	cmd.Env = coreEnv(os.Environ())
	if h.Socket != "" {
		cmd.Env = append(cmd.Env, "HC_SOCKET="+h.Socket)
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
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err != nil {
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
var foreignVars = slices.Concat(proc.TerminalVars, []string{
	"HERDR_ENV", "HERDR_SESSION", "HERDR_SOCKET_PATH", "HERDR_WORKSPACE_ID", "HERDR_TAB_ID", "HERDR_PANE_ID",
}, proc.CommandVars, proc.AgentSessionVars)

// closeInstance kills and removes every terminal that carries the instance
// id, live or finished, on the core called core, all at once: hc's kill can
// take a second a terminal, and a plain connection answers one call after
// another, so each terminal is ended on a connection of its own. Each
// connection checks that core still answers: a core started on the socket in
// between gives the same addresses to terminals of its own.
func (h *Host) closeInstance(ctx context.Context, core, id string) error {
	dial := func() (*session, error) {
		s, err := h.dial(ctx, false)
		if err != nil {
			return nil, err
		}
		if err := (ref{core: core, key: id}).current(s); err != nil {
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
	err = s.call("terminal.list", map[string]any{"labels": map[string]string{instanceLabel: id}, "all": true}, &list)
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
// A terminal hc says not_found of is ended already: one Open started is
// removed when its program exits, which can be before the kill, or the kill
// itself.
func (s *session) end(address string) error {
	params := map[string]string{"terminal": address}
	for _, method := range []string{"terminal.kill", "terminal.rm"} {
		if err := s.call(method, params, nil); err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}
