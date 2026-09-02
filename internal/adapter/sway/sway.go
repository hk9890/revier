// Package sway implements a WindowController over swaymsg, and the first
// WindowWatcher.
//
// sway exposes real IPC: one `get_tree` lists every window, `focus` raises
// one, and `subscribe` streams window events. That makes it the window host
// that can be driven with no display at all - `WLR_BACKENDS=headless sway` is
// a real compositor - and so the substrate layer L5 runs against
// (docs/TESTING.md). GNOME cannot be tested this way, which is why this host
// exists second and proves the abstraction.
//
// An instance is a window node of the tree: a Wayland window carries app_id,
// an XWayland window carries window_properties.class. Both land in
// Instance.Class so one `match = { class = ... }` rule serves both.
package sway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Host is a sway WindowController.
type Host struct {
	// Socket overrides $SWAYSOCK, for a test that starts its own compositor.
	Socket string
	// Env is added to the environment of every launched client, for the same
	// test: the client must find that compositor's WAYLAND_DISPLAY and
	// XDG_RUNTIME_DIR, not the machine's.
	Env []string
}

func (h *Host) Name() string { return "sway" }

// node is the subset of a sway tree node this host reads.
type node struct {
	ID       int64   `json:"id"`
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	AppID    *string `json:"app_id"`
	PID      int     `json:"pid"`
	Focused  bool    `json:"focused"`
	Nodes    []node  `json:"nodes"`
	Floating []node  `json:"floating_nodes"`
	Window   *struct {
		Class string `json:"class"`
	} `json:"window_properties"`
}

// isWindow reports a leaf container that holds an application window.
func (n node) isWindow() bool {
	if n.Type != "con" && n.Type != "floating_con" {
		return false
	}
	if len(n.Nodes) > 0 || len(n.Floating) > 0 {
		return false
	}
	return n.AppID != nil || n.Window != nil || n.PID != 0
}

func (n node) class() string {
	if n.AppID != nil && *n.AppID != "" {
		return *n.AppID
	}
	if n.Window != nil {
		return n.Window.Class
	}
	return ""
}

func (h *Host) args(rest ...string) []string {
	if h.Socket != "" {
		return append([]string{"-s", h.Socket}, rest...)
	}
	return rest
}

func (h *Host) run(ctx context.Context, rest ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, "swaymsg", h.args(rest...)...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("swaymsg %s: %w: %s", strings.Join(rest, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Probe reports sway usable when a socket is known and answers.
func (h *Host) Probe(ctx context.Context) error {
	if h.Socket == "" && os.Getenv("SWAYSOCK") == "" {
		return fmt.Errorf("SWAYSOCK is not set")
	}
	if _, err := exec.LookPath("swaymsg"); err != nil {
		return fmt.Errorf("swaymsg not on PATH: %w", err)
	}
	if _, err := h.run(ctx, "-t", "get_version"); err != nil {
		return fmt.Errorf("sway is not answering: %w", err)
	}
	return nil
}

// Instances lists every window in one get_tree.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	raw, err := h.run(ctx, "-t", "get_tree")
	if err != nil {
		return nil, err
	}
	return h.decode(raw)
}

func (h *Host) decode(raw []byte) ([]revier.Instance, error) {
	var root node
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("swaymsg -t get_tree: %w", err)
	}
	var out []revier.Instance
	var walk func(n node)
	walk = func(n node) {
		if n.isWindow() {
			out = append(out, h.instance(n))
			return
		}
		for _, c := range n.Nodes {
			walk(c)
		}
		for _, c := range n.Floating {
			walk(c)
		}
	}
	walk(root)
	return out, nil
}

func (h *Host) instance(n node) revier.Instance {
	return revier.Instance{
		Ref:   revier.TargetRef{Host: h.Name(), ID: strconv.FormatInt(n.ID, 10), Title: n.Name},
		Title: n.Name,
		Class: n.class(),
		PID:   n.PID,
	}
}

// Open launches the argv detached and returns a zero ref, as the GNOME host
// does: the window does not exist yet, and the next Instances call finds it
// through the realization's match.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if len(r.Launch) == 0 {
		return revier.TargetRef{}, fmt.Errorf("sway: realization has no launch argv")
	}
	c := exec.Command(r.Launch[0], r.Launch[1:]...)
	c.Dir = r.Dir
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if len(h.Env) > 0 {
		c.Env = append(os.Environ(), h.Env...)
	}
	if err := c.Start(); err != nil {
		return revier.TargetRef{}, fmt.Errorf("sway: launch %s: %w", r.Launch[0], err)
	}
	go func() { _ = c.Wait() }()
	return revier.TargetRef{}, nil
}

// Focus raises the window: sway's focus brings a container forward.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("sway: cannot focus a zero ref")
	}
	_, err := h.run(ctx, "[con_id="+ref.ID+"]", "focus")
	return err
}

// Focused reports the focused window, or a zero ref when none is. One
// get_tree: the focused node is in the same tree Instances reads.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	raw, err := h.run(ctx, "-t", "get_tree")
	if err != nil {
		return revier.TargetRef{}, err
	}
	var root node
	if err := json.Unmarshal(raw, &root); err != nil {
		return revier.TargetRef{}, fmt.Errorf("swaymsg -t get_tree: %w", err)
	}
	if n, ok := focusedWindow(root); ok {
		return h.instance(n).Ref, nil
	}
	return revier.TargetRef{}, nil
}

func focusedWindow(n node) (node, bool) {
	if n.isWindow() {
		return n, n.Focused
	}
	for _, c := range n.Nodes {
		if w, ok := focusedWindow(c); ok {
			return w, true
		}
	}
	for _, c := range n.Floating {
		if w, ok := focusedWindow(c); ok {
			return w, true
		}
	}
	return node{}, false
}

// event is one line of `swaymsg -t subscribe -m '["window"]'`.
type event struct {
	Change    string `json:"change"`
	Container node   `json:"container"`
}

// Watch streams window events until ctx ends. It is the capability
// claim-on-appear needs, and sway is the first host that has it.
func (h *Host) Watch(ctx context.Context) (<-chan revier.WindowEvent, error) {
	c := exec.CommandContext(ctx, "swaymsg", h.args("-r", "-m", "-t", "subscribe", `["window"]`)...)
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("sway: subscribe: %w", err)
	}
	out := make(chan revier.WindowEvent)
	go func() {
		defer close(out)
		defer func() { _ = c.Wait() }()
		dec := json.NewDecoder(stdout)
		for {
			var ev event
			if err := dec.Decode(&ev); err != nil {
				return
			}
			kind, ok := kindOf(ev.Change)
			if !ok {
				continue
			}
			select {
			case out <- revier.WindowEvent{Kind: kind, Instance: h.instance(ev.Container)}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func kindOf(change string) (revier.WindowEventKind, bool) {
	switch change {
	case "new":
		return revier.WindowOpened, true
	case "close":
		return revier.WindowClosed, true
	case "focus":
		return revier.WindowFocused, true
	}
	return 0, false
}

// Decode exposes the tree parser to the L3 tests.
func Decode(raw []byte) ([]revier.Instance, error) { return (&Host{}).decode(raw) }

// DecodeEvents parses a recorded subscribe stream, for the L3 tests.
func DecodeEvents(raw []byte) ([]revier.WindowEvent, error) {
	h := &Host{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out []revier.WindowEvent
	for dec.More() {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			return nil, err
		}
		if kind, ok := kindOf(ev.Change); ok {
			out = append(out, revier.WindowEvent{Kind: kind, Instance: h.instance(ev.Container)})
		}
	}
	return out, nil
}
