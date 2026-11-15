package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// RaiseSelf brings back the terminal this process runs in: its instance
// focused, its panel current, and its window raised, as a press on an agent
// brings that agent's.
func TestRaiseSelfRaisesTheTerminalThisProcessRunsIn(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	rt.Add("session:demo", "kitty", shellPanel("1"))
	own := rt.Add("revier", "kitty", shellPanel("2"), shellPanel("3"))
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "session:demo", Class: "kitty", PID: 1001})
	osw := wm.AddInstance(revier.Instance{Title: "revier", Class: "kitty", PID: 1002})
	c := &core.Core{Runtime: rt, View: rt, Window: wm, Self: func(p revier.Panel) bool { return p.ID == "3" }}

	if err := c.RaiseSelf(context.Background()); err != nil {
		t.Fatalf("RaiseSelf: %v", err)
	}
	if !slices.Equal(rt.Focuses, []revier.TargetRef{own}) || !slices.Equal(rt.PanelFocuses, []revier.PanelID{"3"}) {
		t.Errorf("runtime focuses %v, panels %v; want %v and panel 3", rt.Focuses, rt.PanelFocuses, own)
	}
	if !slices.Equal(wm.Focuses, []revier.TargetRef{osw}) {
		t.Errorf("window focuses = %v, want %v", wm.Focuses, osw)
	}
}

// The popup is raised by its class through the window host, as the press
// that raises it is, and no runtime is asked: it is raised under every
// runtime, and a runtime that lists no popup leaves it where the restore hid
// it all the same (decisions.md D182).
func TestRaisePopupRaisesThePopupByItsClassAlone(t *testing.T) {
	rt := hosttest.NewRuntime("hc")
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "billing", Class: "kitty", PID: 1001})
	popup := wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, PID: 1001})
	c := &core.Core{Runtime: rt, View: rt, Window: wm, Self: func(revier.Panel) bool { return true }}

	if err := c.RaisePopup(t.Context()); err != nil {
		t.Fatalf("RaisePopup: %v", err)
	}
	if !slices.Equal(wm.Focuses, []revier.TargetRef{popup}) {
		t.Errorf("window focuses = %v, want the popup %v", wm.Focuses, popup)
	}
	if rt.InstancesCalls != 0 {
		t.Errorf("the runtime was listed %d times, want never", rt.InstancesCalls)
	}
}

// With no window host there is nothing to raise the popup by, and with no
// popup open nothing to raise: both are refused, and nothing is focused.
func TestRaisePopupIsRefusedWithNothingToRaise(t *testing.T) {
	if err := (&core.Core{}).RaisePopup(t.Context()); !errors.Is(err, core.ErrNoPopupHost) {
		t.Errorf("no window host: err = %v, want ErrNoPopupHost", err)
	}
	wm := hosttest.New("wm")
	wm.Add("session:demo", "kitty")
	if err := (&core.Core{Window: wm}).RaisePopup(t.Context()); err == nil || len(wm.Focuses) != 0 {
		t.Errorf("no popup: err = %v, focuses = %v; want a refusal and nothing focused", err, wm.Focuses)
	}
}

// A process that runs in no terminal the runtime lists, or that cannot say
// which it runs in, raises nothing.
func TestRaiseSelfRaisesNothingWhereItRunsInNoListedTerminal(t *testing.T) {
	for name, self := range map[string]func(revier.Panel) bool{
		"no Self":          nil,
		"in no panel here": func(revier.Panel) bool { return false },
	} {
		rt := hosttest.NewRuntime("kitty")
		rt.SetCapabilities(revier.Capabilities{OSWindows: true})
		rt.Add("session:demo", "kitty", shellPanel("1"))
		wm := hosttest.New("wm")
		wm.AddInstance(revier.Instance{Title: "session:demo", Class: "kitty", PID: 1001})
		c := &core.Core{Runtime: rt, View: rt, Window: wm, Self: self}

		if err := c.RaiseSelf(context.Background()); err != nil {
			t.Errorf("%s: RaiseSelf: %v", name, err)
		}
		if len(rt.Focuses)+len(wm.Focuses) != 0 {
			t.Errorf("%s: focused %v and %v, want nothing", name, rt.Focuses, wm.Focuses)
		}
	}
}

// A press with the popup open raises it where the user left it: no second
// terminal, and no placement.
func TestPopupRaisesTheOneAlreadyOpen(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Workarea = 5120
	open := wm.Add("revier", core.PopupClass)
	wm.Add("session:demo", "kitty")
	c := &core.Core{Window: wm}

	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil {
		t.Fatalf("Popup: %v", err)
	}
	if ref != open || !slices.Equal(wm.Focuses, []revier.TargetRef{open}) {
		t.Errorf("ref = %v, focuses = %v; want the open popup raised", ref, wm.Focuses)
	}
	if len(wm.Opened) != 0 || len(wm.Placements) != 0 {
		t.Errorf("opened = %v, placements = %v; want neither", wm.Opened, wm.Placements)
	}
}

// Esc hides the open popup, so the next press raises it with everything it
// holds. It stays listed, which is what the raise finds.
func TestHidePopupHidesTheOpenPopup(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.Add("session:demo", "kitty")
	c := &core.Core{Window: wm}

	if err := c.HidePopup(context.Background()); err != nil {
		t.Fatalf("HidePopup: %v", err)
	}
	if !slices.Equal(wm.Hidden, []revier.TargetRef{open}) {
		t.Errorf("hidden = %v, want the popup alone", wm.Hidden)
	}
	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil || ref != open || len(wm.Opened) != 0 {
		t.Errorf("Popup after the hide: ref %v, err %v, opened %v; want the hidden popup raised", ref, err, wm.Opened)
	}
}

// Without a window host that can hide, or with no popup open, the hide is
// refused, and the surface exits the way it did before hiding existed.
func TestHidePopupIsRefusedWithNothingToHide(t *testing.T) {
	if err := (&core.Core{}).HidePopup(context.Background()); !errors.Is(err, core.ErrNoPopupHost) {
		t.Errorf("no window host: err = %v, want ErrNoPopupHost", err)
	}
	wm := hosttest.New("wm")
	wm.Add("session:demo", "kitty")
	if err := (&core.Core{Window: wm}).HidePopup(context.Background()); err == nil || len(wm.Hidden) != 0 {
		t.Errorf("no popup: err = %v, hidden = %v; want a refusal and nothing hidden", err, wm.Hidden)
	}
}

// A raise of a minimized popup puts that on record for the surface, and
// costs what the raise of one on the screen costs otherwise: the same
// listings, and no switch (decisions.md D169).
func TestARaiseOfAMinimizedPopupIsOnRecord(t *testing.T) {
	raise := func(minimized bool) (*hosttest.Fake, *hosttest.FakeRuntime) {
		wm := hosttest.New("wm")
		open := wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: minimized})
		wm.SetFocus(wm.Add("session:demo", "kitty"))
		rt := hosttest.NewRuntime("rt")
		rt.Add("revier", core.PopupClass, revier.Panel{ID: "9", Command: []string{"revier"}})
		l := &ledger{}
		sw := &switches{}
		if _, err := (&core.Core{Window: wm, Runtime: rt, View: rt, Ledger: l, SwitchPopup: sw.switchPopup}).Popup(context.Background(), popupArgv); err != nil {
			t.Fatalf("Popup: %v", err)
		}
		if !slices.Equal(wm.Focuses, []revier.TargetRef{open}) || sw.calls != 0 {
			t.Errorf("minimized %v: focuses = %v, switches = %d; want the popup raised and no switch", minimized, wm.Focuses, sw.calls)
		}
		if l.popupHidden != minimized {
			t.Errorf("minimized %v: marked = %v, want %v", minimized, l.popupHidden, minimized)
		}
		return wm, rt
	}
	wm, rt := raise(true)
	shownWM, shownRT := raise(false)
	if wm.InstancesCalls != shownWM.InstancesCalls || rt.InstancesCalls != shownRT.InstancesCalls {
		t.Errorf("listings minimized = %d window, %d runtime; on the screen = %d, %d; want the same",
			wm.InstancesCalls, rt.InstancesCalls, shownWM.InstancesCalls, shownRT.InstancesCalls)
	}
}

// No mark outlives a raise that failed, as no focus comes to take it, and a
// launch takes the mark a popup that went away left.
func TestNoMarkOutlivesAFailedRaiseOrALaunch(t *testing.T) {
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "revier", Class: core.PopupClass, Minimized: true})
	wm.FocusErr = errors.New("wctl: no answer")
	l := &ledger{}
	if _, err := (&core.Core{Window: wm, Ledger: l}).Popup(context.Background(), popupArgv); err == nil {
		t.Fatal("a raise whose focus failed reported no error")
	}
	if l.popupHidden {
		t.Error("a raise whose focus failed left its mark")
	}

	l = &ledger{popupHidden: true}
	if _, err := (&core.Core{Window: hosttest.New("wm"), Ledger: l}).Popup(context.Background(), popupArgv); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if l.popupHidden {
		t.Error("a launch kept the mark a popup that went away left")
	}
}

// The surface takes the raise of a minimized popup once: the next focus,
// with no press, finds none. A core with no ledger has none to take.
func TestTakePopupHiddenTakesItOnce(t *testing.T) {
	if (&core.Core{}).TakePopupHidden() {
		t.Error("a core with no ledger took a raise")
	}
	l := &ledger{popupHidden: true}
	c := &core.Core{Ledger: l}
	if !c.TakePopupHidden() {
		t.Error("the marked raise was not taken")
	}
	if c.TakePopupHidden() || l.popupHidden {
		t.Errorf("taken again = true or still marked = %v, want neither", l.popupHidden)
	}
}

// With no popup open, the terminal is launched, and its window is placed and
// raised. Height is always 100%. Width is a fixed 1800 px, centred, on a very
// wide workarea, 75% on a 29 inch wide screen, and 100% on a laptop.
func TestPopupLaunchesAndPlacesByTheWorkarea(t *testing.T) {
	cases := []struct {
		width int
		want  []string
	}{
		{width: 5120, want: []string{"center", "top", "1800", "100%"}},
		{width: 3400, want: []string{"center", "top", "1800", "100%"}},
		{width: 3399, want: []string{"center", "top", "75%", "100%"}},
		{width: 2560, want: []string{"center", "top", "75%", "100%"}},
		{width: 2200, want: []string{"center", "top", "75%", "100%"}},
		{width: 2199, want: []string{"left", "top", "100%", "100%"}},
		{width: 1440, want: []string{"left", "top", "100%", "100%"}},
	}
	for _, tc := range cases {
		wm := hosttest.New("wm")
		wm.Detached = true
		wm.Workarea = tc.width
		wm.Add("session:demo", "kitty")
		c := &core.Core{Window: wm}

		ref, err := c.Popup(context.Background(), popupArgv)
		if err != nil {
			t.Fatalf("width %d: Popup: %v", tc.width, err)
		}
		if len(wm.Opened) != 1 || !slices.Equal(wm.Opened[0].Launch, popupArgv(context.Background())) {
			t.Fatalf("width %d: opened = %v, want one launch of the argv", tc.width, wm.Opened)
		}
		if ref.IsZero() || !slices.Equal(wm.Placements[ref.ID], tc.want) {
			t.Errorf("width %d: placement of %v = %v, want %v", tc.width, ref, wm.Placements, tc.want)
		}
		if !slices.Equal(wm.Focuses, []revier.TargetRef{ref}) {
			t.Errorf("width %d: focuses = %v, want the new popup", tc.width, wm.Focuses)
		}
	}
}

// switches stands for the popup's socket: it counts the switches it was told,
// and fails each with err.
type switches struct {
	calls int
	err   error
}

func (s *switches) switchPopup(context.Context) error {
	s.calls++
	return s.err
}

// A press on the popup that is the focused window already is the second
// press of its key: the popup is told over its socket to switch its list, and
// nothing is raised. The popup is found by its class alone, so no runtime is
// asked, and the switch works under every runtime (decisions.md D182).
func TestPopupSwitchesItsListWhenItIsFocusedAlready(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(open)
	rt := hosttest.NewRuntime("hc")
	sw := &switches{}
	c := &core.Core{Window: wm, Runtime: rt, View: rt, SwitchPopup: sw.switchPopup}

	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil || ref != open {
		t.Fatalf("Popup = %v, %v; want the open popup", ref, err)
	}
	if sw.calls != 1 {
		t.Errorf("switches = %d, want one", sw.calls)
	}
	if len(wm.Focuses) != 0 || len(wm.Opened) != 0 || rt.InstancesCalls != 0 {
		t.Errorf("focuses = %v, opened = %v, runtime listings = %d; want none", wm.Focuses, wm.Opened, rt.InstancesCalls)
	}
}

// A press with the popup open and another window focused raises it, and
// switches nothing: the popup opens on what it showed, and its first press is
// no switch.
func TestPopupSwitchesNothingWhenItIsNotFocused(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(wm.Add("session:demo", "kitty"))
	sw := &switches{}
	c := &core.Core{Window: wm, SwitchPopup: sw.switchPopup}

	if _, err := c.Popup(t.Context(), popupArgv); err != nil {
		t.Fatalf("Popup: %v", err)
	}
	if sw.calls != 0 || !slices.Equal(wm.Focuses, []revier.TargetRef{open}) {
		t.Errorf("switches = %d, focuses = %v; want the popup raised and no switch", sw.calls, wm.Focuses)
	}
}

// A focused popup that takes no switch - no socket wired, or none that a
// surface listens on, as in a popup an older revier started - stays the
// raise the press was before the surface had a second list.
func TestPopupRaisesAFocusedPopupThatTakesNoSwitch(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(open)
	refused := &switches{err: errors.New("popup switch: connection refused")}

	for name, c := range map[string]*core.Core{
		"no switch wired":       {Window: wm},
		"no surface listens on": {Window: wm, SwitchPopup: refused.switchPopup},
	} {
		wm.Focuses = nil
		ref, err := c.Popup(t.Context(), popupArgv)
		if err != nil || ref != open {
			t.Fatalf("%s: Popup = %v, %v; want the open popup and no error", name, ref, err)
		}
		if !slices.Equal(wm.Focuses, []revier.TargetRef{open}) || len(wm.Opened) != 0 {
			t.Errorf("%s: focuses = %v, opened = %v; want the popup raised and none opened", name, wm.Focuses, wm.Opened)
		}
	}
	if refused.calls != 1 {
		t.Errorf("the refused switch was tried %d times, want once", refused.calls)
	}
}

// A revier the user runs in a terminal of their own is no popup: a press with
// that terminal focused opens the popup over it, whatever runs in it, the
// surface current included, and switches nothing (decisions.md D182). The
// runtime is not asked, and the window the key was pressed over is asked for
// once, for the switch and for the launch's line both.
func TestPopupOpensOverAFocusedTerminalOfTheUsersOwn(t *testing.T) {
	for name, panels := range map[string][]revier.Panel{
		"the surface current":        {{ID: "1", Command: []string{"zsh"}}, {ID: "2", Command: []string{"/usr/local/bin/revier"}}},
		"a shell":                    {{ID: "1", Command: []string{"zsh"}}},
		"a revier command":           {{ID: "1", Command: []string{"revier", "agent", "wait", "demo"}}},
		"the surface in another tab": {{ID: "1", Command: []string{"revier"}}, {ID: "2", Command: []string{"zsh"}}},
	} {
		wm, rt, _, _ := surfaceTerminal(panels...)
		wm.Workarea = 5120
		sw := &switches{}
		c := &core.Core{Window: wm, Runtime: rt, View: rt, SwitchPopup: sw.switchPopup}
		if _, err := c.Popup(t.Context(), popupArgv); err != nil {
			t.Fatalf("%s: Popup: %v", name, err)
		}
		if sw.calls != 0 || len(rt.Sent) != 0 || len(wm.Opened) != 1 {
			t.Errorf("%s: switches = %d, sent = %+v, opened = %v; want no switch, nothing typed and the popup launched", name, sw.calls, rt.Sent, wm.Opened)
		}
		if rt.InstancesCalls != 0 {
			t.Errorf("%s: the runtime was listed %d times, want never", name, rt.InstancesCalls)
		}
		if wm.FocusedCalls != 1 {
			t.Errorf("%s: asked for the focused window %d times, want once", name, wm.FocusedCalls)
		}
	}
}

// The popup needs a window host that can measure and place a window. Without
// one nothing is launched, so a key does not open a window in the wrong place.
func TestPopupRefusesAHostThatCannotPlace(t *testing.T) {
	if _, err := (&core.Core{}).Popup(context.Background(), popupArgv); !errors.Is(err, core.ErrNoPopupHost) {
		t.Errorf("no window host: err = %v, want ErrNoPopupHost", err)
	}
	fake := hosttest.New("wm")
	if _, err := (&core.Core{Window: bareWindow{fake}}).Popup(context.Background(), popupArgv); !errors.Is(err, core.ErrNoPopupHost) {
		t.Errorf("host without placement: err = %v, want ErrNoPopupHost", err)
	}
	if len(fake.Opened) != 0 {
		t.Errorf("opened = %v, want nothing launched", fake.Opened)
	}
}

// A workarea that cannot be read is a window host that misbehaved: the launch
// does not happen, rather than a popup of a guessed size.
func TestPopupDoesNotLaunchWithoutTheWorkarea(t *testing.T) {
	wm := hosttest.New("wm")
	wm.WorkareaErr = errors.New("extension not running")
	c := &core.Core{Window: wm}

	if _, err := c.Popup(context.Background(), popupArgv); !errors.Is(err, wm.WorkareaErr) {
		t.Fatalf("err = %v, want the workarea failure", err)
	}
	if len(wm.Opened) != 0 {
		t.Errorf("opened = %v, want nothing", wm.Opened)
	}
}
