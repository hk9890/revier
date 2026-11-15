package kitty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"

	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// The user vars of a kitty window the view opens: the runtime panel it
// attaches, and the runtime instance that panel is in. They map a key's kitty
// window id to the panel behind it, and a runtime ref to the windows that
// show it (decisions.md D180).
const (
	viewPanelVar    = "REVIER_VIEW_PANEL"
	viewInstanceVar = "REVIER_VIEW_INSTANCE"
)

// Attachable is a runtime whose panels a kitty window shows by running a
// command: hc, whose attach shows one terminal.
type Attachable interface {
	Name() string
	AttachPanel(panel revier.PanelID) ([]string, error)
}

// View is the kitty View of a Runtime that is not kitty: hc, which draws
// nothing (decisions.md D178, D180). It opens one kitty OS window per runtime
// instance in the workspace kitty, titled as the instance, one kitty tab per
// tab of it, and one kitty window per panel, which runs the runtime's attach
// of that panel and carries the panel and the instance in user vars. Every
// call takes and returns the runtime's refs and panel ids, read back from
// those vars.
//
// A window only attaches, so closing one ends no program, and the runtime
// lists the instance on (D181). A window is never held: the attach ends with
// its panel, and the window with it.
type View struct {
	Runtime Attachable

	kitty Host
}

func (v *View) Name() string { return "kitty" }

// Probe reports the view usable where kitty is and a display is to show it
// on: over ssh or with no display, the runtime runs unshown (decisions.md
// D178).
func (v *View) Probe(ctx context.Context) error {
	if err := v.kitty.Probe(ctx); err != nil {
		return err
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
		return errors.New("kitty: no display to show a window on: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	return nil
}

// Capabilities says each instance is an OS window of the instance's title,
// which the window host raises (decisions.md D177).
func (v *View) Capabilities() revier.Capabilities {
	return revier.Capabilities{OSWindows: true}
}

// shown is the windows of the workspace kitties that show a runtime
// instance: the socket of the first that does, its OS window, the kitty
// window of each panel shown, across every OS window that shows one, and
// whether an OS window of that kitty has the keyboard.
type shown struct {
	socket  string
	window  *osWindow
	panels  map[revier.PanelID]int
	focused bool
}

// windowsOf finds what shows the instance ref names in the workspace kitties
// that answer. A kitty that does not answer shows nothing, and failed is
// why each did not.
func (v *View) windowsOf(ctx context.Context, ref revier.TargetRef) (found shown, failed error) {
	found = shown{panels: map[revier.PanelID]int{}}
	for _, socket := range v.workspaces() {
		windows, err := v.kitty.ls(ctx, socket)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		focused := slices.ContainsFunc(windows, func(w osWindow) bool { return w.IsFocused })
		for i := range windows {
			for _, t := range windows[i].Tabs {
				for _, win := range t.Windows {
					if win.UserVars[viewInstanceVar] != ref.ID {
						continue
					}
					if found.window == nil {
						found.socket, found.window, found.focused = socket, &windows[i], focused
					}
					if found.socket == socket {
						found.panels[revier.PanelID(win.UserVars[viewPanelVar])] = win.ID
					}
				}
			}
		}
	}
	return found, failed
}

// workspaces is the sockets of the workspace kitties: the one kitty the view
// opens its windows in, and any that ran before it.
func (v *View) workspaces() []string {
	return slices.DeleteFunc(v.kitty.socketList(), func(s string) bool { return !workspaceSocket.MatchString(s) })
}

// Show draws the instance in the workspace kitty, starting that kitty when
// none answers: an OS window for it where none shows it and open says it may,
// a kitty tab for each of its tabs, and a kitty window for each panel, in the
// runtime's order. A panel shown already is left as it is, so a window
// closed by hand comes back over the live panels and starts nothing. A tab
// of which no panel is shown opens last; a panel of a tab shown splits into
// that tab.
//
// The keyboard stays where it is while a window of the kitty has it, as
// OpenTab of the kitty runtime leaves it; the core focuses what a press asks
// for. A new OS window takes the keyboard as kitty gives it one.
//
// Two windows of one title make every later raise of the instance fail, so
// one Show runs at a time across revier's processes, under a lock in
// revier's state directory, and lists the kitties once it holds it: two
// presses at once open one window. A workspace kitty that does not answer
// may show the instance, so with no window found the Show fails rather than
// opening one more.
func (v *View) Show(ctx context.Context, inst revier.Instance, open bool) (bool, error) {
	if inst.Title == "" {
		return false, fmt.Errorf("kitty: %s has no title to give its window", inst.Ref.ID)
	}
	unlock, err := showLock()
	if err != nil {
		return false, fmt.Errorf("kitty: show %s: %w", inst.Ref.ID, err)
	}
	defer unlock()
	at, failed := v.windowsOf(ctx, inst.Ref)
	socket, anchor := at.socket, 0
	if at.window != nil {
		anchor, _ = activeWindow(*at.window)
	} else {
		if failed != nil {
			return false, fmt.Errorf("kitty: show %s: a workspace kitty did not answer, and may show it: %w", inst.Ref.ID, failed)
		}
		if !open {
			return false, nil
		}
		if socket, err = v.kitty.workspaceKitty(ctx); err != nil {
			return false, err
		}
	}
	opened := false
	for _, tab := range tabsOf(inst.Panels) {
		in := 0
		for _, p := range tab {
			if id, ok := at.panels[p.ID]; ok {
				in = id
				break
			}
		}
		for _, p := range tab {
			if _, ok := at.panels[p.ID]; ok {
				continue
			}
			var args []string
			switch {
			case anchor == 0:
				args = []string{"--type=os-window", "--os-window-name", inst.Title, "--os-window-title", inst.Title}
			case in == 0:
				args = []string{"--type=tab", "--location=last", "--match", "window_id:" + strconv.Itoa(anchor)}
			default:
				args = []string{"--type=window", "--match", "window_id:" + strconv.Itoa(in)}
			}
			if anchor != 0 && at.focused {
				args = append(args, "--keep-focus")
			}
			attach, err := v.Runtime.AttachPanel(p.ID)
			if err != nil {
				return opened, err
			}
			args = slices.Concat(args, []string{"--var", viewPanelVar + "=" + p.ID.String(), "--var", viewInstanceVar + "=" + inst.Ref.ID}, attach)
			id, err := v.kitty.launch(ctx, socket, args...)
			if err != nil {
				return opened, err
			}
			if anchor == 0 {
				anchor, opened = id, true
			}
			if in == 0 {
				in = id
			}
			at.panels[p.ID] = id
		}
	}
	return opened, nil
}

// showLockName is the file under revier's state directory whose lock one
// Show holds at a time. It is one file for every instance: a Show is a few
// launches, and a file per instance would stay behind for each.
const showLockName = "kitty-view.lock"

// showLock takes the lock Show runs under, waiting for a Show of another
// process to end, and returns its release.
func showLock() (func(), error) {
	root, err := state.Root()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(root, showLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// tabsOf is the panels of an instance by tab, in the order of the first
// panel of each, which is the runtime's order.
func tabsOf(panels []revier.Panel) [][]revier.Panel {
	var order []string
	tabs := map[string][]revier.Panel{}
	for _, p := range panels {
		if _, ok := tabs[p.Tab]; !ok {
			order = append(order, p.Tab)
		}
		tabs[p.Tab] = append(tabs[p.Tab], p)
	}
	out := make([][]revier.Panel, 0, len(order))
	for _, tab := range order {
		out = append(out, tabs[tab])
	}
	return out
}

// errNotShown is a runtime instance no kitty window shows.
var errNotShown = errors.New("no kitty window shows it")

// Focus makes the active kitty window of the instance's OS window current.
// Under GNOME on Wayland this does not raise the OS window; the core raises
// it through the window host.
func (v *View) Focus(ctx context.Context, ref revier.TargetRef) error {
	at, _ := v.windowsOf(ctx, ref)
	if at.window == nil {
		return fmt.Errorf("kitty: %s: %w", ref.ID, errNotShown)
	}
	win, ok := activeWindow(*at.window)
	if !ok {
		return fmt.Errorf("kitty: %s: %w", ref.ID, errNotShown)
	}
	_, err := v.kitty.kitten(ctx, at.socket, "focus-window", "--match", "id:"+strconv.Itoa(win))
	return err
}

// Focused reports the runtime instance whose OS window holds the keyboard,
// across the workspace kitties, by the var its kitty windows carry; zero when
// none does.
func (v *View) Focused(ctx context.Context) (revier.TargetRef, error) {
	for _, socket := range v.workspaces() {
		windows, err := v.kitty.ls(ctx, socket)
		if err != nil {
			continue
		}
		for _, w := range windows {
			if !w.IsFocused {
				continue
			}
			for _, t := range w.Tabs {
				for _, win := range t.Windows {
					if id := win.UserVars[viewInstanceVar]; id != "" {
						return revier.TargetRef{Host: v.Runtime.Name(), ID: id, Title: w.WMName}, nil
					}
				}
			}
		}
	}
	return revier.TargetRef{}, nil
}

// FocusPanel makes the kitty window that shows the panel current, switching
// to its tab. The OS window is raised by the core.
func (v *View) FocusPanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	at, _ := v.windowsOf(ctx, ref)
	win, ok := at.panels[panel]
	if !ok {
		return fmt.Errorf("kitty: panel %s of %s: %w", panel, ref.ID, errNotShown)
	}
	_, err := v.kitty.kitten(ctx, at.socket, "focus-window", "--match", "id:"+strconv.Itoa(win))
	return err
}

// FocusedPanel reports the panel that the current kitty window of the
// instance's OS window shows.
func (v *View) FocusedPanel(ctx context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	at, _ := v.windowsOf(ctx, ref)
	if at.window == nil {
		return "", fmt.Errorf("kitty: %s: %w", ref.ID, errNotShown)
	}
	win, _ := activeWindow(*at.window)
	for _, t := range at.window.Tabs {
		for _, w := range t.Windows {
			if w.ID == win {
				return revier.PanelID(w.UserVars[viewPanelVar]), nil
			}
		}
	}
	return "", nil
}

// FindPanel maps the kitty window id a key passes, in the kitty the key was
// pressed in, which KITTY_LISTEN_ON names, to the runtime panel that window
// attaches, and to the instance of instances that holds it. A window that
// attaches no panel, as kitty's own, is a zero ref: a tab outside a
// workspace opens beside it (decisions.md D183). An id that is no window of
// that kitty, or one passed from no kitty, is the runtime's to look up, as
// an hc address a script passes is (revier.ErrNoViewPanel).
func (v *View) FindPanel(ctx context.Context, instances []revier.Instance, panel revier.PanelID) (revier.TargetRef, revier.PanelID, error) {
	socket := v.kitty.here()
	if socket == "" {
		return revier.TargetRef{}, "", fmt.Errorf("kitty: the command runs in no kitty, as KITTY_LISTEN_ON is not set: %w", revier.ErrNoViewPanel)
	}
	windows, err := v.kitty.ls(ctx, socket)
	if err != nil {
		return revier.TargetRef{}, "", err
	}
	for _, w := range windows {
		for _, t := range w.Tabs {
			for _, win := range t.Windows {
				if strconv.Itoa(win.ID) != panel.String() {
					continue
				}
				own := revier.PanelID(win.UserVars[viewPanelVar])
				for _, in := range instances {
					if in.Ref.ID == win.UserVars[viewInstanceVar] && slices.ContainsFunc(in.Panels, func(p revier.Panel) bool { return p.ID == own }) {
						return in.Ref, own, nil
					}
				}
				return revier.TargetRef{}, "", nil
			}
		}
	}
	return revier.TargetRef{}, "", fmt.Errorf("kitty: window %s is none of %s: %w", panel, socket, revier.ErrNoViewPanel)
}

// OpenBeside opens a kitty tab beside the window panel, in the kitty the key
// was pressed in, running spec: kitty's own shell, or the agent spec runs,
// held as the kitty runtime holds a panel's command. It is kitty's alone, so
// the runtime lists no instance for it (decisions.md D183). Without spec.Dir
// it starts in the window's directory, read as the kitty runtime reads a
// panel's, while it is a directory: the runtime lists no such window, so the
// core cannot read it (D153, D164).
func (v *View) OpenBeside(ctx context.Context, panel revier.PanelID, spec revier.PanelSpec) error {
	socket := v.kitty.here()
	if socket == "" {
		return errors.New("kitty: the command runs in no kitty: KITTY_LISTEN_ON is not set")
	}
	dir := spec.Dir
	if dir == "" {
		var err error
		if dir, err = v.windowDir(ctx, socket, panel); err != nil {
			return err
		}
	}
	args := slices.Concat([]string{"--type=tab", "--match", "window_id:" + panel.String()}, holdArgs(spec))
	if dir != "" {
		args = append(args, "--cwd", dir)
	}
	_, err := v.kitty.launch(ctx, socket, append(args, spec.Command...)...)
	return err
}

// windowDir is the directory of the kitty window panel on socket, or "" when
// it is no directory now, as one removed since.
func (v *View) windowDir(ctx context.Context, socket string, panel revier.PanelID) (string, error) {
	windows, err := v.kitty.ls(ctx, socket)
	if err != nil {
		return "", err
	}
	parent := v.kitty.parents()
	for _, w := range windows {
		for _, t := range w.Tabs {
			for _, win := range t.Windows {
				if strconv.Itoa(win.ID) != panel.String() {
					continue
				}
				dir := panelOf(win, parent).Dir
				if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
					return "", nil
				}
				return dir, nil
			}
		}
	}
	return "", nil
}
