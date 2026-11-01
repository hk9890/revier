package main

import (
	"context"
	"errors"
	"os"
	"slices"
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
	walk, stop := interruptible(spent, nil)
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
// abandon runs what the second interrupt runs before the process ends.
func interruptedBy(t *testing.T) (cancel, abandon func()) {
	t.Helper()
	was := interruptible
	t.Cleanup(func() { interruptible = was })
	var cancels []context.CancelFunc
	var abandons []func()
	interruptible = func(ctx context.Context, abandon func()) (context.Context, context.CancelFunc) {
		walk, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cancels = append(cancels, cancel)
		if abandon != nil {
			abandons = append(abandons, abandon)
		}
		return walk, cancel
	}
	cancel = func() {
		for _, c := range cancels {
			c()
		}
	}
	abandon = func() {
		for _, a := range abandons {
			a()
		}
	}
	return cancel, abandon
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
// what it did not reach is still open, so the command fails.
func TestACtrlCStopsTheShutdownAfterTheStepThatRuns(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home", "diff")
	cancel, _ := interruptedBy(t)
	rt.OnClose = func(revier.TargetRef) { cancel() }

	var err error
	out := output(t, a, func() error { err = cmdShutdown(context.Background(), a, nil); return nil })
	if !errors.Is(err, errShutdownIncomplete) {
		t.Errorf("err = %v, want the shutdown incomplete", err)
	}
	if len(rt.Closed) != 1 {
		t.Errorf("closed %v, want the step that ran alone", rt.Closed)
	}
	if !strings.Contains(out, "cancelled: 1 step not reached") || strings.Contains(out, "complete") {
		t.Errorf("output = %q, want the step not reached and no complete said", out)
	}
	if sd := a.core.Ledger.State().ShutDown; sd != nil {
		t.Errorf("shutdown on record = %+v after a cancel, want none", sd)
	}
}

// ctrl+c during the last close of `revier shutdown` reaches no step: the
// command says no step was left, and the full shutdown stays on record.
func TestACtrlCDuringTheLastCloseLeavesTheShutdownOnRecord(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home", "diff")
	cancel, _ := interruptedBy(t)
	rt.OnClose = func(revier.TargetRef) {
		if len(rt.Closed) == 2 {
			cancel()
		}
	}

	out := output(t, a, func() error { return cmdShutdown(context.Background(), a, nil) })
	if len(rt.Closed) != 2 {
		t.Errorf("closed %v, want both steps", rt.Closed)
	}
	if strings.Contains(out, "cancelled") || !strings.Contains(out, "shutdown complete; safe to leave") {
		t.Errorf("output = %q, want no step said to be left and the shutdown complete", out)
	}
	if a.core.Ledger.State().ShutDown == nil {
		t.Error("a shutdown that closed every step was taken off the record")
	}
}

// A second ctrl+c ends `revier shutdown` before the shutdown returns, so it
// takes a full shutdown that left a step off the record as it goes.
func TestASecondCtrlCTakesTheFullShutdownOffTheRecord(t *testing.T) {
	a, rt := demoDesktop(t, &state.State{}, "home", "diff")
	cancel, abandon := interruptedBy(t)
	var onRecord []bool
	rt.OnClose = func(revier.TargetRef) {
		if len(rt.Closed) == 1 {
			onRecord = append(onRecord, a.core.Ledger.State().ShutDown != nil)
			cancel()
			abandon()
			onRecord = append(onRecord, a.core.Ledger.State().ShutDown != nil)
		}
	}

	output(t, a, func() error { _ = cmdShutdown(context.Background(), a, nil); return nil })
	if want := []bool{true, false}; !slices.Equal(onRecord, want) {
		t.Errorf("on record before and after the second ctrl+c = %v, want %v", onRecord, want)
	}
}

// A second ctrl+c before `revier shutdown` wrote its record takes nothing
// back: the record of an earlier full shutdown stays. The command fails, as
// nothing closed.
func TestASecondCtrlCBeforeTheRecordKeepsAnEarlierOne(t *testing.T) {
	a, rt := demoDesktop(t, shutDownState(), "home", "diff")
	earlier := *a.core.Ledger.State().ShutDown
	cancel, abandon := interruptedBy(t)
	// The plan's survey lists the runtime first, and the recheck before the
	// save second: both presses come there.
	listed := 0
	rt.OnInstances = func() {
		if listed++; listed == 2 {
			cancel()
			abandon()
		}
	}

	var err error
	output(t, a, func() error { err = cmdShutdown(context.Background(), a, nil); return nil })
	if listed < 2 {
		t.Fatalf("the runtime was listed %d times, want the recheck to list it", listed)
	}
	if !errors.Is(err, errShutdownIncomplete) {
		t.Errorf("err = %v, want the shutdown incomplete", err)
	}
	if len(rt.Closed) != 0 {
		t.Errorf("closed %v, want nothing", rt.Closed)
	}
	if sd := a.core.Ledger.State().ShutDown; sd == nil || !sd.At.Equal(earlier.At) {
		t.Errorf("shutdown on record = %+v, want the earlier one %+v kept", sd, earlier)
	}
}

// The second interrupt ends the hung step's command, runs what the walk
// leaves to it, then ends the process. The command is ended before the
// abandon, which can wait on the state's lock until a third interrupt.
func TestASecondInterruptEndsTheCommandAbandonsTheWalkAndEnds(t *testing.T) {
	was, wasEnd := interrupted, endHung
	t.Cleanup(func() { interrupted, endHung = was, wasEnd })
	var ran []string
	endHung = func() { ran = append(ran, "end the command") }
	ended := make(chan struct{})
	interrupted = func() {
		ran = append(ran, "end")
		close(ended)
	}
	walk, stop := interruptible(context.Background(), func() { ran = append(ran, "abandon") })
	defer stop()

	for range 2 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		if walk.Err() == nil {
			<-walk.Done()
		}
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the second interrupt did not end the process")
	}
	if want := []string{"end the command", "abandon", "end"}; !slices.Equal(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
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
	cancel, _ := interruptedBy(t)
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
