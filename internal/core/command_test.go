package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// commandProject is project() with a command target, web, that runs a
// launcher with the project's name.
func commandProject(timeout string) revier.Project {
	p := project()
	p.Targets = append(p.Targets, revier.Target{
		Name: "web", Key: "ctrl-shift-i",
		Command: &revier.Command{Run: []string{"repo-web", "{{.Name}}"}, Timeout: timeout},
	})
	return p
}

// run is a fake command runner: it records every run, and runs do, which a
// test sets to move focus, fail, or take its time.
type run struct {
	mu   sync.Mutex
	runs []ranCall
	do   func(ctx context.Context) error
}

type ranCall struct {
	argv []string
	dir  string
}

func (r *run) Run(ctx context.Context, argv []string, dir string) error {
	r.mu.Lock()
	r.runs = append(r.runs, ranCall{argv, dir})
	do := r.do
	r.mu.Unlock()
	if do == nil {
		return nil
	}
	return do(ctx)
}

func (r *run) calls() []ranCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.runs)
}

// focusing is a run that moves focus to ref, as a launcher that raises a
// window does.
func focusing(wm *hosttest.Fake, ref revier.TargetRef) func(context.Context) error {
	return func(ctx context.Context) error { return wm.Focus(ctx, ref) }
}

// commandDesktop is a window host with the home workspace's runtime and a
// browser window no project declares, a runner whose run raises the
// browser, and a core over both with ledger l.
func commandDesktop(l *ledger) (*core.Core, *hosttest.Fake, *run, revier.TargetRef) {
	wm := hosttest.New("wm")
	chrome := wm.Add("GitHub - Google Chrome", "google-chrome")
	r := &run{do: focusing(wm, chrome)}
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	return &core.Core{Runtime: rt, Window: wm, Run: r.Run, Ledger: l}, wm, r, chrome
}

// longBindWait makes a press that waits for a window wait an hour, so a
// press that returns at once is shown to wait for none.
func longBindWait(t *testing.T) {
	t.Helper()
	was := core.BindWait
	core.BindWait = time.Hour
	t.Cleanup(func() { core.BindWait = was })
}

// A press runs the rendered argv in the project's directory, and returns at
// once. It records the run and the project, writes no launch, and leaves
// another target's pending launch as it was.
func TestACommandTargetRunsItsArgvAndRecordsTheRun(t *testing.T) {
	longBindWait(t)
	l := pendingLaunch("revier", "editor")
	c, wm, r, chrome := commandDesktop(l)

	start := time.Now()
	res, err := press(context.Background(), c, prepared(t, commandProject("")), "web")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("press took %s, want it to wait for no window", took)
	}
	if !res.Ran || res.Target != "web" || res.Focused != chrome || !res.Ref.IsZero() || res.Launched {
		t.Errorf("result = %+v, want web ran with focus on %v", res, chrome)
	}
	if got := r.calls(); len(got) != 1 || !slices.Equal(got[0].argv, []string{"repo-web", "revier"}) || got[0].dir != "/home/user/dev/github/revier" {
		t.Errorf("runs = %+v, want one of the rendered argv in the project", got)
	}
	st := l.State()
	want := state.LastRun{Project: "revier", Target: "web", Focused: chrome}
	if st.LastRun == nil || *st.LastRun != want || st.Current != "revier" {
		t.Errorf("last run %+v, current %q; want %+v of revier", st.LastRun, st.Current, want)
	}
	if st.Launch == nil || st.Launch.Target != "editor" {
		t.Errorf("launch = %+v, want the editor's kept", st.Launch)
	}
	if len(wm.Opened) != 0 {
		t.Errorf("opened %v, want nothing", wm.Opened)
	}
}

// A launch of the target on record from before it ran a command does not
// hold the press back.
func TestAStaleLaunchDoesNotHoldACommandTargetBack(t *testing.T) {
	c, _, r, _ := commandDesktop(pendingLaunch("revier", "web"))

	res, err := press(context.Background(), c, prepared(t, commandProject("")), "web")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.ComingUp || !res.Ran || len(r.calls()) != 1 {
		t.Errorf("result = %+v after %d runs, want one run", res, len(r.calls()))
	}
}

// A second press, while focus is still where the run left it, goes home and
// runs nothing; the press after that runs again.
func TestASecondPressOfACommandTargetGoesHome(t *testing.T) {
	l := &ledger{}
	c, _, r, _ := commandDesktop(l)
	p := prepared(t, commandProject(""))

	if _, err := press(context.Background(), c, p, "web"); err != nil {
		t.Fatalf("first press: %v", err)
	}
	res, err := press(context.Background(), c, p, "web")
	if err != nil {
		t.Fatalf("second press: %v", err)
	}
	if res.Target != "home" || res.Ran || len(r.calls()) != 1 {
		t.Errorf("result = %+v after %d runs, want home and one run", res, len(r.calls()))
	}
	if st := l.State(); st.LastRun != nil {
		t.Errorf("last run = %+v after going home, want none", st.LastRun)
	}
}

// A second press while the home's launch is still coming up waits for it,
// as a press of home does, and launches no second home.
func TestASecondPressOfACommandTargetWaitsForAHomeComingUp(t *testing.T) {
	l := pendingLaunch("revier", "home")
	c, wm, r, chrome := commandDesktop(l)
	rt := hosttest.NewRuntime("rt")
	c.Runtime = rt
	if err := wm.Focus(context.Background(), chrome); err != nil {
		t.Fatal(err)
	}
	l.st.LastRun = &state.LastRun{Project: "revier", Target: "web", Focused: chrome}

	res, err := press(context.Background(), c, prepared(t, commandProject("")), "web")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !res.ComingUp || res.Target != "home" || len(r.calls()) != 0 || len(rt.Opened) != 0 {
		t.Errorf("result = %+v after %d runs and opens %v, want home coming up", res, len(r.calls()), rt.Opened)
	}
}

// The press runs the command whenever the toggle is not certain.
func TestAPressOfACommandTargetRunsWhenItIsNoToggle(t *testing.T) {
	elsewhere := revier.TargetRef{Host: "wm", ID: "999"}
	for name, tc := range map[string]struct {
		last    func(chrome revier.TargetRef) *state.LastRun
		setup   func(wm *hosttest.Fake)
		project func() revier.Project
	}{
		"focus elsewhere": {
			last: func(revier.TargetRef) *state.LastRun {
				return &state.LastRun{Project: "revier", Target: "web", Focused: elsewhere}
			},
		},
		"another target": {
			last: func(chrome revier.TargetRef) *state.LastRun {
				return &state.LastRun{Project: "revier", Target: "other", Focused: chrome}
			},
		},
		"another project": {
			last: func(chrome revier.TargetRef) *state.LastRun {
				return &state.LastRun{Project: "other", Target: "web", Focused: chrome}
			},
		},
		"focus unreadable": {
			last: func(chrome revier.TargetRef) *state.LastRun {
				return &state.LastRun{Project: "revier", Target: "web", Focused: chrome}
			},
			setup: func(wm *hosttest.Fake) { wm.FocusedErr = errors.New("wctl gone") },
		},
		"no home": {
			last: func(chrome revier.TargetRef) *state.LastRun {
				return &state.LastRun{Project: "revier", Target: "web", Focused: chrome}
			},
			project: func() revier.Project {
				p := commandProject("")
				p.Targets[0].Home = false
				return p
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := &ledger{}
			c, wm, r, chrome := commandDesktop(l)
			if err := wm.Focus(context.Background(), chrome); err != nil {
				t.Fatal(err)
			}
			l.st.LastRun = tc.last(chrome)
			if tc.setup != nil {
				tc.setup(wm)
			}
			proj := commandProject("")
			if tc.project != nil {
				proj = tc.project()
			}
			res, err := press(context.Background(), c, prepared(t, proj), "web")
			if err != nil {
				t.Fatalf("press: %v", err)
			}
			if !res.Ran || len(r.calls()) != 1 {
				t.Errorf("result = %+v after %d runs, want one run", res, len(r.calls()))
			}
		})
	}
}

// A press that is no toggle reads focus once before it runs: the read that
// finds focus off the last run's window is the read the run starts from.
func TestAPressReadsFocusOnceBeforeItRuns(t *testing.T) {
	l := &ledger{}
	c, wm, r, chrome := commandDesktop(l)
	if err := wm.Focus(context.Background(), chrome); err != nil {
		t.Fatal(err)
	}
	l.st.LastRun = &state.LastRun{Project: "revier", Target: "web", Focused: revier.TargetRef{Host: "wm", ID: "999"}}
	editor := wm.Add("main.go - editor", "editor")
	reads := -1
	r.do = func(ctx context.Context) error {
		reads = wm.FocusedCalls
		return wm.Focus(ctx, editor)
	}

	if _, err := press(context.Background(), c, prepared(t, commandProject("")), "web"); err != nil {
		t.Fatalf("press: %v", err)
	}
	if reads != 1 {
		t.Errorf("focus read %d times before the run, want once", reads)
	}
}

// A run that leaves focus where it was records no run, so the next press
// runs again.
func TestARunThatLeavesFocusRecordsNoRun(t *testing.T) {
	l := &ledger{}
	c, _, r, _ := commandDesktop(l)
	r.do = nil
	p := prepared(t, commandProject(""))

	for range 2 {
		res, err := press(context.Background(), c, p, "web")
		if err != nil {
			t.Fatalf("press: %v", err)
		}
		if !res.Ran || !res.Focused.IsZero() {
			t.Errorf("result = %+v, want a run that moved no focus", res)
		}
		if st := l.State(); st.LastRun != nil || st.Current != "revier" {
			t.Errorf("last run %+v, current %q; want none of revier", st.LastRun, st.Current)
		}
	}
	if len(r.calls()) != 2 {
		t.Errorf("runs = %d, want two", len(r.calls()))
	}
}

// A run after a focus that could not be read records no run, though focus
// is on another window after it: the run may have left focus where it was.
func TestARunAfterAnUnreadableFocusRecordsNoRun(t *testing.T) {
	l := &ledger{}
	c, wm, r, chrome := commandDesktop(l)
	wm.FocusedErr = errors.New("wctl gone")
	r.do = func(ctx context.Context) error {
		wm.FocusedErr = nil
		return wm.Focus(ctx, chrome)
	}

	res, err := press(context.Background(), c, prepared(t, commandProject("")), "web")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if !res.Ran || !res.Focused.IsZero() {
		t.Errorf("result = %+v, want a run that records no focus", res)
	}
	if st := l.State(); st.LastRun != nil {
		t.Errorf("last run = %+v, want none", st.LastRun)
	}
}

// A focus the launcher asks for lands after it exits, and the run still
// records it.
func TestARunRecordsAFocusThatLandsLate(t *testing.T) {
	l := &ledger{}
	c, wm, r, chrome := commandDesktop(l)
	r.do = func(context.Context) error {
		time.AfterFunc(core.RunSettle/5, func() { _ = wm.Focus(context.Background(), chrome) })
		return nil
	}

	res, err := press(context.Background(), c, prepared(t, commandProject("")), "web")
	if err != nil {
		t.Fatalf("press: %v", err)
	}
	if res.Focused != chrome {
		t.Errorf("focused = %v, want %v", res.Focused, chrome)
	}
}

// A refused target and a refused project run nothing (decisions.md D85).
func TestARefusedCommandTargetRunsNothing(t *testing.T) {
	for name, p := range map[string]core.Project{
		"target": core.PrepareProject(commandProject("forever")),
		"project": func() core.Project {
			p := core.PrepareProject(commandProject(""))
			p.Invalid = errors.New("project has no path")
			return p
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			c, _, r, _ := commandDesktop(&ledger{})
			if _, err := press(context.Background(), c, p, "web"); err == nil {
				t.Error("press succeeded, want the refusal")
			}
			if len(r.calls()) != 0 {
				t.Errorf("runs = %+v, want none", r.calls())
			}
		})
	}
}

// A run that fails, or that does not end in its timeout, records nothing and
// says why.
func TestAFailedRunRecordsNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout string
		do      func(context.Context) error
		want    string
	}{
		"exit": {
			do:   func(context.Context) error { return errors.New("command exited 1: no repository page") },
			want: "web: command exited 1: no repository page",
		},
		"timeout": {
			timeout: "50ms",
			do: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			want: "web: command did not end within 50ms",
		},
		"timeout with a line": {
			timeout: "50ms",
			do: func(ctx context.Context) error {
				<-ctx.Done()
				return fmt.Errorf("%w: waiting for a lock", ctx.Err())
			},
			want: "web: command did not end within 50ms: waiting for a lock",
		},
		"exit as the timeout ends": {
			timeout: "50ms",
			do: func(ctx context.Context) error {
				<-ctx.Done()
				return errors.New("command exited 1: no repository page")
			},
			want: "web: command exited 1: no repository page",
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := &ledger{}
			c, _, r, _ := commandDesktop(l)
			r.do = tc.do
			_, err := press(context.Background(), c, prepared(t, commandProject(tc.timeout)), "web")
			if err == nil || err.Error() != tc.want {
				t.Errorf("press = %v, want %q", err, tc.want)
			}
			if st := l.State(); st.LastRun != nil || st.Current != "" {
				t.Errorf("last run %+v, current %q; want nothing recorded", st.LastRun, st.Current)
			}
		})
	}
}

// The survey shows a command target available, needing no host, and never
// held.
func TestTheSurveyShowsACommandTarget(t *testing.T) {
	c, _, _, _ := commandDesktop(&ledger{})
	r := survey(t, c, []core.Project{prepared(t, commandProject(""))}, nil)

	tv, ok := viewOf(r, "web")
	if !ok || !tv.Command || !tv.Available || tv.Host != "" || !tv.Ref.IsZero() || tv.Reason != "" {
		t.Errorf("web = %+v, want an available command target with no host and no ref", tv)
	}

	refused := survey(t, c, []core.Project{prepared(t, commandProject("forever"))}, nil)
	if tv, _ := viewOf(refused, "web"); tv.Available || !strings.Contains(tv.Reason, "is no duration") {
		t.Errorf("refused web = %+v, want it unavailable with the reason", tv)
	}
}

func viewOf(r core.Report, name revier.TargetName) (revier.TargetView, bool) {
	for _, tv := range r.Views[0].Targets {
		if tv.Name == name {
			return tv, true
		}
	}
	return revier.TargetView{}, false
}

// The window a command raises is no project's: a command target matches no
// instance.
func TestAFocusedWindowIsNoCommandTargets(t *testing.T) {
	c, wm, _, chrome := commandDesktop(&ledger{})
	if err := wm.Focus(context.Background(), chrome); err != nil {
		t.Fatal(err)
	}
	p, ok, err := c.ProjectOfFocused(context.Background(), []core.Project{prepared(t, commandProject(""))})
	if err != nil || ok {
		t.Errorf("ProjectOfFocused = %s, %v, %v; want none", p.Name, ok, err)
	}
}

// A session saved while web was a window target restores nothing for it now,
// and an end that saw it lost nothing.
func TestARestoreRunsNoCommand(t *testing.T) {
	c, _, r, _ := commandDesktop(&ledger{})
	report := survey(t, c, []core.Project{prepared(t, commandProject(""))}, nil)
	saved := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "web"}}}}}

	plan := c.RestorePlan(saved, report)
	if len(plan) != 1 || plan[0].Action != core.RestoreCommand {
		t.Errorf("plan = %+v, want web as a command", plan)
	}
	if c.Lost(saved, report) {
		t.Error("lost = true, want a command target to lose nothing")
	}
	if len(r.calls()) != 0 {
		t.Errorf("runs = %+v, want none", r.calls())
	}
}

// A shutdown closes nothing for a command target: it holds no instance, and
// the window its command raised is no one's.
func TestAShutdownClosesNoCommandTarget(t *testing.T) {
	c, wm, _, _ := commandDesktop(&ledger{})
	plan := c.ShutdownPlan(survey(t, c, []core.Project{prepared(t, commandProject(""))}, nil), "", core.ShutdownAll)
	for _, step := range plan {
		if step.Target == "web" {
			t.Errorf("plan has %+v, want no step for web", step)
		}
	}
	if len(wm.Closed) != 0 {
		t.Errorf("closed %v, want nothing", wm.Closed)
	}
}

// A command target has a desktop key like any keyed target.
func TestACommandTargetWantsItsChord(t *testing.T) {
	report := keysOf(t, hosttest.NewKeys("gnome"), prepared(t, commandProject("")))
	if r := row(t, report, "ctrl+shift+i"); r.Target != "web" {
		t.Errorf("row = %+v, want web's", r)
	}
}
