// Package proc implements a Host over /proc: the agents and shells that
// `revier agent exec` started on this machine for a terminal on another one
// (decisions.md D84).
//
// Such a process runs under sshd and in no terminal this machine has, so no
// runtime lists it. It carries two variables in its environment instead: the
// workspace it was started for, and the tag the terminal that started it
// knows it by. One instance is reported per workspace, with one panel per tag.
//
// The host lists and does nothing else. What it lists is shown in a terminal
// elsewhere, so there is nothing here to open or to focus.
//
// The package is also the one reader of a process's parent and terminal on
// this machine: Parent, which the kitty host walks a window's processes with,
// ParentIn, which the herdr host finds a pane's process with, Foreground and
// ForegroundIn, which the tmux and herdr hosts read a pane's command with,
// Scan, the one read of every process's environment, which this host and the
// herdr host share, and RunsUnder, the panels a shutdown closes last.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

const (
	// TagVar names a panel: the terminal that started the process chose it.
	TagVar = "REVIER_TAG"

	// WorkspaceVar names the instance: the realization the process was
	// started for, which is what that realization's Match finds.
	WorkspaceVar = "REVIER_WORKSPACE"
)

// ErrListsOnly refuses an Open or a Focus.
var ErrListsOnly = errors.New("proc: lists what `revier agent exec` started and opens nothing")

// Host lists tagged processes. The zero value reads /proc.
type Host struct {
	// Root replaces /proc in tests.
	Root string
}

func (h *Host) Name() string { return "proc" }

func (h *Host) root() string {
	if h.Root != "" {
		return h.Root
	}
	return "/proc"
}

// Probe needs a /proc that lists this process with its environment.
func (h *Host) Probe(context.Context) error {
	if h.Root != "" {
		return nil
	}
	if _, err := os.ReadFile(filepath.Join(h.root(), "self", "environ")); err != nil {
		return fmt.Errorf("proc: %w", err)
	}
	return nil
}

type process struct {
	pid, ppid, pgrp, tty, tpgid int
	tag, workspace              string
	cmdline                     []string
}

// Instances reads /proc once. A process of another user refuses the read of
// its environment and is passed over, as is one that ended meanwhile.
func (h *Host) Instances(context.Context) ([]revier.Instance, error) {
	scanned, err := Scan(h.root(), TagVar, WorkspaceVar)
	if err != nil {
		return nil, fmt.Errorf("proc: %w", err)
	}
	byTag := map[string][]process{}
	for pid, vars := range scanned {
		if p, ok := h.read(pid, vars); ok {
			byTag[p.tag] = append(byTag[p.tag], p)
		}
	}

	panels := map[string][]revier.Panel{}
	for tag, tagged := range byTag {
		p, ok := nearest(tagged)
		if !ok {
			continue
		}
		panels[p.workspace] = append(panels[p.workspace], revier.Panel{
			ID: revier.PanelID(tag), Kind: kindOf(p.cmdline), PID: p.pid, Command: p.cmdline,
		})
	}
	out := make([]revier.Instance, 0, len(panels))
	for workspace, ps := range panels {
		sort.Slice(ps, func(i, j int) bool { return ps[i].PID < ps[j].PID })
		out = append(out, revier.Instance{
			Ref:    revier.TargetRef{Host: h.Name(), ID: workspace, Title: workspace},
			Title:  workspace,
			Panels: ps,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.ID < out[j].Ref.ID })
	return out, nil
}

func (h *Host) read(pid int, vars map[string]string) (process, bool) {
	p := process{pid: pid, tag: vars[TagVar], workspace: vars[WorkspaceVar]}
	if p.workspace == "" {
		return process{}, false
	}
	dir := filepath.Join(h.root(), strconv.Itoa(pid))
	cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return process{}, false
	}
	p.cmdline = strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return process{}, false
	}
	// state, ppid, pgrp, session, tty, tpgid.
	fields := statFields(stat)
	if len(fields) < 6 {
		return process{}, false
	}
	for _, f := range []struct {
		into *int
		at   int
	}{{&p.ppid, 1}, {&p.pgrp, 2}, {&p.tty, 4}, {&p.tpgid, 5}} {
		if *f.into, err = strconv.Atoi(fields[f.at]); err != nil {
			return process{}, false
		}
	}
	return p, true
}

// nearest is the panel's process among those that carry one tag: the program
// in the foreground of the panel's terminal nearest the one `revier agent
// exec` became that is not a shell, as the kitty host reads a window's
// foreground. Everything the program starts inherits the tag - a tool, or a
// second agent run with `claude -p`, which Claude Code lists as a session of
// its own - and is deeper. With shells alone in the foreground, the panel is
// the first of them.
//
// The terminal is the one the tagged process nearest the top of the tree
// runs on, the pty sshd gave it, and only its foreground process group is the
// panel's: a helper a shell's profile started is in the background or on a pty
// of its own, a daemon has no terminal, and a process that outlived the
// terminal - the ssh ended, the pty hung up - is in no foreground group. A tag
// with nothing in the foreground has no panel.
func nearest(tagged []process) (process, bool) {
	byPID := make(map[int]process, len(tagged))
	for _, p := range tagged {
		byPID[p.pid] = p
	}
	depth := func(p process) int {
		d := 0
		for {
			parent, ok := byPID[p.ppid]
			if !ok || d > len(tagged) {
				return d
			}
			p, d = parent, d+1
		}
	}
	sort.Slice(tagged, func(i, j int) bool {
		if di, dj := depth(tagged[i]), depth(tagged[j]); di != dj {
			return di < dj
		}
		return tagged[i].pid < tagged[j].pid
	})
	tty := 0
	for _, p := range tagged {
		if p.tty != 0 {
			tty = p.tty
			break
		}
	}
	var foreground []process
	for _, p := range tagged {
		if tty != 0 && p.tty == tty && p.pgrp == p.tpgid {
			foreground = append(foreground, p)
		}
	}
	for _, p := range foreground {
		if !isShell(p.cmdline) {
			return p, true
		}
	}
	if len(foreground) == 0 {
		return process{}, false
	}
	return foreground[0], true
}

func kindOf(cmdline []string) revier.PanelKind {
	if isShell(cmdline) {
		return revier.PanelShell
	}
	return revier.PanelTool
}

func isShell(cmdline []string) bool {
	if len(cmdline) == 0 {
		return true
	}
	return revier.IsShell(cmdline[0])
}

func (h *Host) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	return revier.TargetRef{}, ErrListsOnly
}

func (h *Host) Focus(context.Context, revier.TargetRef) error { return ErrListsOnly }

// Focused is never one of these: focus is in a terminal on another machine.
func (h *Host) Focused(context.Context) (revier.TargetRef, error) { return revier.TargetRef{}, nil }

// statFields is the fields of a /proc stat line after the command name. The
// name is in parentheses and may hold spaces and parentheses of its own, so
// the fields are counted from its last closing one: state, then ppid.
func statFields(stat []byte) []string {
	return strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
}

// Scan reads the environment of every process in the /proc tree at root once,
// and returns by pid each process whose environment sets key to a value, with
// key and those of more that it sets.
// A process of another user refuses the read of its environment and is passed
// over, as is one that ended meanwhile.
func Scan(root, key string, more ...string) (map[int]map[string]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	setsKey := []byte(key + "=")
	out := map[int]map[string]string{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		env, err := os.ReadFile(filepath.Join(root, e.Name(), "environ"))
		if err != nil || !bytes.Contains(env, setsKey) {
			continue
		}
		vars := map[string]string{}
		for _, kv := range strings.Split(string(env), "\x00") {
			name, value, _ := strings.Cut(kv, "=")
			if name == key || slices.Contains(more, name) {
				vars[name] = value
			}
		}
		if vars[key] != "" {
			out[pid] = vars
		}
	}
	return out, nil
}

// Parent reads a process's parent from this machine's /proc.
func Parent(pid int) (int, bool) { return ParentIn("/proc", pid) }

// ParentIn reads a process's parent from the /proc tree at root, which a test
// replaces.
func ParentIn(root string, pid int) (int, bool) {
	stat, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	fields := statFields(stat)
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

// Foreground reads the foreground process of the terminal pid runs in: the
// leader of the terminal's foreground process group, and its argv. That is
// the process tmux names in pane_current_command, so the two agree.
func Foreground(pid int) (int, []string, bool) { return ForegroundIn("/proc", pid) }

// ForegroundIn is Foreground from the /proc tree at root, which a test
// replaces.
func ForegroundIn(root string, pid int) (int, []string, bool) {
	stat, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, nil, false
	}
	// state, ppid, pgrp, session, tty_nr, tpgid.
	fields := statFields(stat)
	if len(fields) < 6 {
		return 0, nil, false
	}
	fg, err := strconv.Atoi(fields[5])
	if err != nil || fg <= 0 {
		return 0, nil, false
	}
	cmdline, err := os.ReadFile(filepath.Join(root, strconv.Itoa(fg), "cmdline"))
	// A process that cleared its argv reads as NULs alone, and names no
	// program.
	args := strings.TrimRight(string(cmdline), "\x00")
	if err != nil || args == "" {
		return 0, nil, false
	}
	return fg, strings.Split(args, "\x00"), true
}

// RunsUnder reports a panel the calling process runs under: the panel's
// process is this process or one of its ancestors. The ancestors are read on
// the first ask, so a command that closes nothing reads none.
func RunsUnder() func(revier.Panel) bool {
	ancestors := sync.OnceValue(func() map[int]bool {
		mine := map[int]bool{}
		for pid := os.Getpid(); pid > 1 && !mine[pid]; {
			mine[pid] = true
			ppid, ok := Parent(pid)
			if !ok {
				break
			}
			pid = ppid
		}
		return mine
	})
	return func(p revier.Panel) bool { return p.PID > 0 && ancestors()[p.PID] }
}
