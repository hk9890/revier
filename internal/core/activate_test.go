package core_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A press while the target's launch is still coming up does nothing - also
// when a binding remains from a window of that target that has since closed,
// which is what made the launch necessary (decisions.md D21).
func TestActivateDoesNotLaunchATargetStillComingUp(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	closed := core.Bindings{"editor": {Host: "wm", ID: "99"}}

	res, err := c.Activate(context.Background(), prepared(t, project()), "editor", closed, true, nil)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !res.ComingUp || !res.Ref.IsZero() {
		t.Errorf("result = %+v, want the target coming up and no ref", res)
	}
	if len(wm.Opened) != 0 {
		t.Errorf("opened %d, want none", len(wm.Opened))
	}
}

// A pending launch whose window has appeared is raised like any other.
func TestActivateRaisesAPendingLaunchOnceItsWindowIsThere(t *testing.T) {
	wm := hosttest.New("wm")
	editor := wm.Add("Visual Studio Code", "code")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Activate(context.Background(), prepared(t, project()), "editor", nil, true, nil)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if res.ComingUp || res.Ref != editor || len(wm.Opened) != 0 {
		t.Errorf("result = %+v, opened %d; want the editor %v raised", res, len(wm.Opened), editor)
	}
}

// With no launch on record a press is run-or-raise.
func TestActivateLaunchesWhenNothingIsPending(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Activate(context.Background(), prepared(t, project()), "editor", nil, false, nil)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !res.Launched || len(wm.Opened) != 1 {
		t.Errorf("result = %+v, opened %d; want one launch", res, len(wm.Opened))
	}
}

// A link's workspace still coming up is not launched again by a press on one
// of its agents.
func TestActivateAgentWaitsForAPaneOntoTheHostStillComingUp(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	remote := hosttest.NewRemote("buildbox")
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}

	res, err := c.ActivateAgent(context.Background(), prepared(t, remoteProject("demo")), revier.AgentView{Panel: "1"}, nil, true)
	if err != nil {
		t.Fatalf("ActivateAgent: %v", err)
	}
	if !res.ComingUp || res.Target != "home" {
		t.Errorf("result = %+v, want home coming up", res)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("opened %d, want none", len(rt.Opened))
	}
}
