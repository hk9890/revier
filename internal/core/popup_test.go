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

var popupArgv = []string{"kitty", "--class", core.PopupClass, "-e", "revier"}

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
		if len(wm.Opened) != 1 || !slices.Equal(wm.Opened[0].Launch, popupArgv) {
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
