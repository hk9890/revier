package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

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

	ref, err := c.Go(context.Background(), project(), "editor")
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

func TestGoOpensWhenNothingMatches(t *testing.T) {
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}

	if _, err := c.Go(context.Background(), project(), "editor"); err != nil {
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

	got, err := c.Go(context.Background(), project(), "editor")
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

	if _, err := c.Go(context.Background(), project(), "home"); err != nil {
		t.Fatalf("Go: %v", err)
	}
	if len(rt.Focuses) != 1 {
		t.Errorf("focuses = %v, want one: home must not toggle away from itself", rt.Focuses)
	}
}

func TestGoRejectsUnknownTarget(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	if _, err := c.Go(context.Background(), project(), "absent"); !errors.Is(err, core.ErrNoTarget) {
		t.Errorf("err = %v, want ErrNoTarget", err)
	}
}

// A match that constrains nothing would select whichever instance the host
// happened to list first, so it is refused rather than acted on.
func TestGoRejectsUnboundedMatch(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("anything", "")
	c := &core.Core{Runtime: rt}
	p := revier.Project{Targets: []revier.Target{
		{Name: "loose", Runtime: &revier.Realization{Launch: []string{"x"}}},
	}}

	if _, err := c.Go(context.Background(), p, "loose"); !errors.Is(err, core.ErrUnboundedMatch) {
		t.Errorf("err = %v, want ErrUnboundedMatch", err)
	}
}

func TestSurveyReportsRunningAndAvailability(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	c := &core.Core{Runtime: rt} // no window host

	views, err := c.Survey(context.Background(), []revier.Project{project()})
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

	views, err := c.Survey(context.Background(), []revier.Project{project()})
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

	views, err := c.Survey(context.Background(), []revier.Project{project()})
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

	got, err := c.Go(context.Background(), p, "diff")
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

	got, err := c.Go(context.Background(), p, "diff")
	if err != nil {
		t.Fatalf("Go: %v", err)
	}
	if got != homeRef {
		t.Errorf("returned %v, want home %v", got, homeRef)
	}
}
