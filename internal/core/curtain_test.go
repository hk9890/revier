package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// curtainArgv launches a curtain by its title, and records each monitor it
// was asked for.
func curtainArgv(asked *[]revier.Monitor) func(revier.Monitor) []string {
	return func(m revier.Monitor) []string {
		*asked = append(*asked, m)
		return []string{"kitty", "--class", core.CurtainClass, "--title", core.CurtainTitle(m)}
	}
}

// twoMonitors is a window host whose primary monitor is listed second, so a
// test sees the core put it first.
func twoMonitors() *hosttest.Fake {
	wm := hosttest.New("wm")
	wm.MonitorList = []revier.Monitor{{Index: 0}, {Index: 1, Primary: true}}
	return wm
}

// The primary monitor is the main curtain's; any other is named by its index.
func TestCurtainTitleNamesTheMainCurtain(t *testing.T) {
	if got := core.CurtainTitle(revier.Monitor{Index: 2, Primary: true}); got != "revier-curtain-main" {
		t.Errorf("primary: title = %q, want revier-curtain-main", got)
	}
	if got := core.CurtainTitle(revier.Monitor{Index: 2}); got != "revier-curtain-2" {
		t.Errorf("other: title = %q, want revier-curtain-2", got)
	}
}

// One curtain opens on each monitor, the primary one first, each covers its
// own monitor, and the primary one is focused last, so its keys reach it
// (decisions.md D192).
func TestOpenCurtainsCoversEachMonitorPrimaryFirst(t *testing.T) {
	wm := twoMonitors()
	c := &core.Core{Window: wm}
	var asked []revier.Monitor

	refs, err := c.OpenCurtains(t.Context(), curtainArgv(&asked))
	if err != nil {
		t.Fatalf("OpenCurtains: %v", err)
	}
	if want := []revier.Monitor{{Index: 1, Primary: true}, {Index: 0}}; !slices.Equal(asked, want) {
		t.Errorf("argv asked for %+v, want %+v", asked, want)
	}
	if len(refs) != 2 || refs[0].Title != "revier-curtain-main" || refs[1].Title != "revier-curtain-0" {
		t.Fatalf("refs = %+v, want the main curtain, then the curtain of monitor 0", refs)
	}
	for i, r := range wm.Opened {
		if r.Launch[0] != "kitty" || r.Match.Class != "^"+core.CurtainClass+"$" {
			t.Errorf("open %d = %+v, want a kitty launch matched by the curtain's class", i, r)
		}
	}
	if want := []hosttest.Cover{{Ref: refs[0], Monitor: 1}, {Ref: refs[1], Monitor: 0}}; !slices.Equal(wm.Covers, want) {
		t.Errorf("covers = %+v, want %+v", wm.Covers, want)
	}
	if last := wm.WindowOps[len(wm.WindowOps)-1]; last != "focus "+refs[0].ID {
		t.Errorf("window calls = %q, want the main curtain focused last", wm.WindowOps)
	}
}

// A window host that cannot list its monitors opens no curtain.
func TestOpenCurtainsFailsWhenTheMonitorsCannotBeListed(t *testing.T) {
	wm := twoMonitors()
	wm.MonitorsErr = errors.New("wctl: not running")
	c := &core.Core{Window: wm}
	var asked []revier.Monitor

	refs, err := c.OpenCurtains(t.Context(), curtainArgv(&asked))
	if err == nil || len(refs) != 0 || len(wm.Opened) != 0 {
		t.Errorf("refs %v, opened %d, err %v; want an error and nothing opened", refs, len(wm.Opened), err)
	}
}

// A curtain whose window never comes ends the wait when its context does, and
// the refs opened so far are returned for the caller to end.
func TestOpenCurtainsEndsWhenAWindowNeverAppears(t *testing.T) {
	wm := hosttest.NewLateWindows("wm")
	wm.MonitorList = []revier.Monitor{{Index: 0, Primary: true}}
	c := &core.Core{Window: wm}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	var asked []revier.Monitor

	refs, err := c.OpenCurtains(ctx, curtainArgv(&asked))
	if !errors.Is(err, context.DeadlineExceeded) || len(refs) != 0 {
		t.Errorf("refs %v, err %v; want none and the context's deadline", refs, err)
	}
	if len(wm.Covers) != 0 {
		t.Errorf("covers = %+v, want none", wm.Covers)
	}
}

// A window host with no Coverer is no curtain's host.
func TestOpenCurtainsNeedsAHostThatCovers(t *testing.T) {
	var asked []revier.Monitor
	if _, err := (&core.Core{}).OpenCurtains(t.Context(), curtainArgv(&asked)); !errors.Is(err, core.ErrNoCurtainHost) {
		t.Errorf("no window host: err = %v, want ErrNoCurtainHost", err)
	}
	plain := struct{ revier.WindowController }{hosttest.New("wm")}
	if _, err := (&core.Core{Window: plain}).OpenCurtains(t.Context(), curtainArgv(&asked)); !errors.Is(err, core.ErrNoCurtainHost) {
		t.Errorf("a host that cannot cover: err = %v, want ErrNoCurtainHost", err)
	}
	if len(asked) != 0 {
		t.Errorf("argv asked for %+v, want nothing launched", asked)
	}
}

// openCurtains is a window host with a curtain on each of two monitors and
// one other window, and the refs of the main and the other curtain.
func openCurtains(t *testing.T) (wm *hosttest.Fake, main, other revier.TargetRef) {
	t.Helper()
	wm = hosttest.New("wm")
	main = wm.Add("revier-curtain-main", core.CurtainClass)
	wm.Add("session:demo", "kitty")
	other = wm.Add("revier-curtain-1", core.CurtainClass)
	return wm, main, other
}

// A hide lets other windows over each curtain before it minimizes it: a
// minimized window that is still above keeps the focus from every window
// that maps under it.
func TestHideCurtainsTurnsAboveOffBeforeTheHide(t *testing.T) {
	wm, main, other := openCurtains(t)
	if err := (&core.Core{Window: wm}).HideCurtains(t.Context()); err != nil {
		t.Fatalf("HideCurtains: %v", err)
	}
	want := []string{"above " + other.ID + " off", "hide " + other.ID, "above " + main.ID + " off", "hide " + main.ID}
	if !slices.Equal(wm.WindowOps, want) {
		t.Errorf("window calls = %q, want %q", wm.WindowOps, want)
	}
}

// A show raises each curtain before it keeps it above, and the main one is
// last, so it holds the keyboard.
func TestShowCurtainsFocusesBeforeAboveAndTheMainOneLast(t *testing.T) {
	wm, main, other := openCurtains(t)
	if err := (&core.Core{Window: wm}).ShowCurtains(t.Context()); err != nil {
		t.Fatalf("ShowCurtains: %v", err)
	}
	want := []string{"focus " + other.ID, "above " + other.ID + " on", "focus " + main.ID, "above " + main.ID + " on"}
	if !slices.Equal(wm.WindowOps, want) {
		t.Errorf("window calls = %q, want %q", wm.WindowOps, want)
	}
}

// After a restore's return home the main curtain takes the keyboard back,
// unless Esc hid it: a hidden curtain stays hidden.
func TestFocusCurtainLeavesAHiddenCurtainHidden(t *testing.T) {
	wm, main, _ := openCurtains(t)
	c := &core.Core{Window: wm}
	if err := c.FocusCurtain(t.Context()); err != nil {
		t.Fatalf("FocusCurtain: %v", err)
	}
	if !slices.Equal(wm.Focuses, []revier.TargetRef{main}) {
		t.Fatalf("focuses = %v, want the main curtain", wm.Focuses)
	}

	if err := c.HideCurtains(t.Context()); err != nil {
		t.Fatalf("HideCurtains: %v", err)
	}
	if err := c.FocusCurtain(t.Context()); err != nil {
		t.Fatalf("FocusCurtain on a hidden curtain: %v", err)
	}
	if len(wm.Focuses) != 1 {
		t.Errorf("focuses = %v, want no focus of the hidden curtain", wm.Focuses)
	}
}

// While a curtain is open the popup's key shows it again and does nothing
// else: no switch of the list and no raise of the popup (decisions.md D192).
func TestPopupShowsAnOpenCurtainAndRaisesNoPopup(t *testing.T) {
	wm, main, other := openCurtains(t)
	popup := wm.Add("revier", core.PopupClass)
	wm.SetFocus(popup)
	c := &core.Core{Window: wm, SwitchPopup: func(context.Context) error {
		t.Error("the press switched the popup's list")
		return nil
	}}
	if err := c.HideCurtains(t.Context()); err != nil {
		t.Fatalf("HideCurtains: %v", err)
	}
	wm.WindowOps = nil

	ref, err := c.Popup(t.Context(), popupArgv)
	if err != nil {
		t.Fatalf("Popup: %v", err)
	}
	if ref != main {
		t.Errorf("ref = %v, want the main curtain %v", ref, main)
	}
	want := []string{"focus " + other.ID, "above " + other.ID + " on", "focus " + main.ID, "above " + main.ID + " on"}
	if !slices.Equal(wm.WindowOps, want) {
		t.Errorf("window calls = %q, want %q", wm.WindowOps, want)
	}
	if slices.Contains(wm.Focuses, popup) || len(wm.Opened) != 0 {
		t.Errorf("focuses %v, opened %d; want the popup neither raised nor launched", wm.Focuses, len(wm.Opened))
	}
}
