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

func popupArgv(context.Context) []string {
	return []string{"kitty", "--class", core.PopupClass, "-e", "revier"}
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

// With no popup open, the terminal is launched, and its window is placed and
// raised. Height is always 100%. Width is a fixed 1800 px, centred, where the
// workarea holds it, and 100% where it does not.
func TestPopupLaunchesAndPlacesByTheWorkarea(t *testing.T) {
	cases := []struct {
		width int
		want  []string
	}{
		{width: 5120, want: []string{"center", "top", "1800", "100%"}},
		{width: 1920, want: []string{"center", "top", "1800", "100%"}},
		{width: 1800, want: []string{"center", "top", "1800", "100%"}},
		{width: 1799, want: []string{"left", "top", "100%", "100%"}},
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

// A press on the popup that is the focused window already is the second
// press of its key: it is typed into the popup's panel as the key that
// switches its list, and nothing is raised.
func TestPopupSwitchesItsListWhenItIsFocusedAlready(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(open)
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1"})
	surface := rt.Add("revier", core.PopupClass, revier.Panel{ID: "9"})
	c := &core.Core{Window: wm, Runtime: rt}

	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil || ref != open {
		t.Fatalf("Popup = %v, %v; want the open popup", ref, err)
	}
	want := []hosttest.Sent{{Ref: surface, Panel: "9", Text: core.SwitchKey}}
	if !slices.Equal(rt.Sent, want) {
		t.Errorf("sent = %+v, want the switch key typed into the popup's panel", rt.Sent)
	}
	if len(wm.Focuses) != 0 || len(wm.Opened) != 0 {
		t.Errorf("focuses = %v, opened = %v; want neither", wm.Focuses, wm.Opened)
	}
}

// A press with the popup open and another window focused raises it, and types
// nothing: the popup opens on what it showed, and its first press is no
// switch.
func TestPopupTypesNothingWhenItIsNotFocused(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(wm.Add("session:demo", "kitty"))
	rt := hosttest.NewRuntime("rt")
	rt.Add("revier", core.PopupClass, revier.Panel{ID: "9"})
	c := &core.Core{Window: wm, Runtime: rt}

	if _, err := c.Popup(context.Background(), popupArgv); err != nil {
		t.Fatalf("Popup: %v", err)
	}
	if len(rt.Sent) != 0 || !slices.Equal(wm.Focuses, []revier.TargetRef{open}) {
		t.Errorf("sent = %+v, focuses = %v; want the popup raised and nothing typed", rt.Sent, wm.Focuses)
	}
}

// A focused popup no runtime here holds a panel of cannot be typed into: the
// press stays the raise it was before the surface had a second list.
func TestPopupRaisesAFocusedPopupTheRuntimeDoesNotHold(t *testing.T) {
	wm := hosttest.New("wm")
	open := wm.Add("revier", core.PopupClass)
	wm.SetFocus(open)
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1"})

	for name, c := range map[string]*core.Core{
		"no runtime":                  {Window: wm},
		"a runtime without the popup": {Window: wm, Runtime: rt},
	} {
		wm.Focuses = nil
		if _, err := c.Popup(context.Background(), popupArgv); err != nil {
			t.Fatalf("%s: Popup: %v", name, err)
		}
		if len(rt.Sent) != 0 || !slices.Equal(wm.Focuses, []revier.TargetRef{open}) {
			t.Errorf("%s: sent = %+v, focuses = %v; want the popup raised", name, rt.Sent, wm.Focuses)
		}
	}
}

// surfaceTerminal is a window host and a runtime that pair one window with
// one terminal, as kitty and GNOME do: the terminal's panels are the given
// ones, and the second is the current one.
func surfaceTerminal(panels ...revier.Panel) (*hosttest.Fake, *hosttest.FakeRuntime, revier.TargetRef, revier.TargetRef) {
	wm := hosttest.New("wm")
	rt := hosttest.NewRuntime("rt")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	window := wm.AddInstance(revier.Instance{Title: "work", Class: "kitty", PID: 4000})
	terminal := rt.AddInstance(revier.Instance{Title: "work", Class: "kitty", PID: 4000, Panels: panels})
	wm.SetFocus(window)
	_ = rt.FocusPanel(context.Background(), terminal, panels[len(panels)-1].ID)
	rt.PanelFocuses = nil
	return wm, rt, window, terminal
}

// A revier the user runs in a terminal of their own is a surface too, and the
// desktop takes the key from it as it does from the popup. A press with that
// terminal focused, and the surface current in it, switches its list: no
// popup opens over it.
func TestPopupSwitchesTheSurfaceInAFocusedTerminal(t *testing.T) {
	wm, rt, window, terminal := surfaceTerminal(
		revier.Panel{ID: "1", Command: []string{"zsh"}},
		revier.Panel{ID: "2", Command: []string{"/usr/local/bin/revier"}})
	wm.Workarea = 5120
	c := &core.Core{Window: wm, Runtime: rt}

	ref, err := c.Popup(context.Background(), popupArgv)
	if err != nil || ref != window {
		t.Fatalf("Popup = %v, %v; want the focused terminal", ref, err)
	}
	want := []hosttest.Sent{{Ref: terminal, Panel: "2", Text: core.SwitchKey}}
	if !slices.Equal(rt.Sent, want) {
		t.Errorf("sent = %+v, want the switch key typed into the surface's panel", rt.Sent)
	}
	if len(wm.Opened) != 0 || len(wm.Focuses) != 0 {
		t.Errorf("opened = %v, focuses = %v; want no popup", wm.Opened, wm.Focuses)
	}
}

// A focused terminal opens the popup as before when the panel current in it
// is not the surface: a shell, a revier command that is not the TUI, or the
// surface in a tab the user is not looking at.
func TestPopupOpensOverATerminalWhoseCurrentPanelIsNoSurface(t *testing.T) {
	for name, panels := range map[string][]revier.Panel{
		"a shell":                    {{ID: "1", Command: []string{"zsh"}}},
		"a revier command":           {{ID: "1", Command: []string{"revier", "agent", "wait", "demo"}}},
		"the surface in another tab": {{ID: "1", Command: []string{"revier"}}, {ID: "2", Command: []string{"zsh"}}},
	} {
		wm, rt, _, _ := surfaceTerminal(panels...)
		wm.Workarea = 5120
		c := &core.Core{Window: wm, Runtime: rt}
		if _, err := c.Popup(context.Background(), popupArgv); err != nil {
			t.Fatalf("%s: Popup: %v", name, err)
		}
		if len(rt.Sent) != 0 || len(wm.Opened) != 1 {
			t.Errorf("%s: sent = %+v, opened = %v; want nothing typed and the popup launched", name, rt.Sent, wm.Opened)
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
