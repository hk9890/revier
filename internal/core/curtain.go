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

// OpenCurtains opens one curtain on each monitor, the primary one first, and
// focuses the primary one, so that its keys reach it (decisions.md D192).
// argv is the launch of the curtain for a monitor: a terminal of class
// CurtainClass with the title CurtainTitle gives it. Each curtain is waited
// for by its class and title, then covers its monitor. It returns the refs,
// primary first. A failure returns the error and the refs opened so far: the
// caller ends those curtains by closing the socket they read. The wait is
// ctx's to bound.
func (c *Core) OpenCurtains(ctx context.Context, argv func(revier.Monitor) []string) ([]revier.TargetRef, error) {
	start := time.Now()
	refs, err := c.openCurtains(ctx, argv)
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
	// user looks at before any other is waited for.
	slices.SortStableFunc(monitors, func(a, b revier.Monitor) int {
		return cmp.Compare(btoi(b.Primary), btoi(a.Primary))
	})
	var refs []revier.TargetRef
	for _, m := range monitors {
		ref, err := c.openCurtain(ctx, cov, m, argv(m))
		if err != nil {
			return refs, err
		}
		refs = append(refs, ref)
	}
	if len(refs) > 0 {
		if err := c.Window.Focus(ctx, refs[0]); err != nil {
			return refs, err
		}
	}
	return refs, nil
}

// openCurtain launches the curtain of one monitor, waits for its window and
// makes it cover the monitor.
func (c *Core) openCurtain(ctx context.Context, cov revier.Coverer, m revier.Monitor, launch []string) (revier.TargetRef, error) {
	title := CurtainTitle(m)
	before, err := c.Window.Instances(ctx)
	if err != nil {
		return revier.TargetRef{}, err
	}
	if _, err := c.Window.Open(ctx, revier.Realization{
		Launch: launch,
		Match:  revier.Match{Class: "^" + CurtainClass + "$", Title: "^" + title + "$"},
	}); err != nil {
		return revier.TargetRef{}, err
	}
	w, _, err := c.awaitNew(ctx, before, BindWait, popupPoll, func(fresh []revier.Instance) (revier.Instance, bool, bool) {
		i := slices.IndexFunc(fresh, func(w revier.Instance) bool { return w.Class == CurtainClass && w.Title == title })
		if i < 0 {
			return revier.Instance{}, false, false
		}
		return fresh[i], true, false
	})
	if errors.Is(err, errNoNewWindow) {
		return revier.TargetRef{}, fmt.Errorf("curtain: no window %s of class %s appeared in %s", title, CurtainClass, BindWait)
	}
	if err != nil {
		return revier.TargetRef{}, err
	}
	slog.Info("curtain: launched", "ref", w.Ref, "monitor", m.Index, "primary", m.Primary)
	if err := cov.Cover(ctx, w.Ref, m.Index); err != nil {
		return w.Ref, fmt.Errorf("curtain: cover monitor %d: %w", m.Index, err)
	}
	return w.Ref, nil
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
// stays hidden.
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
