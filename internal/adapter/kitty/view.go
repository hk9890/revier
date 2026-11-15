package kitty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

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
// that answer. A kitty that does not answer shows nothing.
func (v *View) windowsOf(ctx context.Context, ref revier.TargetRef) shown {
	found := shown{panels: map[revier.PanelID]int{}}
	for _, socket := range v.workspaces() {
		windows, err := v.kitty.ls(ctx, socket)
		if err != nil {
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
	return found
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
func (v *View) Show(ctx context.Context, inst revier.Instance, open bool) (bool, error) {
	if inst.Title == "" {
		return false, fmt.Errorf("kitty: %s has no title to give its window", inst.Ref.ID)
	}
	at := v.windowsOf(ctx, inst.Ref)
	socket, anchor := at.socket, 0
	if at.window != nil {
		anchor, _ = activeWindow(*at.window)
	} else {
		if !open {
			return false, nil
		}
		var err error
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
	at := v.windowsOf(ctx, ref)
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
	at := v.windowsOf(ctx, ref)
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
	at := v.windowsOf(ctx, ref)
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
// workspace opens beside it (decisions.md D183).
func (v *View) FindPanel(ctx context.Context, instances []revier.Instance, panel revier.PanelID) (revier.TargetRef, revier.PanelID, error) {
	socket := v.kitty.here()
	if socket == "" {
		return revier.TargetRef{}, "", errors.New("kitty: the command runs in no kitty: KITTY_LISTEN_ON is not set")
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
	return revier.TargetRef{}, "", nil
}

// OpenBeside opens a kitty tab beside the window panel, in the kitty the key
// was pressed in, running spec: kitty's own shell, or the agent spec runs,
// held as the kitty runtime holds a panel's command. It is kitty's alone, so
// the runtime lists no instance for it (decisions.md D183).
func (v *View) OpenBeside(ctx context.Context, panel revier.PanelID, spec revier.PanelSpec) error {
	socket := v.kitty.here()
	if socket == "" {
		return errors.New("kitty: the command runs in no kitty: KITTY_LISTEN_ON is not set")
	}
	args := slices.Concat([]string{"--type=tab", "--match", "window_id:" + panel.String()}, holdArgs(spec))
	if spec.Dir != "" {
		args = append(args, "--cwd", spec.Dir)
	}
	_, err := v.kitty.launch(ctx, socket, append(args, spec.Command...)...)
	return err
}
