// Package gnome implements a WindowController over wctl, the CLI of the
// gnome-window-control GNOME Shell extension.
//
// GNOME is the hardest of the compositors to support and the reason wctl
// exists: it exposes no window list to an outside process, so the extension
// supplies one. That also makes it the only adapter whose tests need a live
// logged-in session - wctl talks to a running Shell - which is why
// docs/TESTING.md confines the manual layer to this package.
//
// wctl 0.7.0 has no event or watch command, so this host does not implement
// WindowWatcher. Claim-on-appear, when it is built, has to diff successive
// Instances calls on the refresh the TUI already runs.
package gnome

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

// Host is a GNOME WindowController.
type Host struct {
	// Bin overrides the wctl binary, for tests.
	Bin string
}

func (h *Host) Name() string { return "gnome" }

func (h *Host) bin() string {
	if h.Bin != "" {
		return h.Bin
	}
	return "wctl"
}

// window is wctl's JSON shape. Only the fields revier uses are decoded; wctl
// reports geometry and state flags that a project switcher has no use for.
type window struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	WMClass  string `json:"wm_class"`
	PID      int    `json:"pid"`
	HasFocus bool   `json:"has_focus"`
	// AppearsFocused is set for a window that looks focused to the user even
	// when the compositor gives keyboard focus elsewhere, which happens with
	// override-redirect popups. Focused() prefers has_focus and falls back to
	// it, so a popup open over the workspace does not read as "nothing
	// focused".
	AppearsFocused bool `json:"appears_focused"`
	IsHidden       bool `json:"is_hidden"`
}

// Probe reports GNOME usable when wctl is on PATH and answers. It also checks
// the desktop, so that a wctl left on PATH after a session change does not
// make this host claim a compositor it cannot drive.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := exec.LookPath(h.bin()); err != nil {
		return fmt.Errorf("wctl not on PATH: %w", err)
	}
	if d := os.Getenv("XDG_CURRENT_DESKTOP"); d != "" && !strings.Contains(strings.ToUpper(d), "GNOME") {
		return fmt.Errorf("XDG_CURRENT_DESKTOP is %q, not GNOME", d)
	}
	if _, err := h.run(ctx, "list", "--json"); err != nil {
		return fmt.Errorf("wctl is present but not answering: %w", err)
	}
	return nil
}

func (h *Host) run(ctx context.Context, args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, h.bin(), args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("wctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Instances lists every window in one call. wctl returns the whole list, so a
// refresh costs exactly one invocation regardless of how many projects exist.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	raw, err := h.run(ctx, "list", "--json")
	if err != nil {
		return nil, err
	}
	return h.decode(raw)
}

func (h *Host) decode(raw []byte) ([]revier.Instance, error) {
	var windows []window
	if err := json.Unmarshal(raw, &windows); err != nil {
		return nil, fmt.Errorf("wctl list --json: %w", err)
	}
	out := make([]revier.Instance, 0, len(windows))
	for _, w := range windows {
		if w.IsHidden {
			// A hidden window cannot be activated, so offering it as a match
			// would produce a keypress that appears to do nothing.
			continue
		}
		out = append(out, revier.Instance{
			Ref:   revier.TargetRef{Host: h.Name(), ID: strconv.FormatInt(w.ID, 10), Title: w.Title},
			Title: w.Title,
			Class: w.WMClass,
			PID:   w.PID,
		})
	}
	return out, nil
}

// Open launches the argv and returns a zero ref: the window does not exist yet
// when the process starts, and correlating a launch to the window it produces
// is claim-on-appear's job, not Open's. The core's next Instances call finds
// the window through the realization's match, which is exactly why Open owes
// Match a recognisable window - a browser needs its --class, an editor needs a
// title carrying the project name.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if len(r.Launch) == 0 {
		return revier.TargetRef{}, fmt.Errorf("gnome: realization has no launch argv")
	}
	c := exec.CommandContext(ctx, r.Launch[0], r.Launch[1:]...)
	c.Dir = r.Dir
	// Detach: the launched application outlives the revier process that
	// started it, and must not die when a keypress-sized process exits.
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Start(); err != nil {
		return revier.TargetRef{}, fmt.Errorf("gnome: launch %s: %w", r.Launch[0], err)
	}
	go func() { _ = c.Wait() }() // reap, so the child does not linger as a zombie
	return revier.TargetRef{}, nil
}

// Focus activates the window, raising it. wctl also has `focus`, which sets
// keyboard focus without raising; revier wants the window in front, so this
// uses activate.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("gnome: cannot focus a zero ref")
	}
	_, err := h.run(ctx, "activate", ref.ID)
	return err
}

// Focused reports the focused window.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	raw, err := h.run(ctx, "focused", "--json")
	if err != nil {
		return revier.TargetRef{}, err
	}
	return h.decodeFocused(raw)
}

func (h *Host) decodeFocused(raw []byte) (revier.TargetRef, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		// Nothing focused is a normal desktop state, not a failure.
		return revier.TargetRef{}, nil
	}
	var w window
	if err := json.Unmarshal(trimmed, &w); err != nil {
		// wctl focused --json may answer with a single-element array.
		var list []window
		if err2 := json.Unmarshal(trimmed, &list); err2 != nil {
			return revier.TargetRef{}, fmt.Errorf("wctl focused --json: %w", err)
		}
		if len(list) == 0 {
			return revier.TargetRef{}, nil
		}
		w = list[0]
	}
	if w.ID == 0 {
		return revier.TargetRef{}, nil
	}
	return revier.TargetRef{
		Host: h.Name(), ID: strconv.FormatInt(w.ID, 10), Title: w.Title,
	}, nil
}
