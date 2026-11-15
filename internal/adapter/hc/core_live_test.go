//go:build live

// Layer L4: the core's run-or-raise against a real hc core, headless. L2
// proves the core against a fake; this proves it against the host. hc keeps
// no focus, so with no view a second press raises what the first opened
// rather than returning home; beside the fake view, which reports the focus
// a view keeps, it returns home (decisions.md D184).
package hc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// homeAndDiff is a project of two instances, home and diff.
func homeAndDiff() core.Project {
	sleep := []string{"sh", "-c", "sleep 30"}
	return core.PrepareProject(revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: sleep, Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Runtime: &revier.Realization{
				Name: "diff", Launch: sleep, Match: revier.Match{Title: "^diff$"},
			}},
		},
	})
}

func TestCoreRunOrRaiseAgainstRealHc(t *testing.T) {
	h, c := world(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p := homeAndDiff()

	home, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("open home: %v", err)
	}
	diff, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if diff.Ref.ID == home.Ref.ID {
		t.Fatal("diff and home must be different instances")
	}
	again, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diff.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.Ref.ID, diff.Ref.ID)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("got %d instances, want 2: run-or-raise must not duplicate: %+v", len(instances), instances)
	}
}

// The whole product against a real runtime and a view: run-or-raise opens
// the target the first time, a second press returns home, and the third
// raises the existing instance rather than opening a duplicate.
func TestCoreRunOrRaiseTogglesHomeInAView(t *testing.T) {
	h, c := world(t), ctx(t)
	view := hosttest.NewView("kitty")
	cr := &core.Core{Runtime: h, View: view}
	p := homeAndDiff()

	home, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("open home: %v", err)
	}
	diff, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if focused, _ := view.Focused(c); focused.ID != diff.Ref.ID {
		t.Fatalf("focused %s after the open, want diff %s", focused.ID, diff.Ref.ID)
	}
	back, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if back.Ref.ID != home.Ref.ID {
		t.Fatalf("toggle-back returned %s, want home %s", back.Ref.ID, home.Ref.ID)
	}
	again, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diff.Ref.ID || again.Launched {
		t.Errorf("raise returned %s, launched %v; want the existing %s", again.Ref.ID, again.Launched, diff.Ref.ID)
	}
	if instances, err := h.Instances(c); err != nil || len(instances) != 2 {
		t.Fatalf("instances = %+v, %v; want 2: run-or-raise must not duplicate", instances, err)
	}
}

// A tab whose command exits as soon as it starts is named as having exited,
// on every press, and leaves the instance as it was.
func TestCoreSaysATabWhoseCommandEndsAtOnceExited(t *testing.T) {
	h, c := world(t), ctx(t)
	cr := &core.Core{Runtime: h, View: hosttest.NewView("kitty")}
	p := homeAndDiff()
	p.Targets = append(p.Targets, revier.Target{Name: "quick", Runtime: &revier.Realization{
		Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelTool, Dir: t.TempDir(), Command: []string{"true"}}},
	}})
	p = core.PrepareProject(p.Project)
	home, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("open home: %v", err)
	}

	want := `hc: open tab quick: the tool panel's command "true" exited as soon as it started`
	for range 2 {
		if _, err := press(c, cr, p, "quick"); err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("press quick = %v, want %q", err, want)
		}
	}
	if panels := listing(t, h, home.Ref); len(panels) != 1 {
		t.Errorf("home's panels = %+v, want its own alone", panels)
	}
}

// hc's core keeps its id over a restart, and gives every instance an id of
// its own, so a binding kept from before a restart names no instance of the
// new core, and the next press opens the target again.
func TestABindingDoesNotOutliveItsCore(t *testing.T) {
	h, c := world(t), ctx(t)
	cr := &core.Core{Runtime: h}
	p := homeAndDiff()
	diff, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	stop(h)
	if _, err := press(c, cr, p, "home"); err != nil {
		t.Fatalf("open home on the new core: %v", err)
	}

	// The binding the first press left, as the next press reads it.
	cr.Ledger = ledger.File{Root: t.TempDir()}
	cr.Ledger.Update(func(st *state.State) bool {
		st.Bind(p.Name, "diff", diff.Ref)
		return true
	})
	res, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("go diff: %v", err)
	}
	if !res.Launched {
		t.Fatalf("go diff raised %q: the binding from the old core landed on another instance", res.Ref.Title)
	}
}

func press(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, nil)
	return res, err
}
