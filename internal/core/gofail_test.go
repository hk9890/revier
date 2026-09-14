package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
)

// A launch that fails leaves nothing to pin.
func TestGoReportsAnOpenThatFailed(t *testing.T) {
	wm := hosttest.New("wm")
	wm.OpenErr = errors.New("code: not found")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
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
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	p := prepared(t, project())

	res, err := c.Go(context.Background(), p, "editor", nil)
	if !errors.Is(err, wm.FocusErr) {
		t.Fatalf("err = %v, want the focus failure", err)
	}
	if res.Ref.IsZero() || !res.Launched || res.Target != "editor" {
		t.Fatalf("result = %+v, want the launched editor's ref", res)
	}

	wm.FocusErr = nil
	again, err := c.Go(context.Background(), p, "editor", core.Bindings{"editor": res.Ref})
	if err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if len(wm.Opened) != 1 || again.Ref != res.Ref {
		t.Errorf("second press: opened %d, ref %v; want the first instance raised", len(wm.Opened), again.Ref)
	}
}

// A raise that cannot focus is a failure: the raise half did not happen.
func TestGoReportsARaiseThatCouldNotFocus(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Add("Visual Studio Code", "code")
	wm.FocusErr = errors.New("no such window")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
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

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
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

	res, err := c.Go(context.Background(), prepared(t, raw), "editor", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.IsZero() {
		t.Error("ref is zero, want the launched editor")
	}
}
