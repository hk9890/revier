package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/pkg/revier"
)

// CurtainClass is the class each curtain's terminal is launched with. No
// window rule names it: the popup's overlay rule must not reach it.
const CurtainClass = "revier-curtain"

// curtainMain is the title of the curtain on the primary monitor, the one
// that shows the steps and holds the keyboard.
const curtainMain = CurtainClass + "-main"

// ErrNoCurtainHost means the window host cannot do what a curtain needs:
// list the monitors, cover one, hide a window and focus it. It is a normal
// outcome on a machine without one, and the run goes on with no curtain.
var ErrNoCurtainHost = errors.New("no window host that can cover the monitors")

// CurtainTitle is the title of the curtain on a monitor: "revier-curtain-main"
// for the primary monitor, "revier-curtain-<index>" for any other. A curtain
// is found by its class and its title.
func CurtainTitle(m revier.Monitor) string {
	if m.Primary {
		return curtainMain
	}
	return CurtainClass + "-" + strconv.Itoa(m.Index)
}

// curtainHost is the window host as the curtain needs it, or
// ErrNoCurtainHost.
func (c *Core) curtainHost() (revier.Coverer, revier.Hider, error) {
	if c.Window == nil {
		return nil, nil, ErrNoCurtainHost
	}
	cov, covers := c.Window.(revier.Coverer)
	hider, hides := c.Window.(revier.Hider)
	if !covers || !hides {
		return nil, nil, fmt.Errorf("%s: %w", c.Window.Name(), ErrNoCurtainHost)
	}
	return cov, hider, nil
}

// OpenCurtains opens one curtain on each monitor (decisions.md D192). It
// launches every curtain first, the primary one first, then waits for all
// their windows at once, so a slow terminal on one monitor does not spend the
// wait of the next. argv is the launch of the curtain for a monitor: a
// terminal of class CurtainClass with the title CurtainTitle gives it. Each
// curtain is found by its class and title, then covers its monitor, the
// primary one first; the primary one is focused last, so that its keys reach
// it. It returns the refs, primary first. A failure returns the error and the
// refs of the curtains found so far: the caller ends every curtain by closing
// the socket they read. The wait is ctx's to bound. Each monitor has a window:
// fullscreen covers one monitor, a maximized window goes under fullscreen
// windows, and the popup's own overlay rule minimizes it when the focus moves.
func (c *Core) OpenCurtains(ctx context.Context, argv func(revier.Monitor) []string) ([]revier.TargetRef, error) {
	start := time.Now()
	refs, err := c.openCurtains(ctx, argv)
	// Logs "curtain open" with refs, the curtains found, primary first, and
	// err when the open failed.
	logging.Op("curtain open", start, err, "refs", refs)
	return refs, err
}

func (c *Core) openCurtains(ctx context.Context, argv func(revier.Monitor) []string) ([]revier.TargetRef, error) {
	cov, _, err := c.curtainHost()
	if err != nil {
		return nil, err
	}
	monitors, err := cov.Monitors(ctx)
	if err != nil {
		return nil, fmt.Errorf("curtain: monitors: %w", err)
	}
	// The primary monitor's curtain is first, so it covers the screen the
	// user looks at before any other.
	slices.SortStableFunc(monitors, func(a, b revier.Monitor) int {
		return cmp.Compare(btoi(b.Primary), btoi(a.Primary))
	})
	before, err := c.Window.Instances(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range monitors {
		if _, err := c.Window.Open(ctx, revier.Realization{
			Launch: argv(m),
			Match:  revier.Match{Class: "^" + CurtainClass + "$", Title: "^" + CurtainTitle(m) + "$"},
		}); err != nil {
			return nil, err
		}
	}
	windows, err := c.awaitCurtains(ctx, before, monitors)
	refs := make([]revier.TargetRef, 0, len(monitors))
	for i, m := range monitors {
		if !windows[i].Ref.IsZero() {
			refs = append(refs, windows[i].Ref)
			// Logs "curtain: launched" for each curtain that came up, with
			// ref, monitor and primary.
			slog.Info("curtain: launched", "ref", windows[i].Ref, "monitor", m.Index, "primary", m.Primary)
		}
	}
	if err != nil {
		return refs, err
	}
	for i, m := range monitors {
		if err := cov.Cover(ctx, refs[i], m.Index); err != nil {
			return refs, fmt.Errorf("curtain: cover monitor %d: %w", m.Index, err)
		}
	}
	if len(refs) > 0 {
		if err := c.Window.Focus(ctx, refs[0]); err != nil {
			return refs, err
		}
	}
	return refs, nil
}

// awaitCurtains waits for the window of each monitor's curtain, found by its
// class and title, and returns them in the order of the monitors. A window
// that did not appear is the zero instance at its place, and the error names
// it.
func (c *Core) awaitCurtains(ctx context.Context, before []revier.Instance, monitors []revier.Monitor) ([]revier.Instance, error) {
	windows := make([]revier.Instance, len(monitors))
	all := func() bool {
		return !slices.ContainsFunc(windows, func(w revier.Instance) bool { return w.Ref.IsZero() })
	}
	_, _, err := c.awaitNew(ctx, before, BindWait, popupPoll, func(fresh []revier.Instance) (revier.Instance, bool, bool) {
		for i, m := range monitors {
			title := CurtainTitle(m)
			if j := slices.IndexFunc(fresh, func(w revier.Instance) bool { return w.Class == CurtainClass && w.Title == title }); j >= 0 {
				windows[i] = fresh[j]
			}
		}
		return revier.Instance{}, all(), false
	})
	if errors.Is(err, errNoNewWindow) || errors.Is(err, context.DeadlineExceeded) {
		var missing []string
		for i, m := range monitors {
			if windows[i].Ref.IsZero() {
				missing = append(missing, CurtainTitle(m))
			}
		}
		return windows, fmt.Errorf("curtain: no window %v of class %s appeared before the open's deadline: %w", missing, CurtainClass, err)
	}
	return windows, err
}

// HideCurtains takes every open curtain off the screen while the run goes on:
// each lets other windows over it, then is minimized. A minimized window that
// is still above keeps the focus from every window that maps under it. A
// curtain that fails to hide does not keep the others up.
func (c *Core) HideCurtains(ctx context.Context) error {
	start := time.Now()
	n, err := c.eachCurtain(ctx, func(cov revier.Coverer, hider revier.Hider, ref revier.TargetRef) error {
		if err := cov.Above(ctx, ref, false); err != nil {
			return err
		}
		return hider.Hide(ctx, ref)
	})
	// Logs "curtain hide" with curtains, the number it found: Esc on a
	// curtain while the walk runs.
	logging.Op("curtain hide", start, err, "curtains", n)
	return err
}

// ShowCurtains brings every open curtain back over every window: each is
// raised, then kept above. The main curtain is last, so it holds the
// keyboard.
func (c *Core) ShowCurtains(ctx context.Context) error {
	start := time.Now()
	n, err := c.eachCurtain(ctx, func(cov revier.Coverer, _ revier.Hider, ref revier.TargetRef) error {
		if err := c.Window.Focus(ctx, ref); err != nil {
			return err
		}
		return cov.Above(ctx, ref, true)
	})
	// Logs "curtain show" with curtains, the number it found: the end of a
	// walk that did not go well, or, after "popup: show the curtain", the
	// popup key that brings a hidden curtain back.
	logging.Op("curtain show", start, err, "curtains", n)
	return err
}

// eachCurtain runs do on each curtain the window host lists, the main one
// last, and reports how many it found. Every curtain gets its turn; the
// errors are joined.
func (c *Core) eachCurtain(ctx context.Context, do func(revier.Coverer, revier.Hider, revier.TargetRef) error) (int, error) {
	cov, hider, err := c.curtainHost()
	if err != nil {
		return 0, err
	}
	windows, err := c.Window.Instances(ctx)
	if err != nil {
		return 0, err
	}
	curtains := curtainWindows(windows)
	var errs []error
	for _, w := range curtains {
		if err := do(cov, hider, w.Ref); err != nil {
			errs = append(errs, fmt.Errorf("curtain %s: %w", w.Title, err))
		}
	}
	return len(curtains), errors.Join(errs...)
}

// FocusCurtain gives the keyboard back to the main curtain when it is open
// and not minimized: a restore's return home takes the focus under the
// curtain, and Esc and ctrl+c must reach the curtain again. A hidden curtain
// stays hidden. It logs "curtain focus" with ref, and err when the focus
// fails, or with err alone when the listing fails; a hidden or missing
// curtain, or a window host that cannot cover the monitors, logs nothing.
func (c *Core) FocusCurtain(ctx context.Context) error {
	if _, _, err := c.curtainHost(); err != nil {
		return err
	}
	start := time.Now()
	windows, err := c.Window.Instances(ctx)
	if err != nil {
		logging.Op("curtain focus", start, err)
		return err
	}
	i := slices.IndexFunc(windows, func(w revier.Instance) bool { return w.Class == CurtainClass && w.Title == curtainMain })
	if i < 0 || windows[i].Minimized {
		return nil
	}
	err = c.Window.Focus(ctx, windows[i].Ref)
	logging.Op("curtain focus", start, err, "ref", windows[i].Ref)
	return err
}

// curtainWindows are the curtains among the windows listed, the main one
// last.
func curtainWindows(windows []revier.Instance) []revier.Instance {
	var out []revier.Instance
	for _, w := range windows {
		if w.Class == CurtainClass {
			out = append(out, w)
		}
	}
	slices.SortStableFunc(out, func(a, b revier.Instance) int {
		return cmp.Compare(btoi(a.Title == curtainMain), btoi(b.Title == curtainMain))
	})
	return out
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
