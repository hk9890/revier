// Layer L2: a launch whose window comes after its wait, as a cold editor's
// does, is bound as a late launch's when it comes (decisions.md D190). A
// window that never comes stands for a launch that hangs.
package core_test

import (
	"bytes"
	"log/slog"
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

// ideaClass is the class of every window of the editor in these tests, so
// the class alone never says which project's a window is.
const ideaClass = "jetbrains-idea"

// ideaProject is a project whose editor is one window of a shared editor
// application, told apart by a title that starts with the project's name.
func ideaProject(t *testing.T, name revier.ProjectName) core.Project {
	t.Helper()
	return prepared(t, revier.Project{
		Name: name,
		Path: "/home/user/dev/" + string(name),
		Targets: []revier.Target{{
			Name: "editor",
			Window: &revier.Realization{
				Launch: []string{"idea", "{{.Path}}"},
				Match:  revier.Match{Class: "^" + ideaClass, Title: "^{{.Name}}( |$)"},
			},
		}},
	})
}

// logged captures every log line of the test, as JSON.
func logged(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

// settleAt surveys the projects and settles the survey at now.
func settleAt(t *testing.T, c *core.Core, projects []core.Project, now time.Time) *state.State {
	t.Helper()
	r, err := c.Survey(t.Context(), projects)
	if err != nil {
		t.Fatal(err)
	}
	return c.Settle(r, projects, now)
}

// A launch that another replaces before its window came is set aside, and
// the window its own title marks is bound to it when it comes, while the
// launch that replaced it waits on: then that one's window is claimed too.
func TestAReplacedLaunchIsBoundToItsWindowWhenItComes(t *testing.T) {
	shortBindWait(t)
	out := logged(t)
	wm := hosttest.NewLateWindows("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: &ledger{}}
	a, b := ideaProject(t, "a"), ideaProject(t, "b")
	projects := []core.Project{a, b}

	for _, p := range projects {
		if _, err := press(t.Context(), c, p, "editor"); err != nil {
			t.Fatalf("press %s: %v", p.Name, err)
		}
	}
	if !strings.Contains(out.String(), `"msg":"claim: launch set aside","project":"a","target":"editor"`) ||
		strings.Contains(out.String(), "claim: launch replaced") {
		t.Fatalf("log = %s, want a's launch set aside, not replaced", out)
	}
	settleAt(t, c, projects, time.Now())

	aWindow := wm.Add("a - Main.java", ideaClass)
	st := settleAt(t, c, projects, time.Now())
	if st.Bound["a"]["editor"] != aWindow || !st.Pending("b", "editor", time.Now()) || len(st.Late) != 0 {
		t.Fatalf("bound %v, launch %+v, late %+v; want a's window bound to a, b's launch on record and none late", st.Bound, st.Launch, st.Late)
	}
	if !strings.Contains(out.String(), `"msg":"claim: late launch bound","project":"a","target":"editor"`) {
		t.Errorf("log = %s, want a's late launch bound", out)
	}

	bWindow := wm.Add("b - README.md", ideaClass)
	if st = settleAt(t, c, projects, time.Now()); st.Bound["b"]["editor"] != bWindow || st.Launch != nil {
		t.Errorf("bound %v, launch %+v; want b's window claimed for b", st.Bound, st.Launch)
	}
}

// While a launch is late, a window of its class whose title no rule matches
// is no one's: the class does not say whether it is the late launch's or
// the next one's, so neither the next launch's wait nor the survey's claim
// takes it.
func TestAWindowOfALateLaunchsClassIsNotTakenByClass(t *testing.T) {
	shortBindWait(t)
	wm := hosttest.NewLateWindows("wm")
	var once sync.Once
	wm.OnInstances = func() {
		if len(wm.Opened) == 2 {
			once.Do(func() { wm.Add("Welcome to IntelliJ IDEA", ideaClass) })
		}
	}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: &ledger{}}
	a, b := ideaProject(t, "a"), ideaProject(t, "b")
	projects := []core.Project{a, b}

	if _, err := press(t.Context(), c, a, "editor"); err != nil {
		t.Fatal(err)
	}
	if ref, _, err := c.ActivateWaiting(t.Context(), b, "editor", nil); err != nil || !ref.IsZero() {
		t.Fatalf("b's press = %v, %v; want no window bound in its wait", ref, err)
	}
	settleAt(t, c, projects, time.Now())

	wm.Add("Welcome to IntelliJ IDEA", ideaClass)
	st := settleAt(t, c, projects, time.Now())
	if len(st.Bound) != 0 || len(st.Late) != 1 || !st.Pending("b", "editor", time.Now()) {
		t.Errorf("bound %v, late %+v, launch %+v; want nothing bound, a late and b on record", st.Bound, st.Late, st.Launch)
	}
}

// A window that two targets' rules match is not a late launch's: the rule of
// "foo" matches the window of "foo bar" too. The window that only its own
// rule matches is.
func TestALateLaunchTakesNoWindowAnotherRuleMatches(t *testing.T) {
	now := time.Now()
	wm := hosttest.New("wm")
	l := &ledger{}
	l.st.SetAside(state.Launch{Project: "foo", Target: "editor", Class: "^" + ideaClass, At: now})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	projects := []core.Project{ideaProject(t, "foo"), ideaProject(t, "foo bar")}

	wm.Add("foo bar - Main.java", ideaClass)
	if st := settleAt(t, c, projects, now); len(st.Bound) != 0 || len(st.Late) != 1 {
		t.Fatalf("bound %v, late %+v; want the window both rules match left alone", st.Bound, st.Late)
	}
	foo := wm.Add("foo - Main.java", ideaClass)
	if st := settleAt(t, c, projects, now); st.Bound["foo"]["editor"] != foo || len(st.Late) != 0 {
		t.Errorf("bound %v, late %+v; want foo's own window bound", st.Bound, st.Late)
	}
}

// A slot claim takes a window by its full rule and does not ask the late
// launches, so the broader rule of "foo" takes the window that the late launch
// of "foo bar" awaits (decisions.md D201).
func TestASlotClaimTakesAWindowALateLaunchAwaits(t *testing.T) {
	now := time.Now()
	wm := hosttest.New("wm")
	l := &ledger{}
	l.st.SetAside(state.Launch{Project: "foo bar", Target: "editor", Class: "^" + ideaClass, At: now.Add(-time.Second)})
	l.st.Launch = &state.Launch{Project: "foo", Target: "editor", Class: "^" + ideaClass, At: now}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	projects := []core.Project{ideaProject(t, "foo"), ideaProject(t, "foo bar")}
	settleAt(t, c, projects, now)

	w := wm.Add("foo bar - Main.java", ideaClass)
	st := settleAt(t, c, projects, now.Add(time.Second))
	if st.Bound["foo"]["editor"] != w || len(st.Bound["foo bar"]) != 0 {
		t.Errorf("bound %v, want the window on foo's broader rule", st.Bound)
	}
}

// A window bound after its launch's wait, by a late launch or by the survey's
// claim, stays where it is: the user may have moved it (decisions.md D199).
func TestAWindowBoundAfterItsLaunchsWaitIsNotPlaced(t *testing.T) {
	now := time.Now()
	launch := state.Launch{Project: "foo", Target: "editor", Class: "^" + ideaClass, At: now}
	for name, record := range map[string]func(*state.State){
		"late launch": func(st *state.State) { st.SetAside(launch) },
		"slot claim":  func(st *state.State) { st.Launch = &launch },
	} {
		t.Run(name, func(t *testing.T) {
			wm := hosttest.New("wm")
			l := &ledger{}
			record(&l.st)
			c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
			p := ideaProject(t, "foo")
			p.Targets[0].Window.Place = "right top 75% 100%"
			projects := []core.Project{p}
			settleAt(t, c, projects, now)

			w := wm.Add("foo - Main.java", ideaClass)
			st := settleAt(t, c, projects, now.Add(time.Second))
			if st.Bound["foo"]["editor"] != w || len(wm.Placements) != 0 {
				t.Errorf("bound %v, placements %v; want the window bound and not placed", st.Bound, wm.Placements)
			}
		})
	}
}

// A target's launch past its window is set aside, not lost, and a late
// launch past LateWindow ends with its line: its window never came.
func TestALaunchPastItsWindowIsLateThenEnds(t *testing.T) {
	out := logged(t)
	now := time.Now()
	launched := now.Add(-state.BindWindow - time.Second)
	l := &ledger{st: state.State{Launch: &state.Launch{Project: "revier", Target: "editor", At: launched}}}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}

	settling(t, c, l, now)()
	st := settling(t, c, l, now)()
	if st.Launch != nil || len(st.Late) != 1 || !st.Late[0].At.Equal(launched) {
		t.Fatalf("launch %+v, late %+v; want the launch past its window set aside", st.Launch, st.Late)
	}
	if !strings.Contains(out.String(), `"msg":"claim: launch set aside","project":"revier","target":"editor"`) {
		t.Errorf("log = %s, want the launch set aside", out)
	}

	if st = settling(t, c, l, launched.Add(state.LateWindow))(); len(st.Late) != 0 {
		t.Errorf("late = %+v, want the launch past LateWindow ended", st.Late)
	}
	if !strings.Contains(out.String(), `"msg":"claim: late launch expired with no window bound","project":"revier","target":"editor"`) {
		t.Errorf("log = %s, want the late launch expired", out)
	}
}

// A target's launch hours old was never late: it is past LateWindow as well,
// and ends as a launch past its window, not set aside.
func TestALaunchHoursOldIsNotSetAside(t *testing.T) {
	out := logged(t)
	now := time.Now()
	l := &ledger{st: state.State{Launch: &state.Launch{Project: "revier", Target: "editor", At: now.Add(-3 * time.Hour)}}}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}

	settling(t, c, l, now)()
	if st := settling(t, c, l, now)(); st.Launch != nil || len(st.Late) != 0 {
		t.Errorf("launch %+v, late %+v; want the launch ended and none late", st.Launch, st.Late)
	}
	if !strings.Contains(out.String(), `"msg":"claim: launch expired with no window claimed","project":"revier","target":"editor"`) ||
		strings.Contains(out.String(), "claim: launch set aside") {
		t.Errorf("log = %s, want the launch expired, not set aside", out)
	}
}

// pageProject is a project with two windows of one browser, told apart by
// their titles.
func pageProject(t *testing.T) core.Project {
	t.Helper()
	page := func(name revier.TargetName, title string) revier.Target {
		return revier.Target{Name: name, Window: &revier.Realization{
			Launch: []string{"firefox"}, Match: revier.Match{Class: "^firefox$", Title: title},
		}}
	}
	return prepared(t, revier.Project{Name: "a", Path: "/home/user/dev/a", Targets: []revier.Target{
		page("docs", "^Docs"), page("issues", "^Issues"),
	}})
}

// A window a late launch's claim bound to one target is no other target's
// by class, of the same project either: the claim of the launch still on
// record does not bind it a second time.
func TestAWindowBoundToALateLaunchIsNotClaimedForAnotherTargetOfItsProject(t *testing.T) {
	now := time.Now()
	wm := hosttest.New("wm")
	l := &ledger{st: state.State{Launch: &state.Launch{Project: "a", Target: "issues", Class: "^firefox$", At: now}}}
	l.st.SetAside(state.Launch{Project: "a", Target: "docs", Class: "^firefox$", At: now.Add(-time.Minute)})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	projects := []core.Project{pageProject(t)}
	settleAt(t, c, projects, now)

	docs := wm.Add("Docs - Wiki", "firefox")
	st := settleAt(t, c, projects, now)
	if st.Bound["a"]["docs"] != docs || !st.Bound["a"]["issues"].IsZero() || !st.Pending("a", "issues", now) {
		t.Errorf("bound %v, launch %+v; want the window bound to docs alone and issues still on record", st.Bound, st.Launch)
	}
}

// A window bound to one target of a project that appears during the wait of
// another target's launch is not taken by class for that one.
func TestABindTakesNoHeldWindowOfItsOwnProjectByClass(t *testing.T) {
	shortBindWait(t)
	wm := hosttest.NewLateWindows("wm")
	l := &ledger{}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	var once sync.Once
	wm.OnInstances = func() {
		if len(wm.Opened) == 1 {
			once.Do(func() {
				docs := wm.Add("Docs - Wiki", "firefox")
				l.Update(func(st *state.State) bool { st.Bind("a", "docs", docs); return true })
			})
		}
	}

	if ref, _, err := c.ActivateWaiting(t.Context(), pageProject(t), "issues", nil); err != nil || !ref.IsZero() {
		t.Errorf("issues' press = %v, %v; want the window bound to docs left alone", ref, err)
	}
}

// A late launch of a project this process does not know - deleted, or of
// another configuration - is never bound here, so it holds back no window
// of its class: a bind and a claim take one by class as before.
func TestALateLaunchOfAnUnknownProjectHoldsBackNoWindow(t *testing.T) {
	shortBindWait(t)
	now := time.Now()
	wm := hosttest.NewLateWindows("wm")
	l := &ledger{}
	l.st.SetAside(state.Launch{Project: "gone", Target: "editor", Class: "^" + ideaClass, At: now})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm, Ledger: l}
	a, b := ideaProject(t, "a"), ideaProject(t, "b")
	projects := []core.Project{a, b}
	var welcome revier.TargetRef
	wm.OnInstances = func() {
		if len(wm.Opened) == 1 && welcome.IsZero() {
			welcome = wm.Add("Welcome to IntelliJ IDEA", ideaClass)
		}
	}
	settleAt(t, c, projects, now)

	if ref, _, err := c.ActivateWaiting(t.Context(), a, "editor", nil); err != nil || ref != welcome {
		t.Fatalf("a's press = %v, %v; want the new window of the class %v bound", ref, err, welcome)
	}

	if _, err := press(t.Context(), &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.NewLateWindows("wm2"), Ledger: l}, b, "editor"); err != nil {
		t.Fatal(err)
	}
	settleAt(t, c, projects, time.Now())
	stray := wm.Add("Welcome to IntelliJ IDEA", ideaClass)
	if st := settleAt(t, c, projects, time.Now()); st.Bound["b"]["editor"] != stray {
		t.Errorf("bound %v, want the new window of the class claimed for b", st.Bound)
	}
}

// editorSession is a saved session of each project's editor.
func editorSession(names ...revier.ProjectName) session.Session {
	var s session.Session
	for _, name := range names {
		s.Projects = append(s.Projects, session.Project{Name: name, Targets: []session.Target{{Name: "editor"}}})
	}
	return s
}

// A restore over a cold editor launches each step's editor and no window
// comes in any wait. The windows come minutes later, in reverse order, and
// each is bound to its own step: no launch was lost to the next one.
func TestAColdRestoreBindsEachEditorWhenItsWindowComes(t *testing.T) {
	// A step's deadline is twice its wait, which must outlast one poll.
	defer func(w time.Duration) { core.BindWait = w }(core.BindWait)
	core.BindWait = 2 * core.BindPoll
	out := logged(t)
	wm := hosttest.NewLateWindows("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	projects := []core.Project{ideaProject(t, "a"), ideaProject(t, "b"), ideaProject(t, "c")}
	report := survey(t, c, projects, nil)
	c.Ledger = &ledger{}

	restored, _ := c.Restore(t.Context(), editorSession("a", "b", "c"), report, projects, core.RestoreOpts{})
	if _, pending, _ := restored.Counts(); pending != 3 {
		t.Fatalf("pending = %d (%+v), want 3: no window came in any wait", pending, restored)
	}
	if strings.Contains(out.String(), "claim: launch replaced") {
		t.Errorf("log = %s, want no editor's launch replaced", out)
	}
	settleAt(t, c, projects, time.Now())

	want := map[revier.ProjectName]revier.TargetRef{}
	for _, name := range []revier.ProjectName{"c", "b", "a"} {
		want[name] = wm.Add(string(name)+" - Main.java", ideaClass)
		settleAt(t, c, projects, time.Now())
	}
	st := c.Ledger.State()
	for name, ref := range want {
		if st.Bound[name]["editor"] != ref {
			t.Errorf("%s's editor bound to %v, want %v", name, st.Bound[name]["editor"], ref)
		}
	}
	if st.Launch != nil || len(st.Late) != 0 {
		t.Errorf("launch %+v, late %+v; want none left", st.Launch, st.Late)
	}
}

// A restore over a warm editor binds each window in its wait, as before:
// nothing is pending, late or set aside.
func TestAWarmRestoreLeavesNothingLate(t *testing.T) {
	out := logged(t)
	wm := hosttest.New("wm")
	wm.Detached = true
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	projects := []core.Project{ideaProject(t, "a"), ideaProject(t, "b"), ideaProject(t, "c")}
	report := survey(t, c, projects, nil)
	l := &ledger{}
	c.Ledger = l

	restored, _ := c.Restore(t.Context(), editorSession("a", "b", "c"), report, projects, core.RestoreOpts{})
	if opened, pending, _ := restored.Counts(); opened != 3 || pending != 0 {
		t.Errorf("opened %d, pending %d; want 3 opened and none pending", opened, pending)
	}
	if st := l.State(); len(st.Late) != 0 || strings.Contains(out.String(), "claim: launch set aside") {
		t.Errorf("late %+v, log %s; want none late and none set aside", st.Late, out)
	}
}
