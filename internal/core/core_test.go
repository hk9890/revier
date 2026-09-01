package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// prepared is the form Go and Survey take: rendered and compiled once, as
// config.Load does for a real project file.
func prepared(t *testing.T, p revier.Project) core.Project {
	t.Helper()
	out, err := core.PrepareProject(p)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
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

	ref, err := c.Go(context.Background(), prepared(t, project()), "editor")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
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

	ref, err := c.Go(context.Background(), prepared(t, project()), "editor")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if !ref.IsZero() {
		t.Errorf("ref = %+v, want zero: the host could not name the window", ref)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("focuses = %v, want none: there is no ref to focus", wm.Focuses)
	}
	// The window exists now, so the next press raises it.
	again, err := c.Go(context.Background(), prepared(t, project()), "editor")
	if err != nil {
		t.Fatalf("second Go: %v", err)
	}
	if again.IsZero() || len(wm.Opened) != 1 {
		t.Errorf("second press: ref %+v, opened %d; want the existing window raised", again, len(wm.Opened))
	}
}

func TestGoOpensWhenNothingMatches(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	if _, err := c.Go(context.Background(), prepared(t, project()), "editor"); err != nil {
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

	got, err := c.Go(context.Background(), prepared(t, project()), "editor")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got != homeRef {
		t.Errorf("returned %v, want the home ref %v", got, homeRef)
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

	if _, err := c.Go(context.Background(), prepared(t, project()), "home"); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Focuses) != 1 {
		t.Errorf("focuses = %v, want one: home must not toggle away from itself", rt.Focuses)
	}
}

func TestGoRejectsUnknownTarget(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	if _, err := c.Go(context.Background(), prepared(t, project()), "absent"); !errors.Is(err, core.ErrNoTarget) {
		t.Errorf("err = %v, want ErrNoTarget", err)
	}
}

// A match that constrains nothing would select whichever instance the host
// happened to list first, so it is refused at load rather than acted on.
func TestPrepareRejectsUnboundedMatch(t *testing.T) {
	p := revier.Project{Name: "loose", Targets: []revier.Target{
		{Name: "loose", Runtime: &revier.Realization{Launch: []string{"x"}}},
	}}

	_, err := core.PrepareProject(p)
	if !errors.Is(err, core.ErrUnboundedMatch) {
		t.Fatalf("err = %v, want ErrUnboundedMatch", err)
	}
	if !strings.Contains(err.Error(), `"loose"`) || !strings.Contains(err.Error(), "runtime") {
		t.Errorf("error should name the target and the realization: %v", err)
	}
}

// A pattern that does not parse and a template that does not render are load
// failures naming the project, not keystroke failures and not a silently
// unavailable project on the dashboard.
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
			_, err := core.Prepare([]revier.Project{p})
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range []string{tc.want, `"broken"`, `"home"`} {
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

	views, err := c.Survey(context.Background(), []core.Project{prepared(t, project())})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
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

	views, err := c.Survey(context.Background(), []core.Project{prepared(t, project())})
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if len(views[0].Agents) != 1 {
		t.Fatalf("agents = %d, want 1: only agent panels are probed", len(views[0].Agents))
	}
	if !views[0].Attention() {
		t.Error("Attention() should report true; it is what the TUI sorts on")
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

	views, err := c.Survey(context.Background(), []core.Project{prepared(t, project())})
	if err != nil {
		t.Fatalf("Survey should not fail: %v", err)
	}
	if got := views[0].Agents[0].State.Status; got != revier.StatusUnknown {
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

	got, err := c.Go(context.Background(), prepared(t, p), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Host != "rt" {
		t.Fatalf("landed on %s, want the runtime target", got.Host)
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

	got, err := c.Go(context.Background(), prepared(t, p), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got != homeRef {
		t.Errorf("returned %v, want home %v", got, homeRef)
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

	got, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Host != "kitty" {
		t.Fatalf("landed on %s, want the runtime target", got.Host)
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

	got, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Title != "session:revier" {
		t.Errorf("returned %v, want home: the diff OS window had focus", got)
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

	got, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Title != "diff:revier" {
		t.Errorf("returned %v, want diff: nothing of this project had focus", got)
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

	got, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Title != "diff:revier" {
		t.Errorf("returned %v, want diff: a multiplexer's windows are not OS windows", got)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none", wm.Focuses)
	}
}

// Same title, different process: not the same window. The pid filter is what
// keeps a stray window with a matching title from being raised.
func TestBridgeRejectsAPIDMismatch(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	wm := hosttest.New("wm")
	rt.Add("diff:revier", "kitty") // pid 1001
	rt.Add("session:revier", "kitty")
	other := wm.AddInstance(revier.Instance{Title: "diff:revier", Class: "kitty", PID: 9999})
	wm.SetFocus(other)
	c := &core.Core{Runtime: rt, Window: wm}

	got, err := c.Go(context.Background(), prepared(t, osWindowProject()), "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got.Title != "diff:revier" {
		t.Errorf("returned %v, want diff: the focused window belongs to another process", got)
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none", wm.Focuses)
	}
}
