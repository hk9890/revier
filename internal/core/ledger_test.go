// Layer L2: an activation and a settle against a ledger in memory, with no
// surface driving either.
package core_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// An activation through the ledger writes the launch before the wait for its
// window, so a second press finds it and does not launch again
// (decisions.md D21), and lands where the window then is.
func TestActivateWaitingWritesTheLaunchThenTheLanding(t *testing.T) {
	defer func(w time.Duration) { core.BindWait = w }(core.BindWait)
	core.BindWait = 20 * time.Millisecond
	wm := hosttest.NewLateWindows("wm")
	l := &ledger{}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	p := prepared(t, project())

	if _, _, err := c.ActivateWaiting(context.Background(), p, "editor", nil); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}
	if !l.State().Pending("revier", "editor", time.Now()) {
		t.Fatal("the launch is not on record after the wait ran out")
	}
	if _, _, err := c.ActivateWaiting(context.Background(), p, "editor", nil); err != nil {
		t.Fatalf("second ActivateWaiting: %v", err)
	}
	if len(wm.Opened) != 1 {
		t.Errorf("the editor was launched %d times, want once: the second press found the launch", len(wm.Opened))
	}
}

// A press reads the ledger as it is when the press is made, so it sees what
// another user of the ledger wrote since the core was built: here a launch
// by a second core on the same ledger, as a desktop key's process is to the
// surface (decisions.md D120).
func TestAnActivationSeesWhatASecondLedgerUserWrote(t *testing.T) {
	shortBindWait(t)
	l := &ledger{}
	wm := hosttest.NewLateWindows("wm")
	surface := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	key := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	p := prepared(t, project())

	if _, _, err := key.ActivateWaiting(context.Background(), p, "editor", nil); err != nil {
		t.Fatalf("the key's press: %v", err)
	}
	_, res, err := surface.ActivateWaiting(context.Background(), p, "editor", nil)
	if err != nil || !res.ComingUp {
		t.Fatalf("the surface's press = %+v, %v; want the editor still coming up", res, err)
	}
	if len(wm.Opened) != 1 {
		t.Errorf("the editor was launched %d times, want once", len(wm.Opened))
	}

	// A binding written by one is the other's too.
	editor := wm.AddInstance(revier.Instance{Title: "moved on", Class: "code"})
	l.Update(func(st *state.State) bool { st.Landed("revier", "editor", editor); return true })
	if _, res, err = surface.ActivateWaiting(context.Background(), p, "editor", nil); err != nil || res.Ref != editor {
		t.Errorf("the press after the landing = %+v, %v; want the bound window %v raised", res, err, editor)
	}
}

// settling is a core on a ledger, and a function that surveys and settles
// once at now and returns the state after it.
func settling(t *testing.T, c *core.Core, l *ledger, now time.Time) func() *state.State {
	t.Helper()
	c.Ledger = l
	projects := []core.Project{prepared(t, project())}
	return func() *state.State {
		t.Helper()
		r, err := c.Survey(context.Background(), projects)
		if err != nil {
			t.Fatal(err)
		}
		return c.Settle(r, projects, now)
	}
}

// A survey settles state in one write: a gone ref is pruned, the window that
// appeared after an action's launch is claimed, and a launch past its window
// expires. This is claim-on-appear with no surface: the core keeps the
// listing a window must be new since.
func TestSettlePrunesClaimsAndExpires(t *testing.T) {
	now := time.Now()
	wm := hosttest.New("wm")
	gone := revier.TargetRef{Host: "wm", ID: "99"}
	l := &ledger{st: state.State{Launch: &state.Launch{Project: "revier", At: now}}}
	l.st.Attach("revier", gone)
	settle := settling(t, &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}, l, now)

	// Before the first listing nothing is new: no claim and no expiry. The
	// ref the listing lacks is pruned all the same.
	if st := settle(); st.Launch == nil || len(st.Attached["revier"]) != 0 {
		t.Fatalf("state after the first settle = %+v; want the launch kept and the gone ref pruned", st)
	}
	stray := wm.Add("Pull requests - Chromium", "chromium")
	st := settle()
	if refs := st.Attached["revier"]; len(refs) != 1 || refs[0] != stray {
		t.Errorf("attached = %v, want the stray window claimed", st.Attached)
	}
	if st.Launch != nil {
		t.Error("a claim must consume the launch")
	}
	if on := l.State(); len(on.Attached["revier"]) != 1 || on.Launch != nil {
		t.Errorf("the ledger holds %+v, want what Settle returned", on)
	}

	l = &ledger{st: state.State{Launch: &state.Launch{Project: "revier", At: now.Add(-state.ClaimWindow - time.Second)}}}
	settle = settling(t, &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}, l, now)
	if st := settle(); st.Launch == nil {
		t.Error("the first settle of a process expired a launch: with no previous listing nothing expires")
	}
	if st := settle(); st.Launch != nil {
		t.Errorf("launch = %+v, want the expired launch cleared", st.Launch)
	}
}

// A ref written while a survey listed is to a window the listing may have
// missed: the settle of that survey keeps it.
func TestSettleKeepsARefWrittenSinceTheSurveyStarted(t *testing.T) {
	now := time.Now()
	l := &ledger{}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm"), Ledger: l}
	projects := []core.Project{prepared(t, project())}
	r, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	late := revier.TargetRef{Host: "wm", ID: "7"}
	l.Update(func(st *state.State) bool { st.Bind("revier", "editor", late); return true })

	if st := c.Settle(r, projects, now); st.Bound["revier"]["editor"] != late {
		t.Errorf("bound = %v, want the ref written during the survey kept", st.Bound)
	}
}

// A window claimed for an action's launch is attached with the terminal
// inside it, exactly as `revier attach` records one, so the agent a launched
// terminal holds is surveyed from the moment it is claimed (decisions.md
// D95).
func TestSettleClaimsTheTerminalWithTheWindow(t *testing.T) {
	now := time.Now()
	rt := hosttest.NewRuntime("rt")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	wm := hosttest.New("wm")
	l := &ledger{st: state.State{Launch: &state.Launch{Project: "revier", At: now}}}
	settle := settling(t, &core.Core{Runtime: rt, Window: wm}, l, now)
	settle()

	term := rt.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})
	window := wm.AddInstance(revier.Instance{Title: "scratch", Class: "kitty", PID: 4242})

	want := []revier.TargetRef{window, term}
	if st := settle(); !slices.Equal(st.Attached["revier"], want) {
		t.Errorf("attached = %v, want %v", st.Attached["revier"], want)
	}
}
