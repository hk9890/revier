package hosttest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// FakeView is an in-memory View that draws a runtime it is not, in windows of
// its own, as kitty draws hc (decisions.md D180): an instance is shown once
// Show opened a window for it, and a window closed by hand is gone while the
// runtime runs on. It focuses only what it shows, reports the focus, maps a
// key's window id to a runtime panel, and opens a tab beside a window of its
// own. Its windows are no OS windows a window host lists. Construct it with
// NewView.
type FakeView struct {
	mu sync.Mutex

	name    string
	windows map[string][]revier.Panel // runtime ref id -> the panels shown, in order
	focused revier.TargetRef
	current map[string]revier.PanelID

	// Keys maps a window id a key passes to the runtime panel behind it.
	// An id it lacks is a window of the view's that shows no panel.
	Keys map[revier.PanelID]revier.PanelID

	// Opened is every instance Show opened a window for, in order.
	Opened []revier.TargetRef
	// Shows counts the calls of Show.
	Shows int
	// Beside is every tab OpenBeside opened, with the window it opened by.
	Beside []BesideTab
}

// BesideTab is one tab FakeView.OpenBeside opened.
type BesideTab struct {
	Panel revier.PanelID
	Spec  revier.PanelSpec
}

// NewView is a view with no window.
func NewView(name string) *FakeView {
	return &FakeView{name: name, windows: map[string][]revier.Panel{}, current: map[string]revier.PanelID{}}
}

func (v *FakeView) Name() string { return v.name }

func (v *FakeView) Probe(context.Context) error { return nil }

func (v *FakeView) Capabilities() revier.Capabilities { return revier.Capabilities{} }

// Show opens a window for the instance when open says it may and none shows
// it, and draws in its window every panel not shown yet.
func (v *FakeView) Show(_ context.Context, inst revier.Instance, open bool) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.Shows++
	shown, ok := v.windows[inst.Ref.ID]
	if !ok && !open {
		return false, nil
	}
	for _, p := range inst.Panels {
		if !slices.ContainsFunc(shown, func(s revier.Panel) bool { return s.ID == p.ID }) {
			shown = append(shown, p)
		}
	}
	v.windows[inst.Ref.ID] = shown
	if !ok {
		v.Opened = append(v.Opened, inst.Ref)
	}
	return !ok, nil
}

// CloseWindow closes the window that shows the instance, as a user closes a
// kitty window: the runtime's instance runs on.
func (v *FakeView) CloseWindow(ref revier.TargetRef) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.windows, ref.ID)
	delete(v.current, ref.ID)
	if v.focused.ID == ref.ID {
		v.focused = revier.TargetRef{}
	}
}

// Showing reports the panels the instance's window shows, and whether a
// window shows it.
func (v *FakeView) Showing(ref revier.TargetRef) ([]revier.PanelID, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	shown, ok := v.windows[ref.ID]
	var ids []revier.PanelID
	for _, p := range shown {
		ids = append(ids, p.ID)
	}
	return ids, ok
}

// Focus focuses the window of an instance it shows, and fails for one it
// does not.
func (v *FakeView) Focus(_ context.Context, ref revier.TargetRef) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.windows[ref.ID]; !ok {
		return fmt.Errorf("%s: no window shows %s", v.name, ref.ID)
	}
	v.focused = ref
	return nil
}

func (v *FakeView) Focused(context.Context) (revier.TargetRef, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.focused, nil
}

// FocusPanel makes a panel its window shows current, and fails for one it
// does not show.
func (v *FakeView) FocusPanel(_ context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !slices.ContainsFunc(v.windows[ref.ID], func(p revier.Panel) bool { return p.ID == panel }) {
		return fmt.Errorf("%s: no window shows panel %s of %s", v.name, panel, ref.ID)
	}
	v.current[ref.ID] = panel
	return nil
}

func (v *FakeView) FocusedPanel(_ context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.current[ref.ID], nil
}

// FindPanel maps a key's window id through Keys to the instance that holds
// the runtime panel behind it.
func (v *FakeView) FindPanel(_ context.Context, instances []revier.Instance, panel revier.PanelID) (revier.TargetRef, revier.PanelID, error) {
	v.mu.Lock()
	own, ok := v.Keys[panel]
	v.mu.Unlock()
	if !ok {
		return revier.TargetRef{}, "", nil
	}
	for _, in := range instances {
		if slices.ContainsFunc(in.Panels, func(p revier.Panel) bool { return p.ID == own }) {
			return in.Ref, own, nil
		}
	}
	return revier.TargetRef{}, "", nil
}

// OpenBeside records the tab.
func (v *FakeView) OpenBeside(_ context.Context, panel revier.PanelID, spec revier.PanelSpec) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.Beside = append(v.Beside, BesideTab{Panel: panel, Spec: spec})
	return nil
}
