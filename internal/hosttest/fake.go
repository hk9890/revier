// Package hosttest provides the fake Host that layer L2 runs against.
//
// It is the reason most of revier can be tested without a terminal, a
// compositor, or a screen: the core's behaviour - resolution, matching,
// run-or-raise, toggle-back - depends only on what a host reports, and a fake
// reports whatever a test needs. See docs/TESTING.md for the layer model.
package hosttest

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/hk9890/revier/pkg/revier"
)

// Fake is an in-memory Host. The zero value is unusable; construct it with New.
type Fake struct {
	mu sync.Mutex

	name      string
	instances []revier.Instance
	focused   revier.TargetRef
	nextID    int

	// ProbeErr makes Probe fail, so a test can exercise selection falling
	// through to the next adapter.
	ProbeErr error
	// InstancesErr makes Instances fail, for the survey error path.
	InstancesErr error
	// OpenErr makes Open fail, for the run half of run-or-raise.
	OpenErr error
	// FocusErr makes Focus fail and FocusedErr makes Focused fail, for the
	// raise half. PlaceErr makes Place fail.
	FocusErr   error
	FocusedErr error
	PlaceErr   error
	// Detached makes Open launch without a ref, as a window host does: the
	// window does not exist yet when the process starts, so there is nothing
	// to name.
	Detached bool
	// OnClose runs after a close is recorded and before it takes effect, so
	// a test can make a host go away in the middle of a shutdown.
	OnClose func(revier.TargetRef)

	// Opened records every realization passed to Open, in order. A test
	// asserts on it to prove the core rendered templates before the host saw
	// them, and that run-or-raise ran rather than raised.
	Opened []revier.Realization
	// Focuses records every ref passed to Focus, in order.
	Focuses []revier.TargetRef
	// InstancesCalls counts Instances calls, so a test can assert that a
	// refresh costs one call whatever the project count.
	InstancesCalls int
	// Placements records every geometry passed to Place, by ref. A window
	// host that cannot place windows leaves this nil, which is the
	// degradation a test also has to cover.
	Placements map[string][]string
	// Workarea is the width WorkareaWidth reports, and WorkareaErr makes it
	// fail.
	Workarea    int
	WorkareaErr error

	// Sent records every text a FakeRuntime was asked to type, in order.
	Sent []Sent
	// Screens is what a FakeRuntime's ReadPanel answers, by panel, and
	// ScreenErr makes it fail. ScreenReads records every call, in order.
	Screens     map[revier.PanelID]string
	ScreenErr   error
	ScreenReads []ScreenRead
	// Tabs records every tab a FakeRuntime was asked to open, and
	// PanelFocuses every panel it was asked to focus, in order.
	Tabs         []Tab
	PanelFocuses []revier.PanelID
	// OpenTabErr makes OpenTab fail, for a tab that does not open in an
	// instance that did.
	OpenTabErr error
	current    map[string]revier.PanelID
	// OnSend runs after each SendText, so a test can make the agent react to
	// its prompt the way a real one does.
	OnSend func(panel revier.PanelID, text string)
	// SendErr makes SendText fail once Sent holds SendsBeforeErr calls, so a
	// test can lose the Enter after the text arrived. The count is of every
	// call in Sent, the ones from before SendErr was set included. A call
	// that fails types nothing: it is not in Sent and OnSend does not run.
	SendErr        error
	SendsBeforeErr int
	// FocusPanelErr makes FocusPanel fail and FocusedPanelErr makes
	// FocusedPanel fail, for a runtime that goes away between opening a tab
	// and showing it. A FocusPanel that fails is still in PanelFocuses.
	FocusPanelErr   error
	FocusedPanelErr error

	// Closed records every ref passed to Close and ClosedPanels every panel
	// passed to ClosePanel, in order. CloseErr makes both fail. Refuses holds
	// the instance ids a Close leaves listed.
	Closed       []revier.TargetRef
	ClosedPanels []revier.PanelID
	CloseErr     error
	Refuses      map[string]bool
	// Hidden records every ref passed to Hide, in order. HideErr makes it
	// fail.
	Hidden  []revier.TargetRef
	HideErr error

	caps revier.Capabilities
}

// New returns a fake host with the given name.
func New(name string) *Fake {
	return &Fake{name: name}
}

// NewRuntime returns a fake that satisfies revier.Runtime.
func NewRuntime(name string) *FakeRuntime { return &FakeRuntime{Fake: New(name)} }

// FakeRuntime is a Fake that also reports capabilities.
type FakeRuntime struct{ *Fake }

func (f *FakeRuntime) Capabilities() revier.Capabilities { return f.caps }

// SetCapabilities changes what the runtime reports, for tests of a runtime
// whose instances are OS windows.
func (f *FakeRuntime) SetCapabilities(c revier.Capabilities) { f.caps = c }

// Sent is one SendText call.
type Sent struct {
	Ref   revier.TargetRef
	Panel revier.PanelID
	Text  string
}

// SendText records the text, then runs OnSend, or fails with SendErr. FakeRuntime implements
// revier.PanelWriter; a runtime without the capability is a different double.
func (f *FakeRuntime) SendText(_ context.Context, ref revier.TargetRef, panel revier.PanelID, text string) error {
	f.mu.Lock()
	if f.SendErr != nil && len(f.Sent) >= f.SendsBeforeErr {
		f.mu.Unlock()
		return f.SendErr
	}
	f.Sent = append(f.Sent, Sent{Ref: ref, Panel: panel, Text: text})
	on := f.OnSend
	f.mu.Unlock()
	if on != nil {
		on(panel, text)
	}
	return nil
}

// ReadPanel reports Screens for the panel, or ScreenErr. FakeRuntime
// implements revier.PanelReader; the scrollback asked for is recorded in
// ScreenReads, so a test can tell a read of the screen from one of all of it.
func (f *FakeRuntime) ReadPanel(_ context.Context, _ revier.TargetRef, panel revier.PanelID, scrollback bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ScreenReads = append(f.ScreenReads, ScreenRead{Panel: panel, Scrollback: scrollback})
	return f.Screens[panel], f.ScreenErr
}

// ScreenRead is one ReadPanel call.
type ScreenRead struct {
	Panel      revier.PanelID
	Scrollback bool
}

// Tab is one OpenTab call.
type Tab struct {
	Ref   revier.TargetRef
	Real  revier.Realization
	Vars  map[string]string
	Panel revier.PanelID
}

// Open is Fake.Open with the panels a runtime gives what it opens: one per
// panel spec, and the realization's vars on each, which is how a real
// runtime reports back the mark the core set on them. Only a runtime's
// instances carry panels, so only a runtime's Open makes them.
func (f *FakeRuntime) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	ref, err := f.Fake.Open(ctx, r)
	if err != nil || ref.IsZero() {
		return ref, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.instances {
		if f.instances[i].Ref.ID != ref.ID {
			continue
		}
		var panels []revier.Panel
		for _, spec := range r.PanelSpecs() {
			f.nextID++
			panels = append(panels, revier.Panel{
				ID: revier.PanelID("p" + strconv.Itoa(f.nextID)), Kind: liveKind(spec),
				Title: spec.Title, Command: spec.Command, PID: 2000 + f.nextID,
				Vars: r.Vars,
			})
		}
		f.instances[i].Panels = panels
		if f.current == nil {
			f.current = map[string]revier.PanelID{}
		}
		f.current[ref.ID] = panels[0].ID
	}
	return ref, nil
}

// liveKind is the kind a host reports for the panel it started from spec: a
// shell for a declared shell, and a tool for anything else. No host says
// agent of a live panel (decisions.md D119).
func liveKind(spec revier.PanelSpec) revier.PanelKind {
	if spec.Kind == revier.PanelShell {
		return revier.PanelShell
	}
	return revier.PanelTool
}

// OpenTab adds the tab's panels to the instance, in a tab of their own with
// vars on each, and records the call.
// FakeRuntime implements revier.PanelOpener; a runtime without the capability
// is a different double.
func (f *FakeRuntime) OpenTab(_ context.Context, ref revier.TargetRef, r revier.Realization, vars map[string]string) (revier.PanelID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.OpenTabErr != nil {
		return "", f.OpenTabErr
	}
	for i := range f.instances {
		if f.instances[i].Ref.ID != ref.ID {
			continue
		}
		specs := r.Panels
		if len(specs) == 0 {
			specs = []revier.PanelSpec{{Kind: revier.PanelTool, Command: r.Launch}}
		}
		live := append([]revier.Panel(nil), f.instances[i].Panels...)
		var first revier.PanelID
		f.nextID++
		tab := "tab" + strconv.Itoa(f.nextID)
		for n, spec := range specs {
			f.nextID++
			panel := revier.Panel{ID: revier.PanelID("tab" + strconv.Itoa(f.nextID)), Kind: liveKind(spec), Title: spec.Title, Command: spec.Command, Tab: tab, Vars: vars}
			if n == 0 {
				first = panel.ID
			}
			live = append(live, panel)
		}
		f.instances[i].Panels = live
		f.Tabs = append(f.Tabs, Tab{Ref: ref, Real: r, Vars: vars, Panel: first})
		return first, nil
	}
	return "", fmt.Errorf("%s: no instance %s", f.name, ref.ID)
}

// FocusPanel records the panel and makes it current in its instance, or fails
// with FocusPanelErr.
func (f *FakeRuntime) FocusPanel(_ context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PanelFocuses = append(f.PanelFocuses, panel)
	if f.FocusPanelErr != nil {
		return f.FocusPanelErr
	}
	if f.current == nil {
		f.current = map[string]revier.PanelID{}
	}
	f.current[ref.ID] = panel
	return nil
}

// FocusedPanel reports the panel last focused in the instance, or fails with
// FocusedPanelErr.
func (f *FakeRuntime) FocusedPanel(_ context.Context, ref revier.TargetRef) (revier.PanelID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FocusedPanelErr != nil {
		return "", f.FocusedPanelErr
	}
	return f.current[ref.ID], nil
}

// Close removes the instance and records the call. Fake implements
// revier.Closer; CloseErr makes it fail, and Refuses keeps the instance listed
// as an application that asks about unsaved work does.
func (f *Fake) Close(_ context.Context, ref revier.TargetRef) error {
	f.mu.Lock()
	f.Closed = append(f.Closed, ref)
	err, refuses, then := f.CloseErr, f.Refuses[ref.ID], f.OnClose
	f.mu.Unlock()
	if then != nil {
		then(ref)
	}
	if err != nil {
		return err
	}
	if !refuses {
		f.Remove(ref)
	}
	return nil
}

// Hide records the call and leaves the instance listed, as a minimized
// window is. Fake implements revier.Hider; HideErr makes it fail.
func (f *Fake) Hide(_ context.Context, ref revier.TargetRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Hidden = append(f.Hidden, ref)
	return f.HideErr
}

// ClosePanel removes the panel from its instance and records the call.
// FakeRuntime implements revier.PanelCloser.
func (f *FakeRuntime) ClosePanel(_ context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ClosedPanels = append(f.ClosedPanels, panel)
	if f.CloseErr != nil {
		return f.CloseErr
	}
	for i := range f.instances {
		if f.instances[i].Ref.ID != ref.ID {
			continue
		}
		var live []revier.Panel
		for _, p := range f.instances[i].Panels {
			if p.ID != panel {
				live = append(live, p)
			}
		}
		f.instances[i].Panels = live
	}
	return nil
}

// Retitle changes a panel's title wherever it is listed, as the program in it
// does when its state changes.
func (f *Fake) Retitle(panel revier.PanelID, title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.instances {
		// A fresh slice: a listing already handed out shares the old one.
		panels := append([]revier.Panel(nil), f.instances[i].Panels...)
		for j := range panels {
			if panels[j].ID == panel {
				panels[j].Title = title
			}
		}
		f.instances[i].Panels = panels
	}
}

// Add registers a live instance and returns its ref.
func (f *Fake) Add(title, class string, panels ...revier.Panel) revier.TargetRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	ref := revier.TargetRef{Host: f.name, ID: strconv.Itoa(f.nextID), Title: title}
	f.instances = append(f.instances, revier.Instance{
		Ref: ref, Title: title, Class: class, PID: 1000 + f.nextID, Panels: panels,
	})
	return ref
}

// AddPanel adds a panel to a live instance, as a tab opened by hand does. A
// test that changes the desktop between a plan and the close it runs uses it,
// which is why it takes the lock: the close may be listing already.
func (f *Fake) AddPanel(ref revier.TargetRef, panel revier.Panel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.instances {
		if f.instances[i].Ref.ID != ref.ID {
			continue
		}
		// A fresh slice: a listing already handed out shares the old one.
		f.instances[i].Panels = append(append([]revier.Panel(nil), f.instances[i].Panels...), panel)
	}
}

// SetInstancesErr makes Instances fail from here on, under the lock a
// listing reads it with: a test that stops a host mid-shutdown writes it from
// the close, which another goroutine may be listing against.
func (f *Fake) SetInstancesErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InstancesErr = err
}

// FirstPanel is the first panel of an instance, which for one this host
// opened is the panel it made current, and none when it holds no panels.
func (f *Fake) FirstPanel(ref revier.TargetRef) revier.PanelID {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inst := range f.instances {
		if inst.Ref.ID == ref.ID && len(inst.Panels) > 0 {
			return inst.Panels[0].ID
		}
	}
	return ""
}

// AddInstance registers an instance as given, assigning only its id. Tests of
// the OS-window bridge use it to make a window host report the title and pid
// a runtime instance carries.
func (f *Fake) AddInstance(inst revier.Instance) revier.TargetRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	inst.Ref = revier.TargetRef{Host: f.name, ID: strconv.Itoa(f.nextID), Title: inst.Title}
	f.instances = append(f.instances, inst)
	return inst.Ref
}

// Remove drops an instance, for tests that assert on a target disappearing.
func (f *Fake) Remove(ref revier.TargetRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, in := range f.instances {
		if in.Ref.ID == ref.ID {
			f.instances = append(f.instances[:i], f.instances[i+1:]...)
			return
		}
	}
}

// SetFocus makes ref the focused instance.
func (f *Fake) SetFocus(ref revier.TargetRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = ref
}

func (f *Fake) Name() string { return f.name }

func (f *Fake) Probe(context.Context) error { return f.ProbeErr }

func (f *Fake) Instances(context.Context) ([]revier.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InstancesCalls++
	if f.InstancesErr != nil {
		return nil, f.InstancesErr
	}
	out := make([]revier.Instance, len(f.instances))
	copy(out, f.instances)
	return out, nil
}

// Open records the realization and materialises an instance whose title is the
// realization's own title pattern, so a follow-up match finds what was opened.
func (f *Fake) Open(_ context.Context, r revier.Realization) (revier.TargetRef, error) {
	f.mu.Lock()
	f.Opened = append(f.Opened, r)
	f.mu.Unlock()
	if f.OpenErr != nil {
		return revier.TargetRef{}, f.OpenErr
	}
	if len(r.Launch) == 0 && len(r.Panels) == 0 {
		return revier.TargetRef{}, fmt.Errorf("%s: realization has no launch argv and no panels", f.name)
	}
	// Honour the invariant every real host must honour: what Open creates,
	// this realization's Match finds. Name wins when set, as it does for tmux;
	// otherwise the match patterns describe the instance.
	title := literal(r.Match.Title)
	if r.Name != "" {
		title = r.Name
	}
	ref := f.Add(title, literal(r.Match.Class))
	if f.Detached {
		return revier.TargetRef{}, nil
	}
	return ref, nil
}

func (f *Fake) Focus(_ context.Context, ref revier.TargetRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Focuses = append(f.Focuses, ref)
	if f.FocusErr != nil {
		return f.FocusErr
	}
	f.focused = ref
	return nil
}

// Place records the geometry. Fake implements revier.WindowPlacer, so a test
// that wants a host without the capability uses a different double.
func (f *Fake) Place(_ context.Context, ref revier.TargetRef, geometry []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PlaceErr != nil {
		return f.PlaceErr
	}
	if f.Placements == nil {
		f.Placements = map[string][]string{}
	}
	f.Placements[ref.ID] = geometry
	return nil
}

// WorkareaWidth reports Workarea. Fake implements revier.WorkareaReader.
func (f *Fake) WorkareaWidth(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Workarea, f.WorkareaErr
}

func (f *Fake) Focused(context.Context) (revier.TargetRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FocusedErr != nil {
		return revier.TargetRef{}, f.FocusedErr
	}
	return f.focused, nil
}

// literal strips the anchors a match pattern carries so the fake can turn a
// pattern back into a plausible title. Tests use plain anchored literals; a
// pattern with real metacharacters is a test that should assert on Opened
// instead.
func literal(pattern string) string {
	s := pattern
	if len(s) > 0 && s[0] == '^' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '$' {
		s = s[:len(s)-1]
	}
	return s
}
