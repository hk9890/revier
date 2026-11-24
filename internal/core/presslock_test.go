// Layer L2: one press at a time per project launches, whichever process it
// runs in, and a launch whose later steps fail is still
// placed.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	statefile "github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

func runtimeProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Launch: []string{"sh"}, Match: revier.Match{Title: "^session:revier$"},
		}},
	}}
}

// twoWindowTargets is a project of two targets whose windows come after
// their launch.
func twoWindowTargets() revier.Project {
	target := func(name revier.TargetName, class string) revier.Target {
		return revier.Target{Name: name, Window: &revier.Realization{
			Launch: []string{string(name)}, Match: revier.Match{Class: class},
		}}
	}
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		target("editor", "^editor$"), target("docs", "^docs$"),
	}}
}

// Two processes press a closed project at once. The second reaches its
// listing while the first is inside Open, before the instance exists, which
// is the interleaving that launched a duplicate.
func TestTwoCoresPressingAClosedProjectLaunchOnce(t *testing.T) {
	root := t.TempDir()
	rt := hosttest.NewRuntime("rt")
	first := &core.Core{Runtime: rt, Ledger: statefile.File{Root: root}}
	second := &core.Core{Runtime: rt, Ledger: statefile.File{Root: root}}
	p := prepared(t, runtimeProject())

	var opens atomic.Int32
	var pressed sync.WaitGroup
	secondOpened := make(chan struct{})
	var secondRef revier.TargetRef
	var secondErr error
	rt.OnOpen = func(revier.Realization) {
		if opens.Add(1) != 1 {
			close(secondOpened)
			return
		}
		pressed.Add(1)
		go func() {
			defer pressed.Done()
			secondRef, _, secondErr = second.ActivateWaiting(t.Context(), p, "home", nil)
		}()
		select {
		case <-secondOpened:
		case <-time.After(300 * time.Millisecond):
		}
	}

	firstRef, _, err := first.ActivateWaiting(t.Context(), p, "home", nil)
	if err != nil {
		t.Fatalf("first press: %v", err)
	}
	pressed.Wait()
	if secondErr != nil {
		t.Fatalf("second press: %v", secondErr)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("opened %d instances, want one", len(rt.Opened))
	}
	if secondRef != firstRef {
		t.Errorf("second press landed on %v, want the first's %v", secondRef, firstRef)
	}
}

// The lock is released before the wait for a detached launch's window, so a
// second press answers at once that the launch is coming up.
func TestASecondPressDoesNotWaitForTheFirstsWindow(t *testing.T) {
	longBindWait(t)
	root := t.TempDir()
	wm := hosttest.NewLateWindows("wm")
	first := &core.Core{Window: wm, Ledger: statefile.File{Root: root}}
	second := &core.Core{Window: wm, Ledger: statefile.File{Root: root}}
	p := prepared(t, twoWindowTargets())

	waiting, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = first.ActivateWaiting(waiting, p, "editor", nil)
	}()
	defer func() {
		stop()
		<-done
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !first.Ledger.State().Pending("revier", "editor", time.Now()) {
		if time.Now().After(deadline) {
			t.Fatal("the first press recorded no launch")
		}
		time.Sleep(5 * time.Millisecond)
	}

	bounded, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, res, err := second.ActivateWaiting(bounded, p, "editor", nil)
	if err != nil {
		t.Fatalf("second press: %v", err)
	}
	if !res.ComingUp || len(wm.Opened) != 1 {
		t.Errorf("coming up %v with %d launches, want coming up and one launch", res.ComingUp, len(wm.Opened))
	}
}

// A launch moved to Late inside its window by a launch of another target
// still holds its own target back.
func TestALaunchJustMovedToLateIsNotLaunchedAgain(t *testing.T) {
	shortBindWait(t)
	wm := hosttest.NewLateWindows("wm")
	c := &core.Core{Window: wm, Ledger: &ledger{}}
	p := prepared(t, twoWindowTargets())
	for _, name := range []revier.TargetName{"editor", "docs"} {
		if _, err := press(t.Context(), c, p, name); err != nil {
			t.Fatalf("press %s: %v", name, err)
		}
	}
	if st := c.Ledger.State(); len(st.Late) != 1 || st.Late[0].Target != "editor" {
		t.Fatalf("late = %+v, want the editor's launch", st.Late)
	}

	res, err := press(t.Context(), c, p, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if !res.ComingUp || len(wm.Opened) != 2 {
		t.Errorf("coming up %v with %d launches, want coming up and two launches", res.ComingUp, len(wm.Opened))
	}
}

// A late launch past its window holds nothing back: a new launch replaces it.
func TestALateLaunchPastItsWindowLaunchesAgain(t *testing.T) {
	shortBindWait(t)
	wm := hosttest.NewLateWindows("wm")
	at := time.Now().Add(-state.BindWindow - time.Second)
	l := &ledger{st: state.State{Late: []state.Launch{{Project: "revier", Target: "editor", Class: "^editor$", At: at}}}}
	c := &core.Core{Window: wm, Ledger: l}

	res, err := press(t.Context(), c, prepared(t, twoWindowTargets()), "editor")
	if err != nil {
		t.Fatal(err)
	}
	if res.ComingUp || len(wm.Opened) != 1 {
		t.Errorf("coming up %v with %d launches, want one launch", res.ComingUp, len(wm.Opened))
	}
	if st := l.State(); len(st.Late) != 0 || !st.Pending("revier", "editor", time.Now()) {
		t.Errorf("late %+v, launch %+v; want the new launch in the late one's place", st.Late, st.Launch)
	}
}

// A failed Open leaves no record, so the next press launches at once.
func TestAPressAfterAFailedOpenLaunchesAgain(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Ledger: statefile.File{Root: t.TempDir()}}
	p := prepared(t, runtimeProject())
	rt.OpenErr = errors.New("no terminal")
	if _, err := press(t.Context(), c, p, "home"); !errors.Is(err, rt.OpenErr) {
		t.Fatalf("err = %v, want the failed open", err)
	}
	rt.OpenErr = nil

	res, err := press(t.Context(), c, p, "home")
	if err != nil {
		t.Fatal(err)
	}
	if res.ComingUp || res.Ref.IsZero() || len(rt.Opened) != 2 {
		t.Errorf("coming up %v, ref %v, %d opens; want a second launch", res.ComingUp, res.Ref, len(rt.Opened))
	}
}

// countingPlacer counts the placements a window host is asked for, which the
// fake's map of the last geometry cannot show.
type countingPlacer struct {
	*hosttest.Fake
	places atomic.Int32
}

func (w *countingPlacer) Place(ctx context.Context, ref revier.TargetRef, geometry []string) error {
	w.places.Add(1)
	return w.Fake.Place(ctx, ref, geometry)
}

var placeAt = []string{"right", "top", "75%", "100%"}

// A new instance whose focus fails is placed once, and the press reports the
// focus failure.
func TestALaunchWhoseFocusFailsIsPlacedAndReportsIt(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = strings.Join(placeAt, " ")
	wm := &countingPlacer{Fake: hosttest.New("wm")}
	wm.FocusErr = errors.New("focus refused")
	wm.PlaceErr = nil
	c := &core.Core{Window: wm}

	res, err := press(t.Context(), c, prepared(t, raw), "editor")
	if !errors.Is(err, wm.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if !slices.Equal(wm.Placements[res.Ref.ID], placeAt) || wm.places.Load() != 1 {
		t.Errorf("placed %v in %d calls, want %v once", wm.Placements[res.Ref.ID], wm.places.Load(), placeAt)
	}
}

// A failed place does not replace the focus failure the press reports.
func TestAFailedPlaceKeepsTheFocusError(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = strings.Join(placeAt, " ")
	wm := hosttest.New("wm")
	wm.FocusErr, wm.PlaceErr = errors.New("focus refused"), errors.New("place refused")

	_, err := press(t.Context(), &core.Core{Window: wm}, prepared(t, raw), "editor")
	if !errors.Is(err, wm.FocusErr) || errors.Is(err, wm.PlaceErr) {
		t.Errorf("err = %v, want the focus failure alone", err)
	}
}

// failingShow is a view whose first Show fails, after it opened its window
// or before.
type failingShow struct {
	*osWindowView
	openFirst bool
	failed    bool
	err       error
}

func (v *failingShow) Show(ctx context.Context, inst revier.Instance, open bool) (bool, error) {
	if v.failed {
		return v.osWindowView.Show(ctx, inst, open)
	}
	v.failed = true
	if !v.openFirst {
		return false, v.err
	}
	opened, _ := v.osWindowView.Show(ctx, inst, open)
	return opened, v.err
}

// A show that fails after it opened a window places that window once. One
// that opened none places nothing, and the press that opens the window
// places it.
func TestALaunchWhoseShowFailsIsPlacedOnce(t *testing.T) {
	for name, openFirst := range map[string]bool{"opened a window": true, "opened none": false} {
		t.Run(name, func(t *testing.T) {
			proj := viewedProject()
			proj.Targets[0].Runtime.Place = strings.Join(placeAt, " ")
			rt, wm := hosttest.NewRuntime("hc"), &countingPlacer{Fake: hosttest.New("wm")}
			inner := &osWindowView{FakeView: hosttest.NewView("kitty"), wm: wm.Fake, windows: map[string]revier.TargetRef{}}
			view := &failingShow{osWindowView: inner, openFirst: openFirst, err: errors.New("show refused")}
			c := &core.Core{Runtime: rt, View: view, Window: wm, Ledger: &ledger{}}
			p := prepared(t, proj)

			res, err := press(t.Context(), c, p, "home")
			if !errors.Is(err, view.err) {
				t.Fatalf("err = %v, want the show failure", err)
			}
			want := int32(0)
			if openFirst {
				want = 1
			}
			if got := wm.places.Load(); got != want {
				t.Fatalf("%d placements after the failed show, want %d", got, want)
			}

			if _, err := press(t.Context(), c, p, "home"); err != nil {
				t.Fatalf("second press: %v", err)
			}
			if got := wm.places.Load(); got != 1 {
				t.Errorf("%d placements after the next press, want 1", got)
			}
			if got := wm.Placements[inner.windows[res.Ref.ID].ID]; !slices.Equal(got, placeAt) {
				t.Errorf("window placed %v, want %v", got, placeAt)
			}
		})
	}
}

// A workspace whose tab fails is placed, starts no agent past the tabs that
// opened, and reports the tab failure.
func TestALaunchWhoseTabFailsIsPlaced(t *testing.T) {
	proj := viewedProject()
	proj.Targets[0].Runtime.Place = strings.Join(placeAt, " ")
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	rt.OpenTabErr = errors.New("no room")
	wm := &countingPlacer{Fake: hosttest.New("wm")}
	osw := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	c := &core.Core{Runtime: rt, View: rt, Window: wm}

	res, err := press(t.Context(), c, prepared(t, proj), "home")
	if !errors.Is(err, rt.OpenTabErr) {
		t.Fatalf("err = %v, want the tab failure", err)
	}
	if res.Ref.IsZero() || !slices.Equal(wm.Placements[osw.ID], placeAt) || wm.places.Load() != 1 {
		t.Errorf("ref %v, placed %v in %d calls; want the window placed once", res.Ref, wm.Placements[osw.ID], wm.places.Load())
	}
}
