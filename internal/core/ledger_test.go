// Layer L2: the state ledger against a scratch state root, and what a survey
// settles in state, with no surface driving either.
package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// The ledger reads and writes the state on disk: a launch it records is
// pending to the next read, a landing is bound, and every write is told.
func TestTheStateLedgerReadsAndWritesTheFile(t *testing.T) {
	root := t.TempDir()
	written := make(chan struct{}, 1)
	l := core.StateLedger{Root: root, Written: written}

	if l.Pending("work", "home") || len(l.Bound("work")) != 0 {
		t.Fatal("a fresh root has nothing pending and nothing bound")
	}
	l.Launched("work", "home", time.Now())
	if !l.Pending("work", "home") {
		t.Error("the launch just written is not pending")
	}
	select {
	case <-written:
	default:
		t.Error("the write was not told")
	}
	ref := revier.TargetRef{Host: "rt", ID: "1"}
	l.Landed("work", "home", ref)
	if got := l.Bound("work")["home"]; got != ref {
		t.Errorf("bound = %v, want the landing %v", got, ref)
	}
	st, _ := state.Load(root)
	if st.Launch != nil || st.Current != "work" {
		t.Errorf("state = launch %+v, current %q; want the launch consumed and the project current", st.Launch, st.Current)
	}
}

// An activation through the ledger writes the launch before the wait for its
// window, so a second press finds it and does not launch again
// (decisions.md D21), and lands where the window then is.
func TestActivateWaitingWritesTheLaunchThenTheLanding(t *testing.T) {
	defer func(w time.Duration) { core.BindWait = w }(core.BindWait)
	core.BindWait = 20 * time.Millisecond
	wm := hosttest.NewLateWindows("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	p := prepared(t, project())
	l := core.StateLedger{Root: t.TempDir()}

	if _, _, err := c.ActivateWaiting(context.Background(), p, "editor", nil, l); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}
	if !l.Pending("revier", "editor") {
		t.Fatal("the launch is not on record after the wait ran out")
	}
	if _, _, err := c.ActivateWaiting(context.Background(), p, "editor", nil, l); err != nil {
		t.Fatalf("second ActivateWaiting: %v", err)
	}
	if len(wm.Opened) != 1 {
		t.Errorf("the editor was launched %d times, want once: the second press found the launch", len(wm.Opened))
	}
}

// A survey settles state in one write: a gone ref is pruned, the window that
// appeared after an action's launch is claimed, and a launch past its window
// expires.
func TestSettlePrunesClaimsAndExpires(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	projects := []core.Project{prepared(t, project())}
	survey := func() core.Report {
		r, err := c.Survey(context.Background(), projects, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := survey()
	now := time.Now()

	st := &state.State{Launch: &state.Launch{Project: "revier", At: now}}
	gone := revier.TargetRef{Host: "wm", ID: "99"}
	st.Attach("revier", gone)
	stray := wm.Add("Pull requests - Chromium", "chromium")
	// The state as the survey started from is this one: what it holds and
	// the listing lacks is gone.
	if !c.Settle(st, st, survey(), first.Windows, true, projects, now) {
		t.Fatal("Settle reported no change")
	}
	if refs := st.Attached["revier"]; len(refs) != 1 || refs[0] != stray {
		t.Errorf("attached = %v, want the gone ref pruned and the stray window claimed", st.Attached)
	}
	if st.Launch != nil {
		t.Error("a claim must consume the launch")
	}

	st = &state.State{Launch: &state.Launch{Project: "revier", At: now.Add(-core.ClaimWindow - time.Second)}}
	if !c.Settle(st, nil, survey(), first.Windows, true, projects, now) || st.Launch != nil {
		t.Errorf("launch = %+v, want the expired launch cleared", st.Launch)
	}

	st = &state.State{Launch: &state.Launch{Project: "revier", At: now}}
	if c.Settle(st, nil, survey(), nil, false, projects, now) || st.Launch == nil {
		t.Error("before the first listing nothing is new: no claim and no expiry")
	}
}
