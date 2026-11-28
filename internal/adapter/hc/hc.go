// Package hc implements a Runtime host over hc, herdr-core
// (https://github.com/herdrdev/herdr-core): a headless terminal runtime
// whose core owns every terminal and gives each a permanent address. It is a
// prototype that a user opts into by name.
//
// hc has no object that groups terminals. An instance is the set of live
// terminals that carry the same revier.instance label, which Open gives every
// terminal it starts, and a tab is the terminals of the
// instance that carry the same revier.tab label. The host speaks hc's
// JSON-RPC on the core's Unix socket.
package hc

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// The labels Open and OpenTab give each terminal of an instance: the
// instance's id, the realization's name, the tab's token, the tab's place
// among the tabs, the terminal's place among the panels of its tab, and the
// panel's title when it has one. The token is the tab's own, so two tabs of
// the same vars, as two agent tabs, stay two; the places keep the order a
// view draws them in.
const (
	instanceLabel = "revier.instance"
	nameLabel     = "revier.name"
	tabLabel      = "revier.tab"
	tabPlaceLabel = "revier.tabplace"
	panelLabel    = "revier.panel"
	titleLabel    = "revier.title"
)

// ownLabels are the labels that are revier's, and no panel var.
var ownLabels = []string{instanceLabel, nameLabel, tabLabel, tabPlaceLabel, panelLabel, titleLabel, heldLabel}

// Host is an hc Runtime, and no View: hc keeps no focus and draws nothing,
// so a terminal is put on an instance by attaching it, by hand or in a
// view's window.
// Socket selects the core's socket; an empty Socket uses the one hc's own CLI
// would use from this environment.
type Host struct {
	Socket string
	// Revier is the revier binary a view's window runs to attach a terminal
	// and hold it once its program ends. With none, the
	// window runs hc's attach alone, and ends with its program.
	Revier string

	// procRoot replaces /proc in tests.
	procRoot string
}

func (h *Host) Name() string { return "hc" }

func (h *Host) root() string {
	if h.procRoot != "" {
		return h.procRoot
	}
	return "/proc"
}

// Probe reports hc usable when herdr-core is on PATH and the core, when one
// listens, is one this host can work with. It does not require a running
// core: Open starts one. It never runs hc itself: every hc command starts a
// core when none runs.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := Program(); err != nil {
		return err
	}
	s, err := h.dial(ctx, false)
	if errors.Is(err, errNoServer) {
		return nil
	}
	if err != nil {
		return err
	}
	s.close()
	return nil
}

// Program is the hc binary on PATH: herdr-core, else an hc that is a link to
// a file named herdr-core, as hc's install makes. Any other hc is not
// herdr-core: hc is also the name of Holochain's CLI. It is found by its name
// and its link, never by running it.
func Program() (string, error) {
	if _, err := exec.LookPath("herdr-core"); err == nil {
		return "herdr-core", nil
	}
	path, err := exec.LookPath("hc")
	if err != nil {
		return "", errors.New("hc: herdr-core not found on PATH")
	}
	if target, err := filepath.EvalSymlinks(path); err == nil && filepath.Base(target) == "herdr-core" {
		return "hc", nil
	}
	return "", errors.New("hc: herdr-core not found (an hc on PATH is not herdr-core)")
}

// Instances lists every live terminal of this machine's core, and each
// finished one a window holds, grouped into
// instances by revier's instance label. A terminal without one that is an
// instance id, as one a user started by hand, is an instance of its own,
// named by its address, so it is seen too. A terminal
// on another machine is left out: its process is not on this machine, where
// a probe reads it.
//
// One connection, one hello and one terminal.list, whatever the project
// count: the list carries every terminal with its labels and its foreground
// process. The list asks each machine added with "hc machine add" too, as
// it has no way to ask this core alone, so a machine that does not answer
// slows each listing by up to 3 s.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	s, err := h.dial(ctx, false)
	if errors.Is(err, errNoServer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer s.close()
	var list listResult
	if err := s.call("terminal.list", map[string]bool{"all": true}, &list); err != nil {
		return nil, err
	}

	var keys []string
	groups := map[string][]terminal{}
	for _, t := range list.Terminals {
		if (t.State != "live" && t.Labels[heldLabel] == "") || strings.Contains(t.Address, "/") {
			continue
		}
		key := t.Labels[instanceLabel]
		if !isInstanceID(key) {
			key = t.Address
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], t)
	}

	instances := make([]revier.Instance, 0, len(keys))
	for _, key := range keys {
		terminals := groups[key]
		slices.SortStableFunc(terminals, func(a, b terminal) int {
			return cmp.Or(cmp.Compare(place(a, tabPlaceLabel), place(b, tabPlaceLabel)), cmp.Compare(place(a, panelLabel), place(b, panelLabel)))
		})
		title := titleOf(terminals[0])
		inst := revier.Instance{
			Ref:   revier.TargetRef{Host: h.Name(), ID: s.hello.Core.ID + "/" + key, Title: title},
			Title: title,
		}
		for _, t := range terminals {
			inst.Panels = append(inst.Panels, panelOf(t))
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// panelOf is a terminal as a panel. What it runs is its foreground process,
// which the Claude probe finds an agent by; a terminal hc has not read one
// for yet is its program.
func panelOf(t terminal) revier.Panel {
	pid, command, dir := t.PID, t.Argv, t.Cwd
	if fg := t.Foreground; fg != nil {
		pid, command, dir = fg.PID, fg.Argv, fg.Cwd
	}
	vars := maps.Clone(t.Labels)
	for _, label := range ownLabels {
		delete(vars, label)
	}
	return revier.Panel{
		ID:      revier.PanelID(t.Address),
		Kind:    kindOf(command),
		Title:   t.Title,
		Vars:    vars,
		Tab:     t.Labels[tabLabel],
		PID:     pid,
		Command: command,
		Dir:     dir,
	}
}

// titleOf is the name revier gave a terminal's instance, else the
// terminal's own name, else its address.
func titleOf(t terminal) string {
	return cmp.Or(t.Labels[nameLabel], t.Name, t.Address)
}

// place is the number a terminal's label holds: its tab's place among the
// tabs, or its own within the tab. A terminal with none comes first.
func place(t terminal, label string) int {
	n, _ := strconv.Atoi(t.Labels[label])
	return n
}

// kindOf classifies a terminal by its command: a shell or a tool. A terminal
// with no command read is a shell, as the kitty and proc hosts report one.
func kindOf(argv []string) revier.PanelKind {
	if len(argv) == 0 || revier.IsShell(argv[0]) {
		return revier.PanelShell
	}
	return revier.PanelTool
}

// ref is an instance ref taken apart: the core that made it, and the
// instance's id or, for a terminal revier did not start, its address.
type ref struct {
	core, key string
}

// parse refuses a ref this host did not produce.
func (h *Host) parse(r revier.TargetRef) (ref, error) {
	core, key, ok := strings.Cut(r.ID, "/")
	if r.Host != h.Name() || !ok || core == "" || key == "" {
		return ref{}, fmt.Errorf("hc: %s/%s is not an hc instance", r.Host, r.ID)
	}
	return ref{core: core, key: key}, nil
}

// byHand reports whether the ref names a terminal by its address, as
// Instances names one whose label is no instance id.
func (r ref) byHand() bool { return !isInstanceID(r.key) }

// isInstanceID reports whether s is an id Open gives an instance: 16
// lowercase hex digits. A revier.instance label that is not one, such as one
// a user set by hand, makes no instance.
func isInstanceID(s string) bool {
	return len(s) == 16 && strings.Trim(s, "0123456789abcdef") == ""
}

// current refuses a ref made by another core than the one answering now: a
// ref outlives a wipe of hc's state, which starts its addresses over.
func (r ref) current(s *session) error {
	if r.core != s.hello.Core.ID {
		return fmt.Errorf("hc: instance %s/%s is gone; the core that answers now is %s", r.core, r.key, s.hello.Core.ID)
	}
	return nil
}
