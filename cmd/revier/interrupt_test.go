package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// The first interrupt cancels the walk, and a deadline spent before it does
// not: each step bounds its own.
func TestTheFirstInterruptCancelsTheWalkAndTheDeadlineDoesNot(t *testing.T) {
	spent, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	walk, stop := interruptible(spent)
	defer stop()
	if err := walk.Err(); err != nil {
		t.Fatalf("walk = %v before any interrupt, want it running past the spent deadline", err)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-walk.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupt did not cancel the walk")
	}
	if !errors.Is(walk.Err(), context.Canceled) {
		t.Errorf("walk = %v, want it cancelled", walk.Err())
	}
}

// interruptedBy makes the walk of a shutdown or a restore one that cancel
// stops, as the first interrupt does, without signalling the test's process.
func interruptedBy(t *testing.T) (cancel func()) {
	t.Helper()
	was := interruptible
	t.Cleanup(func() { interruptible = was })
	var cancels []context.CancelFunc
	interruptible = func(ctx context.Context) (context.Context, context.CancelFunc) {
		walk, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cancels = append(cancels, cancel)
		return walk, cancel
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// demoDesktop is the demo project with the targets named open on the
// runtime, and the app over state st.
func demoDesktop(t *testing.T, st *state.State, open ...string) (*app, *hosttest.FakeRuntime) {
	t.Helper()
	rt := hosttest.NewRuntime("tmux")
	for _, title := range open {
		rt.Add(title, "")
	}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, st)
	return a, rt
}

// shutDownState is a state that records a full shutdown.
func shutDownState() *state.State {
	st := &state.State{}
	st.MarkShutDown(time.Date(2026, 9, 15, 18, 30, 0, 0, time.Local), "")
	return st
}

// ctrl+c on `revier shutdown` lets the step that runs finish, reaches no
// other, says how much it left, and takes the full shutdown off the record:
// what it did not reach is still open.
func TestACtrlCStopsTheShutdownAfterTheStepThatRuns(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home", "diff")
	cancel := interruptedBy(t)
	rt.OnClose = func(revier.TargetRef) { cancel() }

	out := output(t, a, func() error { return cmdShutdown(context.Background(), a, nil) })
	if len(rt.Closed) != 1 {
		t.Errorf("closed %v, want the step that ran alone", rt.Closed)
	}
	if !strings.Contains(out, "cancelled: 1 step not reached") {
		t.Errorf("output = %q, want the step not reached", out)
	}
	if sd := a.core.Ledger.State().ShutDown; sd != nil {
		t.Errorf("shutdown on record = %+v after a cancel, want none", sd)
	}
}

// ctrl+c on `revier session restore` lets the target that opens finish and
// reaches no other.
func TestACtrlCStopsTheRestoreAfterTheTargetThatOpens(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{})
	if _, _, err := session.Save(a.stateRoot, session.Session{At: time.Now(), Projects: []session.Project{
		{Name: "demo", Targets: []session.Target{{Name: "home"}, {Name: "diff"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	cancel := interruptedBy(t)
	rt.OnOpen = func(revier.Realization) { cancel() }

	out := output(t, a, func() error { return cmdSessionRestore(context.Background(), a, nil) })
	if len(rt.Opened) != 1 {
		t.Errorf("opened %v, want the target that ran alone", rt.Opened)
	}
	if !strings.Contains(out, "cancelled: 1 target not reached") {
		t.Errorf("output = %q, want the target not reached", out)
	}
}

// `revier session restore` ends the full shutdown on record as it starts,
// also when it opens nothing; --dry-run opens nothing and ends nothing.
func TestARestoreEndsTheShutdownOnRecordAndADryRunDoesNot(t *testing.T) {
	for _, dry := range []bool{true, false} {
		a, rt := demoDesktop(t, shutDownState(), "home")
		if _, _, err := session.Save(a.stateRoot, session.Session{At: time.Now(), Projects: []session.Project{
			{Name: "demo", Targets: []session.Target{{Name: "home"}}},
		}}); err != nil {
			t.Fatal(err)
		}
		var args []string
		if dry {
			args = []string{"--dry-run"}
		}

		output(t, a, func() error { return cmdSessionRestore(context.Background(), a, args) })
		if len(rt.Opened) != 0 {
			t.Errorf("dry run %v: opened %v, want home kept as it is", dry, rt.Opened)
		}
		if onRecord := a.core.Ledger.State().ShutDown != nil; onRecord != dry {
			t.Errorf("dry run %v: shutdown on record = %v, want %v", dry, onRecord, dry)
		}
	}
}
