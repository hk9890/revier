package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A launch that fails leaves nothing to pin.
func TestGoReportsAnOpenThatFailed(t *testing.T) {
	wm := hosttest.New("wm")
	wm.OpenErr = errors.New("code: not found")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := press(context.Background(), c, prepared(t, project()), "editor")
	if !errors.Is(err, wm.OpenErr) {
		t.Fatalf("err = %v, want the open failure", err)
	}
	if !res.Ref.IsZero() || res.Launched {
		t.Errorf("result = %+v, want nothing: no instance was made", res)
	}
}

// A new instance that cannot be focused still exists. Its ref comes back with
// the error, so the caller pins it and the next press raises it instead of
// opening a second (decisions.md D21).
func TestGoKeepsTheRefOfAnOpenedInstanceItCouldNotFocus(t *testing.T) {
	wm := hosttest.New("wm")
	wm.FocusErr = errors.New("no such window")
	// The ledger pins the ref a press returned with its error.
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: &ledger{}}
	p := prepared(t, project())

	res, err := press(context.Background(), c, p, "editor")
	if !errors.Is(err, wm.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if res.Ref.IsZero() || !res.Launched || res.Target != "editor" {
		t.Fatalf("result = %+v, want the launched editor's ref", res)
	}

	wm.FocusErr = nil
	again, err := press(context.Background(), c, p, "editor")
	if err != nil {
		t.Fatalf("second press: %v", err)
	}
	if len(wm.Opened) != 1 || again.Ref != res.Ref {
		t.Errorf("second press: opened %d, ref %v; want the first instance raised", len(wm.Opened), again.Ref)
	}
}

// A window a detached launch produced and the press could not raise still
// exists. It is pinned with the error, so the next press does not wait for a
// window that is already there.
func TestADetachedLaunchKeepsAWindowItCouldNotRaise(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Detached = true
	wm.FocusErr = errors.New("no such window")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: &ledger{}}

	_, err := press(context.Background(), c, prepared(t, project()), "editor")
	if !errors.Is(err, wm.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	windows, _ := wm.Instances(context.Background())
	if ref := landed(t, c, "editor"); len(windows) != 1 || ref != windows[0].Ref {
		t.Errorf("bound %v, want the editor %+v", ref, windows)
	}
}

// A workspace opened for a tab and not focused still exists. Its ref comes
// back with the error, as Go's does.
func TestATabKeepsTheWorkspaceItOpenedAndCouldNotFocus(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	rt.FocusErr = errors.New("no such window")
	c := &core.Core{Runtime: rt}

	res, err := press(context.Background(), c, prepared(t, tabProject()), "tickets")
	if !errors.Is(err, rt.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if res.Ref.IsZero() || res.Target != "home" || len(rt.Opened) != 1 {
		t.Errorf("result = %+v, opened %d; want the opened workspace's ref on home", res, len(rt.Opened))
	}
}

// A raise that cannot focus is a failure: the raise half did not happen.
func TestGoReportsARaiseThatCouldNotFocus(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Add("Visual Studio Code", "code")
	wm.FocusErr = errors.New("no such window")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := press(context.Background(), c, prepared(t, project()), "editor")
	if !errors.Is(err, wm.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if !res.Ref.IsZero() || len(wm.Opened) != 0 {
		t.Errorf("result = %+v, opened %d; want no ref and no launch", res, len(wm.Opened))
	}
}

// A host that cannot say what has focus cannot toggle back. The press raises
// the target it names.
func TestGoRaisesWhenFocusCannotBeRead(t *testing.T) {
	wm := hosttest.New("wm")
	editor := wm.Add("Visual Studio Code", "code")
	wm.SetFocus(editor)
	wm.FocusedErr = errors.New("focused: timed out")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := press(context.Background(), c, prepared(t, project()), "editor")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref != editor {
		t.Errorf("ref = %v, want the editor %v: nothing says it has focus", res.Ref, editor)
	}
}

// Placement is a convenience. A launch whose window cannot be placed is still
// a launch that landed.
func TestGoLandsWhenPlacementFails(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = "right top 75% 100%"
	wm := hosttest.New("wm")
	wm.PlaceErr = errors.New("place: refused")
	c := &core.Core{Window: wm}

	res, err := press(context.Background(), c, prepared(t, raw), "editor")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.IsZero() {
		t.Error("ref is zero, want the launched editor")
	}
}
