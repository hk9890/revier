// Package gnome implements a WindowController over wctl, the CLI of the
// gnome-window-control GNOME Shell extension.
//
// GNOME is the hardest of the compositors to support and the reason wctl
// exists: it exposes no window list to an outside process, so the extension
// supplies one. That also makes it the only adapter whose tests need a live
// logged-in session - wctl talks to a running Shell - which is why
// docs/TESTING.md confines the manual layer to this package.
//
// wctl reports no events - `wctl wait` answers once, with the first window
// that matches, open or new - so claim-on-appear diffs successive Instances
// calls on the refresh the TUI already runs (decisions.md D87).
package gnome

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/hk9890/revier/pkg/revier"
)

// Host is a GNOME WindowController.
type Host struct {
	// run runs wctl with the arguments and returns its standard output, also
	// beside the error of a wctl that exits non-zero. Tests replace it;
	// production leaves it nil and runs the wctl on PATH.
	run func(ctx context.Context, args ...string) ([]byte, error)

	// quietHide and quietFocus are whether the extension the shell has loaded
	// minimizes and activates without the animation, as Probe read it. A host
	// that was never probed asks for the animation.
	quietHide, quietFocus bool
}

// minWctl is the oldest wctl this host drives.
var minWctl = [3]int{0, 16, 0}

// versionDoc is what `wctl version --json` prints, on every outcome.
type versionDoc struct {
	Wctl string `json:"wctl"`
	// Extension is the version the shell has loaded, nil when no extension
	// answers.
	Extension    *string  `json:"extension"`
	Capabilities []string `json:"capabilities"`
	Message      string   `json:"message"`
}

func (h *Host) Name() string { return "gnome" }

// window is wctl's JSON shape. Only the fields revier uses are decoded; wctl
// reports geometry and state flags that a project switcher has no use for.
type window struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	WMClass     string `json:"wm_class"`
	PID         int    `json:"pid"`
	IsHidden    bool   `json:"is_hidden"`
	IsMinimized bool   `json:"is_minimized"`
	// Type is mutter's window type: "normal" for an application's main
	// window, else a dialog, a splash or a menu.
	Type string `json:"window_type_name"`
	// Workspace is -1 for a window on no workspace, and for one on all of
	// them; nil only from a wctl that does not report it.
	Workspace *int `json:"workspace_index"`
}

// unmapped reports whether a window is hidden for the one reason activate
// cannot undo: mutter has created it and not yet shown it. A minimized window
// and a window on another workspace are hidden too, and activate restores the
// first and switches to the second, so both are windows revier can raise.
// wctl reads an unmapped window the same way, as hidden while unminimized on
// the active workspace; active is asked only for such a window.
//
// A window whose workspace cannot be told from the active one is kept. Kept,
// an unshown window can be placed before mutter places it, for the moment it
// takes to show; dropped, a window on another workspace is not found, and its
// key fails.
func (w window) unmapped(active func() (int, bool)) bool {
	if !w.IsHidden || w.IsMinimized {
		return false
	}
	if w.Workspace == nil || *w.Workspace < 0 {
		return true
	}
	at, ok := active()
	return ok && *w.Workspace == at
}

// shownWorkspace is the active workspace as the listing itself shows it: every
// shown window on one workspace is on the active one. A sticky window reports
// -1 and says nothing.
func shownWorkspace(windows []window) (int, bool) {
	for _, w := range windows {
		if !w.IsHidden && w.Workspace != nil && *w.Workspace >= 0 {
			return *w.Workspace, true
		}
	}
	return 0, false
}

// Probe reports GNOME usable when wctl is on PATH, is minWctl or later, and
// names the extension the shell has loaded. It also checks the desktop, so
// that a wctl left on PATH after a session change does not make this host
// claim a compositor it cannot drive. What the extension can do without the
// animation is read here, once a process.
//
// wctl exits non-zero when the loaded extension is outside the versions it
// was built for, and prints the same document. That stack still works: an
// upgrade the shell has not reloaded yet is one. So the exit decides nothing
// while the document names an extension.
func (h *Host) Probe(ctx context.Context) error {
	if _, err := exec.LookPath("wctl"); err != nil {
		return fmt.Errorf("wctl not on PATH: %w", err)
	}
	if d := os.Getenv("XDG_CURRENT_DESKTOP"); d != "" && !strings.Contains(strings.ToUpper(d), "GNOME") {
		return fmt.Errorf("XDG_CURRENT_DESKTOP is %q, not GNOME", d)
	}
	raw, err := h.wctl(ctx, "version", "--json")
	var doc versionDoc
	if json.Unmarshal(bytes.TrimSpace(raw), &doc) != nil || doc.Wctl == "" {
		if err != nil {
			return fmt.Errorf("wctl is present but not answering: %w", err)
		}
		return fmt.Errorf("wctl version --json names no version: revier needs wctl %s or later", minWctlText())
	}
	if !atLeast(doc.Wctl, minWctl) {
		return fmt.Errorf("wctl %s is older than %s, the oldest revier drives", doc.Wctl, minWctlText())
	}
	if doc.Extension == nil {
		return fmt.Errorf("wctl is present but no extension answers: %s", doc.Message)
	}
	h.quietHide = slices.Contains(doc.Capabilities, "no-animation")
	h.quietFocus = slices.Contains(doc.Capabilities, "activate-no-animation")
	return nil
}

func minWctlText() string { return fmt.Sprintf("%d.%d.%d", minWctl[0], minWctl[1], minWctl[2]) }

// atLeast reports whether a version such as "0.16.0" is oldest or later. A
// pre-release or build suffix is ignored, and a version that does not parse
// is not.
func atLeast(version string, oldest [3]int) bool {
	core, _, _ := strings.Cut(strings.SplitN(version, "+", 2)[0], "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		v[i] = n
	}
	return slices.Compare(v[:], oldest[:]) >= 0
}

func (h *Host) wctl(ctx context.Context, args ...string) ([]byte, error) {
	if h.run != nil {
		return h.run(ctx, args...)
	}
	return execRun(ctx, "wctl", args...)
}

// Instances lists every window in one call. The active workspace comes from
// that listing, and a second call asks for it only when no shown window says,
// so a refresh costs one invocation in nearly every case and at most two,
// regardless of how many projects exist.
func (h *Host) Instances(ctx context.Context) ([]revier.Instance, error) {
	raw, err := h.wctl(ctx, "list", "--json")
	if err != nil {
		return nil, err
	}
	return h.decode(raw, h.activeWorkspace(ctx))
}

// activeWorkspace asks wctl for the active workspace once, the first time it
// is needed: a desktop with a hidden window and no shown one to read it from.
func (h *Host) activeWorkspace(ctx context.Context) func() (int, bool) {
	var at int
	var ok, asked bool
	return func() (int, bool) {
		if asked {
			return at, ok
		}
		asked = true
		raw, err := h.wctl(ctx, "workspaces", "--json")
		if err != nil {
			return 0, false
		}
		var spaces []struct {
			Index    int  `json:"index"`
			IsActive bool `json:"is_active"`
		}
		if json.Unmarshal(raw, &spaces) != nil {
			return 0, false
		}
		for _, s := range spaces {
			if s.IsActive {
				at, ok = s.Index, true
			}
		}
		return at, ok
	}
}

func (h *Host) decode(raw []byte, active func() (int, bool)) ([]revier.Instance, error) {
	var windows []window
	if err := json.Unmarshal(raw, &windows); err != nil {
		return nil, fmt.Errorf("wctl list --json: %w", err)
	}
	if at, ok := shownWorkspace(windows); ok {
		active = func() (int, bool) { return at, true }
	}
	out := make([]revier.Instance, 0, len(windows))
	for _, w := range windows {
		if w.unmapped(active) {
			// Not shown yet, so not placed: a match would place it before
			// mutter does, and mutter's own placement would then win.
			continue
		}
		if w.Type != "" && w.Type != "normal" {
			// A splash or a dialog shares its application's class, and a
			// launch would bind it in place of the window it waits for.
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
	// Detach: the launched application outlives the revier process that
	// started it, and must not die when a keypress-sized process exits. So no
	// context, which would kill it when the keypress ends, and a session of
	// its own, so the hangup of the terminal revier runs in does not reach it.
	c := exec.Command(r.Launch[0], r.Launch[1:]...)
	c.Dir = r.Dir
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return revier.TargetRef{}, fmt.Errorf("gnome: launch %s: %w", r.Launch[0], err)
	}
	go func() { _ = c.Wait() }() // reap, so the child does not linger as a zombie
	return revier.TargetRef{}, nil
}

// Focus activates the window, raising it. wctl also has `focus`, which sets
// keyboard focus without raising; revier wants the window in front, so this
// uses activate.
//
// --no-animation restores a minimized window without the shell's animation,
// in the same call, so a hidden popup comes back in place. It is sent where
// the loaded extension reports 'activate-no-animation'.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("gnome: cannot focus a zero ref")
	}
	return h.onWindow(ctx, "activate", ref.ID, h.quietFocus)
}

// Close asks the window to close, as its close button does: an application
// with unsaved work may keep it open to ask.
func (h *Host) Close(ctx context.Context, ref revier.TargetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("gnome: cannot close a zero ref")
	}
	_, err := h.wctl(ctx, "close", ref.ID)
	return err
}

// Hide minimizes the window without the shell's minimize animation, so it
// leaves in place instead of shrinking into a corner. activate restores a
// minimized window, so Focus brings it back. --no-animation is sent where the
// loaded extension reports 'no-animation'; elsewhere the window minimizes
// with the animation.
func (h *Host) Hide(ctx context.Context, ref revier.TargetRef) error {
	if ref.ID == "" {
		return fmt.Errorf("gnome: cannot hide a zero ref")
	}
	return h.onWindow(ctx, "minimize", ref.ID, h.quietHide)
}

// onWindow runs a wctl command on a window, with --no-animation when quiet.
func (h *Host) onWindow(ctx context.Context, command, id string, quiet bool) error {
	args := []string{command, id}
	if quiet {
		args = append(args, "--no-animation")
	}
	_, err := h.wctl(ctx, args...)
	return err
}

// Place positions a window through the extension and returns once the move
// is applied, without waiting for the frame to come to rest. The window is
// one mutter has shown, as decode drops one it has not, unless decode cannot
// tell the active workspace and keeps an unshown window (see unmapped), which
// mutter may then place over the move. An application
// that resizes its window after the move, as kitty snaps to its cell grid,
// has the last word either way: waiting for the frame moves nothing back.
//
// A refusal is not an error the caller should act on: a window pinned by
// maximize, fullscreen or tiling keeps its geometry, and the launch it
// followed succeeded either way.
func (h *Host) Place(ctx context.Context, ref revier.TargetRef, geometry []string) error {
	if ref.ID == "" {
		return fmt.Errorf("gnome: cannot place a zero ref")
	}
	if len(geometry) != 4 {
		return fmt.Errorf("gnome: place needs four tokens, got %d", len(geometry))
	}
	_, err := h.wctl(ctx, append([]string{"place", ref.ID}, geometry...)...)
	return err
}

// WorkareaWidth reports the width of the primary monitor's usable area, the
// rectangle wctl resolves a placement's percentages against.
func (h *Host) WorkareaWidth(ctx context.Context) (int, error) {
	raw, err := h.wctl(ctx, "workarea", "--json")
	if err != nil {
		return 0, err
	}
	return decodeWorkareaWidth(raw)
}

func decodeWorkareaWidth(raw []byte) (int, error) {
	var area struct {
		Width int `json:"width"`
	}
	if err := json.Unmarshal(raw, &area); err != nil {
		return 0, fmt.Errorf("wctl workarea --json: %w", err)
	}
	if area.Width <= 0 {
		return 0, fmt.Errorf("wctl workarea --json: no width in %s", bytes.TrimSpace(raw))
	}
	return area.Width, nil
}

// Focused reports the focused window.
func (h *Host) Focused(ctx context.Context) (revier.TargetRef, error) {
	raw, err := h.wctl(ctx, "focused", "--json")
	if err != nil {
		return revier.TargetRef{}, err
	}
	return h.decodeFocused(raw)
}

// noFocus is what wctl 0.16 and older print when no window has the focus:
// plain text with exit 0, with --json too. wctl 0.17 prints null.
const noFocus = "No window focused"

func (h *Host) decodeFocused(raw []byte) (revier.TargetRef, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(noFocus)) {
		// Nothing focused is a normal desktop state, not a failure.
		return revier.TargetRef{}, nil
	}
	var w window
	if err := json.Unmarshal(trimmed, &w); err != nil {
		return revier.TargetRef{}, fmt.Errorf("wctl focused --json: %w", err)
	}
	if w.ID == 0 {
		return revier.TargetRef{}, nil
	}
	return revier.TargetRef{
		Host: h.Name(), ID: strconv.FormatInt(w.ID, 10), Title: w.Title,
	}, nil
}
