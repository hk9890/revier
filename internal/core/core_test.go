package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// prepared is the form Go and Survey take: rendered and compiled once, as
// config.Load does for a real project file.
func prepared(t *testing.T, p revier.Project) core.Project {
	t.Helper()
	out := core.PrepareProject(p)
	return out
}

// editor is a window-realized target; diff has both realizations; home is the
// project's workspace on the runtime.
func project() revier.Project {
	return revier.Project{
		Name: "revier",
		Path: "/home/hans/dev/github/revier",
		Targets: []revier.Target{
			{
				Name: "home", Home: true,
				Runtime: &revier.Realization{
					Launch: []string{"kitty"},
					Match:  revier.Match{Title: "^session:revier$"},
				},
			},
			{
				Name: "editor", Key: "ctrl-o",
				Window: &revier.Realization{
					Launch: []string{"code", "/home/hans/dev/github/revier"},
					Match:  revier.Match{Class: "^code$"},
				},
			},
			{
				Name: "diff", Key: "ctrl-shift-d",
				Window: &revier.Realization{
					Launch: []string{"meld"}, Match: revier.Match{Class: "^meld$"},
				},
				Runtime: &revier.Realization{
					Launch: []string{"nvim", "-d"}, Match: revier.Match{Title: "^diff:revier$"},
				},
			},
		},
	}
}

func TestResolvePrefersWindowByDefault(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	diff, _ := project().Target("diff")

	h, _, err := c.Resolve(diff)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if h.Name() != "wm" {
		t.Errorf("host = %s, want wm: a desktop user expects a separate window", h.Name())
	}
}

func TestResolveHonoursPrefer(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm")}
	diff, _ := project().Target("diff")
	diff.Prefer = revier.HostRuntime

	h, real, err := c.Resolve(diff)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if h.Name() != "rt" {
		t.Errorf("host = %s, want rt", h.Name())
	}
	if real.Launch[0] != "nvim" {
		t.Errorf("launch = %v, want the runtime realization", real.Launch)
	}
}

// The headless case: no compositor adapter, so a target with a runtime
// realization still resolves and a window-only one reports ErrNoHost.
func TestResolveWithoutWindowHost(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	p := project()

	diff, _ := p.Target("diff")
	h, real, err := c.Resolve(diff)
	if err != nil {
		t.Fatalf("diff should fall back to the runtime: %v", err)
	}
	if h.Name() != "rt" || real.Launch[0] != "nvim" {
		t.Errorf("got %s %v, want the runtime realization", h.Name(), real.Launch)
	}

	editor, _ := p.Target("editor")
	if _, _, err := c.Resolve(editor); !errors.Is(err, core.ErrNoHost) {
		t.Errorf("editor error = %v, want ErrNoHost", err)
	}
}

func TestGoRaisesAnExistingInstance(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Add("Visual Studio Code", "code")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	ref := res.Ref
	if len(wm.Opened) != 0 {
		t.Errorf("Open called %d times; an existing instance must be raised, not launched", len(wm.Opened))
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != ref {
		t.Errorf("Focuses = %v, want one call for %v", wm.Focuses, ref)
	}
}

// A window host launches a process and cannot name the window it produces.
// Go must not then fail on focusing nothing: the compositor focuses the new
// window, and the next press finds it through Match.
func TestGoAcceptsADetachedOpen(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Detached = true
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if !res.Ref.IsZero() || !res.Launched {
		t.Errorf("result = %+v, want a launch with no ref: the host could not name the window", res)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("focuses = %v, want none: there is no ref to focus", wm.Focuses)
	}
	// The window exists now, so the next press raises it.
	again, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
	if err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if again.Ref.IsZero() || len(wm.Opened) != 1 {
		t.Errorf("second press: ref %+v, opened %d; want the existing window raised", again.Ref, len(wm.Opened))
	}
}

func TestGoOpensWhenNothingMatches(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, project()), "editor", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(wm.Opened) != 1 {
		t.Fatalf("Open called %d times, want 1", len(wm.Opened))
	}
	if got := wm.Opened[0].Launch[0]; got != "code" {
		t.Errorf("launched %q, want code", got)
	}
}

// The round trip: pressing the editor key while the editor already holds focus
// returns to home rather than doing nothing.
func TestGoTogglesBackToHome(t *testing.T) {
	wm := hosttest.New("wm")
	rt := hosttest.NewRuntime("rt")
	editorRef := wm.Add("Visual Studio Code", "code")
	homeRef := rt.Add("session:revier", "kitty")
	wm.SetFocus(editorRef)
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref != homeRef {
		t.Errorf("returned %v, want the home ref %v", res.Ref, homeRef)
	}
	if len(rt.Focuses) != 1 || rt.Focuses[0] != homeRef {
		t.Errorf("runtime focuses = %v, want one call for home", rt.Focuses)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none: the editor already had focus", wm.Focuses)
	}
}

func TestGoOnHomeDoesNotToggle(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	homeRef := rt.Add("session:revier", "kitty")
	rt.SetFocus(homeRef)
	c := &core.Core{Runtime: rt}

	if _, err := c.Go(context.Background(), prepared(t, project()), "home", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Focuses) != 1 {
		t.Errorf("focuses = %v, want one: home must not toggle away from itself", rt.Focuses)
	}
}

func TestGoRejectsUnknownTarget(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	if _, err := c.Go(context.Background(), prepared(t, project()), "absent", nil); !errors.Is(err, core.ErrNoTarget) {
		t.Errorf("err = %v, want ErrNoTarget", err)
	}
}

// A match that constrains nothing would select whichever instance the host
// happened to list first, so the target that declares one is refused. The
// refusal is that target's: the project keeps every other target.
func TestPrepareRejectsUnboundedMatch(t *testing.T) {
	p := revier.Project{Name: "loose", Targets: []revier.Target{
		{Name: "loose", Runtime: &revier.Realization{Launch: []string{"x"}}},
	}}

	err := core.PrepareProject(p).TargetErr(0)
	if !errors.Is(err, core.ErrUnboundedMatch) {
		t.Fatalf("err = %v, want ErrUnboundedMatch", err)
	}
	if !strings.Contains(err.Error(), `"loose"`) || !strings.Contains(err.Error(), "runtime") {
		t.Errorf("error should name the target and the realization: %v", err)
	}
}

// A pattern that does not parse and a template that does not render refuse
// their own target, naming it and the realization. Neither is a keystroke
// failure, and neither costs the project its other targets.
func TestPrepareReportsBadPatternsAndTemplates(t *testing.T) {
	cases := []struct {
		name string
		real revier.Realization
		want string
	}{
		{"bad regex", revier.Realization{Launch: []string{"x"}, Match: revier.Match{Class: "("}}, "error parsing regexp"},
		{"missing key", revier.Realization{Launch: []string{"{{.Vars.absent}}"}, Match: revier.Match{Class: "^x$"}}, "absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := revier.Project{Name: "broken", Targets: []revier.Target{{Name: "home", Home: true, Window: &tc.real}}}
			out := core.Prepare([]revier.Project{p})
			if len(out) != 1 {
				t.Fatalf("%d projects, want the one it was given", len(out))
			}
			err := out[0].TargetErr(0)
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range []string{tc.want, `"home"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should mention %q", err, want)
				}
			}
		})
	}
}

// Prepare hands hosts literals: after it, no realization carries a template.
func TestPreparedProjectIsRendered(t *testing.T) {
	p := prepared(t, revier.Project{
		Name: "revier", Path: "/p",
		Targets: []revier.Target{{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "{{.Name}}", Launch: []string{"x", "{{.Path}}"}, Match: revier.Match{Title: "^{{.Name}}$"},
		}}},
	})
	r := p.Targets[0].Runtime
	if r.Name != "revier" || r.Launch[1] != "/p" || r.Match.Title != "^revier$" {
		t.Errorf("realization not rendered: %+v", r)
	}
}

func TestSurveyReportsRunningAndAvailability(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	c := &core.Core{Runtime: rt} // no window host

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	views := report.Views
	v := views[0]
	if !v.Running {
		t.Error("project should be running: its home instance exists")
	}
	byName := map[revier.TargetName]revier.TargetView{}
	for _, tv := range v.Targets {
		byName[tv.Name] = tv
	}
	if byName["editor"].Available {
		t.Error("editor is window-only and no window host is configured")
	}
	if !byName["diff"].Available || byName["diff"].Host != "rt" {
		t.Errorf("diff = %+v, want available on rt", byName["diff"])
	}
}

func TestSurveyReportsAgentAttention(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude: waiting"},
		revier.Panel{ID: "2", Kind: revier.PanelShell, Title: "zsh"},
	)
	c := &core.Core{
		Runtime: rt,
		Probes: []revier.AgentProbe{&hosttest.FakeProbe{
			Harness: "claude", Marker: "claude",
			State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention},
		}},
	}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	views := report.Views
	if len(views[0].Agents) != 1 {
		t.Fatalf("agents = %d, want 1: only agent panels are probed", len(views[0].Agents))
	}
	if !views[0].Attention() {
		t.Error("Attention() should report true; it is what the TUI sorts on")
	}
}

// A shell in the foreground is not an agent, even where a probe still
// recognises the panel: the marker a harness sets outlives it in a --hold
// window, and the dashboard would report the gone agent's last state - here,
// that it wants the human - for as long as the window stays open. `revier
// agent` refuses the same panel.
func TestSurveySkipsAShellLeftWhereAnAgentWas(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "claude"})
	c := &core.Core{
		Runtime: rt,
		Probes: []revier.AgentProbe{&hosttest.FakeProbe{
			Harness: "claude", Marker: "claude",
			State: revier.AgentState{Harness: "claude", Status: revier.StatusAttention},
		}},
	}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if agents := report.Views[0].Agents; len(agents) != 0 {
		t.Errorf("agents = %+v, want none: the panel's foreground is a shell", agents)
	}
}

// One broken harness must not blank the dashboard.
func TestSurveySurvivesAProbeError(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude"})
	c := &core.Core{
		Runtime: rt,
		Probes:  []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Err: errors.New("boom")}},
	}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey should not fail: %v", err)
	}
	if got := report.Views[0].Agents[0].State.Status; got != revier.StatusUnknown {
		t.Errorf("status = %v, want unknown", got)
	}
}

// Ids are host-scoped: a window id and a pane id are unrelated numbers. This
// pins the bug where toggle-back compared them across hosts, which made a
// runtime target either never toggle or toggle on a coincidence.
func TestToggleBackIgnoresOtherHostsIDs(t *testing.T) {
	wm := hosttest.New("wm")
	rt := hosttest.NewRuntime("rt")
	// Same numeric id on both hosts, meaning entirely different things.
	rt.Add("diff:revier", "kitty")
	wmRef := wm.Add("Some Window", "other")
	wm.SetFocus(wmRef)

	c := &core.Core{Runtime: rt, Window: wm}
	p := revier.Project{Name: "revier", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}}

	res, err := c.Go(context.Background(), prepared(t, p), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.Host != "rt" {
		t.Fatalf("landed on %s, want the runtime target", res.Ref.Host)
	}
	if len(rt.Focuses) != 1 {
		t.Errorf("runtime focuses = %v, want exactly one: the diff target", rt.Focuses)
	}
}

// With no window host the runtime is the focus authority, so toggle-back works
// for runtime targets - the headless and SSH case.
func TestToggleBackWorksOnRuntimeWhenItIsTheAuthority(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	homeRef := rt.Add("session:revier", "kitty")
	diffRef := rt.Add("diff:revier", "kitty")
	rt.SetFocus(diffRef)

	c := &core.Core{Runtime: rt}
	p := revier.Project{Name: "revier", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}}

	res, err := c.Go(context.Background(), prepared(t, p), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref != homeRef {
		t.Errorf("returned %v, want home %v", res.Ref, homeRef)
	}
}

// A toggle-back lands on home, and says so. A caller that pinned the pressed
// target to where the key landed would bind diff to the home window, and every
// later press of diff would find home through that binding and go nowhere
// else.
func TestAToggleBackNamesHomeAsWhereItLanded(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	homeRef := rt.Add("session:revier", "kitty")
	diffRef := rt.Add("diff:revier", "kitty")
	c := &core.Core{Runtime: rt}
	p := prepared(t, revier.Project{Name: "revier", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}})
	bound := core.Bindings{}
	press := func() core.Result {
		t.Helper()
		res, err := c.Go(context.Background(), p, "diff", bound)
		if err != nil {
			t.Fatalf("Go: %v", err)
		}
		bound[res.Target] = res.Ref // what goTarget does with the result
		return res
	}

	rt.SetFocus(diffRef)
	if res := press(); res.Target != "home" || res.Ref != homeRef {
		t.Fatalf("toggle-back = %+v, want it to land on home %v", res, homeRef)
	}
	if res := press(); res.Target != "diff" || res.Ref != diffRef {
		t.Errorf("the press after it = %+v, want diff %v: the key must still reach its target", res, diffRef)
	}
}

// osWindowProject is a workspace and a runtime-only diff target, as a kitty
// user has them: both are OS windows a window host also lists.
func osWindowProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff:revier", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}}
}

// osWindowHosts builds a runtime that reports OSWindows and a window host that
// sees the same two windows, and returns the window host's refs for them.
func osWindowHosts() (*hosttest.FakeRuntime, *hosttest.Fake, revier.TargetRef, revier.TargetRef) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	wm := hosttest.New("wm")
	homeRt := rt.Add("session:revier", "kitty")
	diffRt := rt.Add("diff:revier", "kitty")
	_ = homeRt
	_ = diffRt
	// The window host reports the pid the runtime reports (hosttest assigns
	// 1000+id), because every OS window of one kitty process shares it.
	homeWm := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	diffWm := wm.AddInstance(revier.Instance{Title: "diff:revier", Class: "kitty", PID: 1002})
	wm.AddInstance(revier.Instance{Title: "Some Editor", Class: "code", PID: 4242})
	return rt, wm, homeWm, diffWm
}

// A terminal on Wayland cannot raise its own OS window, so focusing a runtime
// target also raises that window through the window host.
func TestGoRaisesTheOSWindowOfARuntimeTarget(t *testing.T) {
	rt, wm, _, diffWm := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.Host != "kitty" {
		t.Fatalf("landed on %s, want the runtime target", res.Ref.Host)
	}
	if len(rt.Focuses) != 1 {
		t.Errorf("runtime focuses = %v, want the diff window", rt.Focuses)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != diffWm {
		t.Errorf("window focuses = %v, want the OS window holding diff (%v)", wm.Focuses, diffWm)
	}
}

// Toggle-back for a runtime target on a desktop, resolved: the window host is
// still the only authority on focus, and it is asked about the OS window that
// holds the runtime instance.
func TestToggleBackThroughTheOSWindow(t *testing.T) {
	rt, wm, homeWm, diffWm := osWindowHosts()
	wm.SetFocus(diffWm)
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.Title != "session:revier" {
		t.Errorf("returned %v, want home: the diff OS window had focus", res.Ref)
	}
	if n := len(wm.Focuses); n != 1 || wm.Focuses[0] != homeWm {
		t.Errorf("window focuses = %v, want home's OS window raised", wm.Focuses)
	}
}

// The false positive D16 refuses: the runtime target exists but the user is
// looking at the editor, so the key goes to the target and never home.
func TestToggleBackNeedsTheOSWindowFocused(t *testing.T) {
	rt, wm, _, diffWm := osWindowHosts()
	editor := revier.TargetRef{Host: "wm", ID: "3"}
	wm.SetFocus(editor)
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.Title != "diff:revier" {
		t.Errorf("returned %v, want diff: nothing of this project had focus", res.Ref)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != diffWm {
		t.Errorf("window focuses = %v, want diff's OS window raised", wm.Focuses)
	}
}

// A runtime that does not report OSWindows - a multiplexer - keeps the D16
// behaviour: its refs are never judged by the window host, and never raised.
func TestNoBridgeWithoutOSWindows(t *testing.T) {
	rt, wm, _, diffWm := osWindowHosts()
	rt.SetCapabilities(revier.Capabilities{Layout: true})
	wm.SetFocus(diffWm)
	c := &core.Core{Runtime: rt, Window: wm}

	res, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.Title != "diff:revier" {
		t.Errorf("returned %v, want diff: a multiplexer's windows are not OS windows", res.Ref)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none", wm.Focuses)
	}
}

// Same title, different process: not the same window. The pid filter is what
// keeps a stray window with a matching title from being raised, and with no
// window of its own to raise the press is refused rather than half done.
func TestBridgeRejectsAPIDMismatch(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	wm := hosttest.New("wm")
	rt.Add("diff:revier", "kitty") // pid 1001
	rt.Add("session:revier", "kitty")
	other := wm.AddInstance(revier.Instance{Title: "diff:revier", Class: "kitty", PID: 9999})
	wm.SetFocus(other)
	c := &core.Core{Runtime: rt, Window: wm}

	_, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff", nil)
	if !errors.Is(err, core.ErrUnraisable) {
		t.Fatalf("err = %v, want ErrUnraisable: the only window of that title belongs to another process", err)
	}
	if len(wm.Focuses) != 0 || len(rt.Focuses) != 0 {
		t.Errorf("focuses = %v %v, want none: nothing moves when the window cannot be raised", wm.Focuses, rt.Focuses)
	}
}

// Two unnamed windows in one process cannot be told apart, so neither is
// raised. Focusing inside the terminal anyway is what made GNOME show "is
// ready" in place of the window.
func TestAnUnidentifiedOSWindowIsNotFocusedInsideTheTerminal(t *testing.T) {
	rt := unnamedRuntime(t, 4242)
	wm := hosttest.New("wm")
	for _, title := range []string{"session:revier", "session:setup"} {
		wm.AddInstance(revier.Instance{Title: title, Class: "kitty", PID: 4242})
	}
	c := &core.Core{Runtime: rt, Window: wm}
	bound := core.Bindings{"home": {Host: "rt", ID: "1"}}

	_, err := c.Go(context.Background(), prepared(t, project()), "home", bound)
	if !errors.Is(err, core.ErrUnraisable) {
		t.Fatalf("err = %v, want ErrUnraisable", err)
	}
	if len(rt.Focuses) != 0 || len(wm.Focuses) != 0 {
		t.Errorf("focuses = %v %v, want none", rt.Focuses, wm.Focuses)
	}
}

// A binding wins over the rule: once a key landed on an instance, the next
// press finds it by id, whatever the application did to its title since.
func TestGoPrefersTheBoundRef(t *testing.T) {
	wm := hosttest.New("wm")
	// The editor's title no longer matches the rule - it opened on the
	// project and now names a file. The class is what D21 binds by and what
	// a binding is re-checked against, so it still holds.
	editor := wm.AddInstance(revier.Instance{Title: "main.go - somewhere else", Class: "code"})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", core.Bindings{"editor": editor})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref != editor || len(wm.Opened) != 0 {
		t.Errorf("result = %+v, opened %d; want the bound window raised", res, len(wm.Opened))
	}
}

// A binding whose instance is gone falls back to the rule, and to the launch.
func TestGoIgnoresADeadBinding(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	dead := revier.TargetRef{Host: "wm", ID: "999"}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", core.Bindings{"editor": dead})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if !res.Launched || len(wm.Opened) != 1 {
		t.Errorf("result = %+v, opened %d; want the editor launched", res, len(wm.Opened))
	}
}

// Bind takes the window of the launched class even while its title has not
// settled, and never one of another class.
func TestBindTakesTheNewWindowOfTheClass(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Add("Some Window", "other")
	c := &core.Core{Window: wm}
	p := prepared(t, project())
	before, _ := wm.Instances(context.Background())

	stray := wm.AddInstance(revier.Instance{Title: "Pull requests", Class: "chromium"})
	splash := wm.AddInstance(revier.Instance{Title: "Visual Studio Code", Class: "code"})
	inst, ok, err := c.Bind(context.Background(), p, "editor", before, 0)
	if err != nil || !ok {
		t.Fatalf("Bind = %v, %v, %v; want the code window", inst, ok, err)
	}
	if inst.Ref != splash {
		t.Errorf("bound %v, want the code window %v, not the chromium one %v", inst.Ref, splash, stray)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != splash {
		t.Errorf("focuses = %v, want the bound window raised", wm.Focuses)
	}
}

func TestBindLeavesAmbiguityAlone(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Window: wm}
	// A rule on class and title: two windows of the class whose titles have
	// not settled are two class candidates, and the launch does not say which.
	p := prepared(t, revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "h", Launch: []string{"x"}, Match: revier.Match{Title: "^h$"}}},
		{Name: "editor", Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$", Title: "revier"}}},
	}})
	wm.AddInstance(revier.Instance{Title: "one", Class: "code"})
	wm.AddInstance(revier.Instance{Title: "two", Class: "code"})
	if _, ok, _ := c.Bind(context.Background(), p, "editor", nil, 0); ok {
		t.Error("two new windows of the class at once must bind nothing")
	}
	// One of them settles into the full rule: that one is it.
	wm.AddInstance(revier.Instance{Title: "main.go - revier", Class: "code"})
	inst, ok, _ := c.Bind(context.Background(), p, "editor", nil, 0)
	if !ok || inst.Title != "main.go - revier" {
		t.Errorf("Bind = %+v, %v; want the window the full rule matches", inst, ok)
	}
	if _, ok, _ := c.Bind(context.Background(), p, "home", nil, 0); ok {
		t.Error("a runtime target has no window to bind")
	}
}

// A survey reports a bound instance as the target's, ahead of the rule.
func TestSurveyUsesBindings(t *testing.T) {
	wm := hosttest.New("wm")
	// A title that moved, which is what a binding exists to survive (D21).
	editor := wm.AddInstance(revier.Instance{Title: "renamed", Class: "code"})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())},
		map[revier.ProjectName]core.Bindings{"revier": {"editor": editor}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tv := range report.Views[0].Targets {
		if tv.Name == "editor" && tv.Ref != editor {
			t.Errorf("editor ref = %+v, want the bound %v", tv.Ref, editor)
		}
	}
	if len(report.Instances) != 1 {
		t.Errorf("instances = %d, want every host's listing for pruning", len(report.Instances))
	}
}

// An attachment still listed follows the project's targets, marked attached
// and carrying the title it has now; one whose window is gone is left out.
func TestSurveyListsLiveAttachments(t *testing.T) {
	wm := hosttest.New("wm")
	live := wm.Add("Pull requests", "chromium")
	gone := revier.TargetRef{Host: "wm", ID: "999", Title: "closed"}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil,
		map[revier.ProjectName][]revier.TargetRef{"revier": {{Host: "wm", ID: live.ID, Title: "stale"}, gone}})
	if err != nil {
		t.Fatal(err)
	}
	var attached []revier.TargetView
	for _, tv := range report.Views[0].Targets {
		if tv.Attached {
			attached = append(attached, tv)
		}
	}
	if len(attached) != 1 || attached[0].Ref != live || attached[0].Host != "wm" || !attached[0].Available {
		t.Errorf("attached = %+v, want the live window %v alone", attached, live)
	}
}

// Claim settles a launch with the one window that appeared after it. A
// target's launch binds by class, title settled or not; an action's launch
// attaches a window no declared target matches. Every bound is a way of
// claiming nothing rather than the wrong thing.
func TestClaimBounds(t *testing.T) {
	c := &core.Core{Window: hosttest.New("wm")}
	p := prepared(t, project())
	projects := []core.Project{p}
	ref := func(id string) revier.TargetRef { return revier.TargetRef{Host: "wm", ID: id} }
	before := []revier.Instance{{Ref: ref("1"), Title: "old", Class: "x"}}
	stray := revier.Instance{Ref: ref("2"), Title: "Pull requests", Class: "chromium"}
	editor := revier.Instance{Ref: ref("3"), Title: "Visual Studio Code", Class: "code"}
	unsettled := revier.Instance{Ref: ref("4"), Title: "", Class: "code"}
	now := time.Now()
	action := core.Launch{Project: p, At: now.Add(-time.Second)}
	target := core.Launch{Project: p, Target: "editor", At: now.Add(-20 * time.Second)}

	got, ok := c.Claim(before, append(before, stray), action, now, projects)
	if !ok || got.Ref != stray.Ref || got.Target != "" {
		t.Errorf("action claim = %+v, %v; want the stray attached", got, ok)
	}
	if _, ok := c.Claim(before, append(before, editor), action, now, projects); ok {
		t.Error("an action's launch must not attach a declared target's window")
	}
	got, ok = c.Claim(before, append(before, unsettled), target, now, projects)
	if !ok || got.Ref != unsettled.Ref || got.Target != "editor" {
		t.Errorf("target claim = %+v, %v; want the unsettled code window bound to editor", got, ok)
	}
	if _, ok := c.Claim(before, append(before, stray), target, now, projects); ok {
		t.Error("a target's launch must not bind a window of another class")
	}
	stale := core.Launch{Project: p, At: now.Add(-core.ClaimWindow - time.Second)}
	if _, ok := c.Claim(before, append(before, stray), stale, now, projects); ok {
		t.Error("an action's launch older than the claim window must not claim")
	}
	old := core.Launch{Project: p, Target: "editor", At: now.Add(-core.BindWindow - time.Second)}
	if _, ok := c.Claim(before, append(before, unsettled), old, now, projects); ok {
		t.Error("a target's launch older than the bind window must not bind")
	}
	if _, ok := c.Claim(before, append(before, stray, revier.Instance{Ref: ref("5"), Class: "other"}), action, now, projects); ok {
		t.Error("two candidates at once is ambiguity, and claims nothing")
	}
	if _, ok := c.Claim(append(before, stray), append(before, stray), action, now, projects); ok {
		t.Error("a window already present is not new")
	}
	if got, ok := c.ClaimEvent(unsettled, target, now, projects); !ok || got.Target != "editor" {
		t.Errorf("event path = %+v, %v; want the same binding", got, ok)
	}
	if _, ok := c.ClaimEvent(editor, action, now, projects); ok {
		t.Error("the event path must not attach a declared target's window")
	}
	// A terminal's OS window carries the title its runtime rule matches, so
	// another project's workspace opening after an action is declared too.
	workspace := revier.Instance{Ref: ref("6"), Title: "session:revier", Class: "kitty"}
	if _, ok := c.Claim(before, append(before, workspace), action, now, projects); ok {
		t.Error("an action's launch must not attach a window a runtime rule declares")
	}
}

// A project file outlives the checkout it names. The survey reports whether
// the directory is there, so the surface can say so before a target launches
// into a path that is not.
func TestSurveyReportsWhetherTheProjectPathExists(t *testing.T) {
	here := project()
	here.Path = t.TempDir()
	gone := project()
	gone.Name, gone.Path = "gone", filepath.Join(t.TempDir(), "never-cloned")

	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	report, err := c.Survey(context.Background(), []core.Project{
		prepared(t, here), prepared(t, gone),
	}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if !report.Views[0].PathExists {
		t.Errorf("%s: PathExists = false for a directory that is there", report.Views[0].Project.Path)
	}
	if report.Views[1].PathExists {
		t.Errorf("%s: PathExists = true for a directory that is not", report.Views[1].Project.Path)
	}
}

// osWindow is a runtime that owns OS windows, as kitty does, with one
// instance that has no identity of its own - the state a window opened by the
// shell session tool is in.
func unnamedRuntime(t *testing.T, pid int) *hosttest.FakeRuntime {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	rt.AddInstance(revier.Instance{
		Ref:    revier.TargetRef{Host: "rt", ID: "1"},
		PID:    pid,
		Panels: []revier.Panel{{ID: "1", Kind: revier.PanelAgent, Title: "claude"}},
	})
	return rt
}

// A window the shell tool opened has kitty's default name, so the runtime
// reports no title and no rule can match it. The window manager sees the
// title. The core pairs them by process, and the project reads as running.
func TestAnUnnamedRuntimeWindowTakesTheWindowManagersTitle(t *testing.T) {
	rt := unnamedRuntime(t, 4242)
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{
		Ref:   revier.TargetRef{Host: "wm", ID: "w1"},
		Title: "session:revier", Class: "kitty", PID: 4242,
	})
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{&hosttest.FakeProbe{
		Harness: "claude", Marker: "claude",
		State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle},
	}}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	if !v.Running {
		t.Fatal("project should be running: its window is open, under a name only the window manager sees")
	}
	if len(v.Agents) != 1 {
		t.Errorf("agents = %+v, want the one the runtime reports: identity must not cost the panels", v.Agents)
	}
}

// The point of finding it is that the next press raises it. A second window
// beside the one already open is the failure this fixes.
func TestGoRaisesAnUnnamedWindowInsteadOfOpeningASecond(t *testing.T) {
	rt := unnamedRuntime(t, 4242)
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{
		Ref:   revier.TargetRef{Host: "wm", ID: "w1"},
		Title: "session:revier", Class: "kitty", PID: 4242,
	})
	c := &core.Core{Runtime: rt, Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, project()), "home", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("runtime opened %d instances, want none: the window was already there", len(rt.Opened))
	}
	if len(rt.Focuses) == 0 && len(wm.Focuses) == 0 {
		t.Error("nothing was focused, so nothing was raised")
	}
}

// D19 rejected the process id for pairing a pane to a window, because one
// kitty process can own several OS windows. That case is refused here rather
// than guessed at: a wrong title would send a keypress to the wrong window.
func TestTwoWindowsOfOneProcessAreLeftUnidentified(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	for _, id := range []string{"1", "2"} {
		rt.AddInstance(revier.Instance{Ref: revier.TargetRef{Host: "rt", ID: id}, PID: 4242})
	}
	wm := hosttest.New("wm")
	for i, title := range []string{"session:revier", "session:setup"} {
		wm.AddInstance(revier.Instance{
			Ref:   revier.TargetRef{Host: "wm", ID: fmt.Sprintf("w%d", i)},
			Title: title, Class: "kitty", PID: 4242,
		})
	}
	c := &core.Core{Runtime: rt, Window: wm}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if report.Views[0].Running {
		t.Error("a process owning two windows must not lend either title to the other")
	}
}

// A named window launched into the same process - a ticket viewer beside the
// session - claims its own title, which leaves one unnamed window and one
// title. The pairing is not ambiguous, and the next press raises the session.
func TestANamedSiblingWindowDoesNotHideTheUnnamedOne(t *testing.T) {
	rt := unnamedRuntime(t, 4242)
	rt.AddInstance(revier.Instance{
		Ref:   revier.TargetRef{Host: "rt", ID: "2", Title: "tickets:revier"},
		Title: "tickets:revier", PID: 4242,
	})
	wm := hosttest.New("wm")
	for _, title := range []string{"tickets:revier", "session:revier"} {
		wm.AddInstance(revier.Instance{
			Ref:   revier.TargetRef{Host: "wm", ID: title},
			Title: title, Class: "kitty", PID: 4242,
		})
	}
	c := &core.Core{Runtime: rt, Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, project()), "home", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("runtime opened %d instances, want none: the window was already there", len(rt.Opened))
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0].Title != "session:revier" {
		t.Errorf("window host focused %+v, want session:revier raised", wm.Focuses)
	}
}

// A named window the window host does not list may be the window that is
// left over, under a title the named window was not given. Lending that title
// to the unnamed window would raise the named one on the next press. Two named
// windows of one title are two windows to find, not one.
func TestAnUnlistedNamedSiblingLeavesTheUnnamedOneUnidentified(t *testing.T) {
	for name, named := range map[string]int{"its only named sibling": 1, "one of two named siblings of one title": 2} {
		t.Run(name, func(t *testing.T) {
			rt := unnamedRuntime(t, 4242)
			wm := hosttest.New("wm")
			for n := range named {
				rt.AddInstance(revier.Instance{
					Ref:   revier.TargetRef{Host: "rt", ID: fmt.Sprint(n + 2), Title: "tickets:revier"},
					Title: "tickets:revier", PID: 4242,
				})
				if n > 0 {
					wm.AddInstance(revier.Instance{Title: "tickets:revier", Class: "kitty", PID: 4242})
				}
			}
			wm.AddInstance(revier.Instance{Title: "taskmgr", Class: "kitty", PID: 4242})
			c := &core.Core{Runtime: rt, Window: wm}

			report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
			if err != nil {
				t.Fatalf("Survey: %v", err)
			}
			for _, inst := range report.Instances {
				if inst.Ref.Host == "rt" && inst.Ref.ID == "1" && inst.Title != "" {
					t.Errorf("the unnamed window took %q, which may be its named sibling's window", inst.Title)
				}
			}
		})
	}
}

// With no window host there is nothing to ask, and the behaviour is what it
// was: an unnamed window stays unidentified.
func TestWithNoWindowHostAnUnnamedWindowStaysUnidentified(t *testing.T) {
	c := &core.Core{Runtime: unnamedRuntime(t, 4242)}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if report.Views[0].Running {
		t.Error("without a window host the title cannot be learned")
	}
}

// A runtime that does not own OS windows - tmux - is untouched by this: its
// instances are panes, and a pane has no window manager title.
func TestARuntimeWithoutOSWindowsIsNotIdentified(t *testing.T) {
	rt := hosttest.NewRuntime("rt") // no OSWindows capability
	rt.AddInstance(revier.Instance{Ref: revier.TargetRef{Host: "rt", ID: "1"}, PID: 4242})
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{
		Ref:   revier.TargetRef{Host: "wm", ID: "w1"},
		Title: "session:revier", Class: "kitty", PID: 4242,
	})
	c := &core.Core{Runtime: rt, Window: wm}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if report.Views[0].Running {
		t.Error("a pane is not an OS window; its process tells nothing about a window title")
	}
}

// A desktop key has no useful working directory, so the project comes from
// the window in front of the user. The rules a project already declares are
// the mapping.
func TestProjectOfFocusedUsesTheDeclaredRules(t *testing.T) {
	wm := hosttest.New("wm")
	other := wm.Add("something else", "firefox")
	editor := wm.Add("revier - README.md", "code")
	c := &core.Core{Window: wm}
	projects := []core.Project{prepared(t, project())}

	wm.SetFocus(editor)
	p, ok, err := c.ProjectOfFocused(context.Background(), projects)
	if err != nil || !ok {
		t.Fatalf("ProjectOfFocused: ok=%v err=%v, want the project of the editor window", ok, err)
	}
	if p.Name != "revier" {
		t.Errorf("project = %q, want revier", p.Name)
	}

	wm.SetFocus(other)
	if _, ok, err := c.ProjectOfFocused(context.Background(), projects); ok || err != nil {
		t.Errorf("ok=%v err=%v, want no project for a window no rule claims", ok, err)
	}
}

// The focused window is reported by the window host while the target it
// matches may be realized by the runtime. Requiring the two hosts to agree
// would answer nothing, so the rule is matched wherever the window came from.
func TestProjectOfFocusedMatchesARuntimeRuleOnAWindow(t *testing.T) {
	wm := hosttest.New("wm")
	session := wm.AddInstance(revier.Instance{
		Ref:   revier.TargetRef{Host: "wm", ID: "w1"},
		Title: "session:revier", Class: "kitty", PID: 4242,
	})
	wm.SetFocus(session)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	p, ok, err := c.ProjectOfFocused(context.Background(), []core.Project{prepared(t, project())})
	if err != nil || !ok {
		t.Fatalf("ProjectOfFocused: ok=%v err=%v", ok, err)
	}
	if p.Name != "revier" {
		t.Errorf("project = %q, want revier: home's rule matches the session window", p.Name)
	}
}

// With no host that can report focus there is nothing to ask, and no error.
func TestProjectOfFocusedWithNoHostIsQuiet(t *testing.T) {
	c := &core.Core{}
	if _, ok, err := c.ProjectOfFocused(context.Background(), nil); ok || err != nil {
		t.Errorf("ok=%v err=%v, want a quiet miss", ok, err)
	}
}

// A launched workspace lands where the project says. The shell tool places
// every new session window at the right of the screen; a compositor rule
// cannot express "the window this launch just made", which is why revier
// places what revier starts.
func TestALaunchedWindowIsPlacedWhereTheProjectSays(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = "right top 75% 100%" // the editor target
	wm := hosttest.New("wm")
	c := &core.Core{Window: wm}

	res, err := c.Go(context.Background(), prepared(t, raw), "editor", nil)
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref.IsZero() {
		t.Fatal("the fake window host names what it opens; expected a ref")
	}
	got := wm.Placements[res.Ref.ID]
	want := []string{"right", "top", "75%", "100%"}
	if !slices.Equal(got, want) {
		t.Errorf("placement = %v, want %v", got, want)
	}
}

// Raising is not placing. A window the user has already moved stays where
// they put it.
func TestARaiseDoesNotPlace(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = "right top 75% 100%"
	wm := hosttest.New("wm")
	wm.Add("revier - README.md", "code") // already open, so Go raises it
	c := &core.Core{Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, raw), "editor", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(wm.Placements) != 0 {
		t.Errorf("placements = %v, want none: the window was already open", wm.Placements)
	}
}

// A target that declares no placement is never placed, which is every target
// that has not asked for one.
func TestNoPlacementDeclaredIsNoPlacement(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, project()), "editor", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(wm.Placements) != 0 {
		t.Errorf("placements = %v, want none", wm.Placements)
	}
}

// noPlacer is a window host without the placement capability. The method
// shadows the one promoted from the fake with a signature that does not
// satisfy revier.WindowPlacer, which is how a host that cannot place windows
// is expressed in a test.
type noPlacer struct{ *hosttest.Fake }

func (noPlacer) Place() {}

// A window host that cannot place windows ignores the declaration.
func TestAHostThatCannotPlaceIgnoresThePlacement(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Place = "right top 75% 100%"
	fake := hosttest.New("wm")
	c := &core.Core{Window: noPlacer{fake}}

	if _, err := c.Go(context.Background(), prepared(t, raw), "editor", nil); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(fake.Placements) != 0 {
		t.Errorf("placements = %v, want none from a host without the capability", fake.Placements)
	}
	if len(fake.Opened) != 1 {
		t.Errorf("opened %d, want 1: the launch must still happen", len(fake.Opened))
	}
}

// A window manager reuses window ids, so the id a target was bound to can
// come back as an unrelated window. Nothing about that failure is visible -
// the wrong window simply comes forward - so the binding is re-checked
// against the class the target declares before a keypress is sent to it.
func TestABindingToAReusedIdIsNotTrusted(t *testing.T) {
	wm := hosttest.New("wm")
	// The id the editor was bound to now belongs to something else.
	stale := wm.AddInstance(revier.Instance{Title: "Inbox", Class: "thunderbird"})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, project()), "editor", core.Bindings{"editor": stale})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref == stale {
		t.Fatal("the keypress went to a window of another application")
	}
	if len(wm.Opened) != 1 {
		t.Errorf("opened %d, want 1: with no editor to find, the rule launches one", len(wm.Opened))
	}
}

// A target whose rule constrains no class has nothing to re-check, and its
// binding is trusted as it was.
func TestABindingIsTrustedWhenTheRuleNamesNoClass(t *testing.T) {
	raw := project()
	raw.Targets[1].Window.Match = revier.Match{Title: "^revier"} // editor, title only
	wm := hosttest.New("wm")
	anything := wm.AddInstance(revier.Instance{Title: "moved on", Class: "whatever"})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	res, err := c.Go(context.Background(), prepared(t, raw), "editor", core.Bindings{"editor": anything})
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if res.Ref != anything || len(wm.Opened) != 0 {
		t.Errorf("result = %+v, opened %d; want the binding trusted", res, len(wm.Opened))
	}
}
