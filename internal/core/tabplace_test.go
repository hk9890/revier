// Layer L2: where `revier agent new` and `revier shell new` open their tab
// is a decision the core makes from the request and what the hosts report.
package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// placeProject is a project in a directory of its own whose home workspace,
// the runtime instance titled with its name, lists the tab agent: an agent
// panel beside a shell.
func placeProject(t *testing.T, name revier.ProjectName) core.Project {
	t.Helper()
	return prepared(t, revier.Project{Name: name, Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: string(name), Match: revier.Match{Title: "^" + string(name) + "$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
}

// mkdir is a new directory at path.
func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// tabDirs is the directory of each tab the runtime opened, in order.
func tabDirs(rt *hosttest.FakeRuntime) []string {
	var dirs []string
	for _, tab := range rt.Tabs {
		dirs = append(dirs, tab.Real.Dir)
	}
	return dirs
}

// placeTab runs PlaceTab and fails the test on an error.
func placeTab(t *testing.T, c *core.Core, projects []core.Project, req core.TabRequest) core.PlacedTab {
	t.Helper()
	placed, err := c.PlaceTab(t.Context(), projects, req)
	if err != nil {
		t.Fatalf("PlaceTab(%+v): %v", req, err)
	}
	return placed
}

// focusedOther is two open workspaces, demo's and other's. other's is
// focused and other is the project revier acted on last, so the focused
// window and the last project both answer other. It returns demo's home.
func focusedOther(t *testing.T) (*core.Core, *hosttest.FakeRuntime, []core.Project, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("tmux")
	home := rt.Add("demo", "")
	rt.SetFocus(rt.Add("other", ""))
	c := &core.Core{Runtime: rt, Ledger: &ledger{st: state.State{Current: "other"}}}
	return c, rt, []core.Project{placeProject(t, "demo"), placeProject(t, "other")}, home
}

// Without a project or a panel, an agent or a shell asked for from a
// directory in no project, by Dir or as the working directory, fails with
// ErrNoProject and opens no tab: neither the focused window nor the last
// project stands in for it. A Dir in no project fails even where the working
// directory is in one.
func TestPlaceTabFromADirectoryInNoProjectFails(t *testing.T) {
	c, rt, projects, _ := focusedOther(t)
	demo, nowhere := projects[0], t.TempDir()
	kinds := map[string]core.TabRequest{
		"agent new":            {Kind: revier.PanelAgent},
		"agent new --no-focus": {Kind: revier.PanelAgent, NoFocus: true},
		"shell new":            {Kind: revier.PanelShell},
	}
	for _, tc := range []struct{ cwd, dir, named string }{
		{cwd: nowhere, named: nowhere},
		{cwd: nowhere, dir: filepath.Join(nowhere, "x"), named: filepath.Join(nowhere, "x")},
		{cwd: demo.Path, dir: nowhere, named: nowhere},
	} {
		for what, req := range kinds {
			req.Cwd, req.Dir = tc.cwd, tc.dir
			_, err := c.PlaceTab(t.Context(), projects, req)
			if !errors.Is(err, core.ErrNoProject) {
				t.Errorf("%s --dir %q in %s: err = %v, want core.ErrNoProject", what, tc.dir, tc.cwd, err)
				continue
			}
			if msg := err.Error(); !strings.Contains(msg, tc.named) || !strings.Contains(msg, "-p <project>") {
				t.Errorf("%s --dir %q in %s: err = %q, want it to name %s and -p", what, tc.dir, tc.cwd, msg, tc.named)
			}
		}
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none", rt.Tabs)
	}
}

// Without a project or a panel, a tab opens in the project Dir is in,
// wherever the command runs, else in the project of the working directory:
// not in the focused window's.
func TestPlaceTabOpensInTheProjectOfItsDirectory(t *testing.T) {
	c, rt, projects, home := focusedOther(t)
	demo := projects[0]

	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Dir: demo.Path, Cwd: t.TempDir()})
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Cwd: demo.Path})
	var refs []revier.TargetRef
	for _, tab := range rt.Tabs {
		refs = append(refs, tab.Ref)
	}
	if want := []revier.TargetRef{home, home}; !slices.Equal(refs, want) {
		t.Errorf("tabs in %v, want %v", refs, want)
	}
}

// A tab opens in the target named after -p's colon, else in the kind's: an
// agent tab in the one that declares an agent panel, a shell tab in the home
// target.
func TestPlaceTabTakesTheTargetAfterTheColonElseTheKindsTarget(t *testing.T) {
	demo := prepared(t, revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "demo", Match: revier.Match{Title: "^demo$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
		{Name: "side", Runtime: &revier.Realization{Name: "side", Match: revier.Match{Title: "^side$"}}},
	}})
	rt := hosttest.NewRuntime("tmux")
	home, side := rt.Add("demo", ""), rt.Add("side", "")
	c := &core.Core{Runtime: rt}
	projects := []core.Project{demo}

	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelAgent, Project: "demo"})
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Project: "demo"})
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Project: "demo", Target: "side"})
	var refs []revier.TargetRef
	for _, tab := range rt.Tabs {
		refs = append(refs, tab.Ref)
	}
	if want := []revier.TargetRef{home, home, side}; !slices.Equal(refs, want) {
		t.Errorf("tabs in %v, want %v", refs, want)
	}
}

// A shell tab of a project with no home target is refused with the
// project's own reason where it has one, else for the missing home
// (decisions.md D85).
func TestPlaceTabShellRefusesAProjectWithNoHome(t *testing.T) {
	homeless := prepared(t, revier.Project{Name: "homeless", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "side", Runtime: &revier.Realization{Name: "side", Match: revier.Match{Title: "^side$"}}},
	}})
	rt := hosttest.NewRuntime("tmux")
	rt.Add("side", "")
	c := &core.Core{Runtime: rt}
	req := core.TabRequest{Kind: revier.PanelShell, Project: "homeless"}

	if _, err := c.PlaceTab(t.Context(), []core.Project{homeless}, req); err == nil || !strings.Contains(err.Error(), "has no home target") {
		t.Errorf("no reason: err = %v, want the missing home named", err)
	}
	invalid := errors.New("two targets are home")
	homeless.Invalid = invalid
	if _, err := c.PlaceTab(t.Context(), []core.Project{homeless}, req); !errors.Is(err, invalid) {
		t.Errorf("invalid: err = %v, want the project's reason", err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none", rt.Tabs)
	}
}

// An agent or a shell asked for a remote project opens here, as a tab of the
// link's workspace that reaches the host itself: by -p, or by the panel a key
// was pressed in. A Dir is a path on this machine and is dropped, not
// checked. The host is asked nothing.
func TestPlaceTabOfARemoteProjectOpensHereAndDropsDir(t *testing.T) {
	far := prepared(t, revier.Project{Name: "far", Path: "~/dev/far", Remote: &revier.Link{Host: "buildbox", Project: "far"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "far", Match: revier.Match{Title: "^far$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
	remote := hosttest.NewRemote("buildbox")
	rt := hosttest.NewRuntime("tmux")
	pane := rt.Add("far", "", revier.Panel{ID: "%7", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}
	projects := []core.Project{placeProject(t, "demo"), far}

	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelAgent, Project: "far", Resume: core.Resume{Session: "abc-123"}})
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Panel: "%7", Dir: "/srv/only-on-the-host"})
	if len(rt.Tabs) != 2 || rt.Tabs[0].Ref != pane || rt.Tabs[1].Ref != pane {
		t.Fatalf("tabs = %+v, want two in %v", rt.Tabs, pane)
	}
	agent, shell := rt.Tabs[0].Real.Panels[0], rt.Tabs[1].Real.Panels[0]
	if want := append(remote.PanelCommand("far", revier.PanelAgent, ""), "--resume", "abc-123"); agent.Kind != revier.PanelAgent || !slices.Equal(agent.Command, want) {
		t.Errorf("agent tab = %+v, want the agent panel running %q", agent, want)
	}
	if want := remote.PanelCommand("far", revier.PanelShell, ""); shell.Kind != revier.PanelShell || shell.Dir != "" || !slices.Equal(shell.Command, want) {
		t.Errorf("shell tab = %+v, want the shell panel running %q with no directory here", shell, want)
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %v, want nothing", remote.Asked)
	}
}

// A Dir outside the project of the panel opens the tab where no Dir would;
// one inside it is kept.
func TestPlaceTabAtAPanelDropsADirOutsideItsProject(t *testing.T) {
	demo := placeProject(t, "demo")
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "", revier.Panel{ID: "3", Kind: revier.PanelTool})
	c := &core.Core{Runtime: rt}

	for _, dir := range []string{"", "/", demo.Path} {
		placeTab(t, c, []core.Project{demo}, core.TabRequest{Kind: revier.PanelShell, Panel: "3", Dir: dir})
	}
	dirs := tabDirs(rt)
	if want := []string{dirs[0], dirs[0], demo.Path}; !slices.Equal(dirs, want) {
		t.Errorf("tab dirs = %q, want %q", dirs, want)
	}
}

// A shell tab at a panel starts in the Dir given, else in the panel's own
// directory, each while it is a directory in the panel's project: a
// worktree removed under the panel's shell, given by a key or reported by
// the runtime, or a panel directory outside the project, opens the tab where
// the project's own tab would. Outside a workspace the plain tab starts in
// the panel's directory too (decisions.md D153).
func TestPlaceTabAtAPanelShellStartsInThePanelsDirectory(t *testing.T) {
	demo := placeProject(t, "demo")
	here, given := mkdir(t, filepath.Join(demo.Path, "wt")), mkdir(t, filepath.Join(demo.Path, "given"))
	gone, elsewhere := filepath.Join(demo.Path, "removed"), t.TempDir()
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "", revier.Panel{ID: "1", Dir: here}, revier.Panel{ID: "2", Dir: gone}, revier.Panel{ID: "5", Dir: elsewhere})
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9", Dir: elsewhere})
	c := &core.Core{Runtime: rt}
	projects := []core.Project{demo}

	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Project: "demo"})
	project := rt.Tabs[0].Real.Dir
	for _, tc := range []struct {
		panel     revier.PanelID
		dir, want string
	}{
		{"1", "", here},
		{"1", given, given},
		{"2", "", project},
		{"1", gone, here},
		{"2", gone, project},
		{"5", "", project},
	} {
		placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Panel: tc.panel, Dir: tc.dir})
		if got := rt.Tabs[len(rt.Tabs)-1].Real.Dir; got != tc.want {
			t.Errorf("panel %s, dir %q: tab in %q, want %q", tc.panel, tc.dir, got, tc.want)
		}
	}

	rt.Tabs = nil
	if placed := placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Panel: "9", Outside: core.OutsideKey{Outside: core.OutsideTab}}); !placed.Outside {
		t.Errorf("outside: PlacedTab = %+v, want it outside", placed)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Real.Dir != elsewhere || rt.Tabs[0].Real.Panels[0].Dir != elsewhere {
		t.Errorf("tabs = %+v, want one plain tab in %s", rt.Tabs, elsewhere)
	}
}

// An agent tab at a panel starts in the Dir given, else in the project,
// never in the panel's own directory: an agent pressed from a worktree starts
// where it can enter any worktree (decisions.md D158). Outside a workspace
// the key opens a plain shell tab, which starts in the panel's directory.
func TestPlaceTabAtAPanelAgentStartsInTheProject(t *testing.T) {
	demo := placeProject(t, "demo")
	worktree, given := mkdir(t, filepath.Join(demo.Path, "wt")), mkdir(t, filepath.Join(demo.Path, "given"))
	gone, elsewhere := filepath.Join(demo.Path, "removed"), t.TempDir()
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "", revier.Panel{ID: "3", Dir: worktree})
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9", Dir: elsewhere})
	c := &core.Core{Runtime: rt}
	projects := []core.Project{demo}

	for _, tc := range []struct{ dir, want string }{
		{"", demo.Path},
		{given, given},
		{gone, demo.Path},
	} {
		placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelAgent, Panel: "3", Dir: tc.dir})
		if got := rt.Tabs[len(rt.Tabs)-1].Real.Dir; got != tc.want {
			t.Errorf("dir %q: tab in %q, want %q", tc.dir, got, tc.want)
		}
	}
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Panel: "3"})
	if got := rt.Tabs[len(rt.Tabs)-1].Real.Dir; got != worktree {
		t.Errorf("shell beside it: tab in %q, want %q", got, worktree)
	}

	rt.Tabs = nil
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelAgent, Panel: "9", Outside: core.OutsideKey{Outside: core.OutsideTab}})
	if len(rt.Tabs) != 1 || rt.Tabs[0].Real.Dir != elsewhere || rt.Tabs[0].Real.Panels[0].Kind != revier.PanelShell {
		t.Errorf("tabs = %+v, want one plain tab in %s", rt.Tabs, elsewhere)
	}
}

// With -p a Dir that is not a directory is refused, and no tab opens. At a
// panel the same Dir falls back to the panel's directory, since a key passes
// the one its terminal names, which can be removed since.
func TestPlaceTabRefusesADirThatIsNotADirectory(t *testing.T) {
	demo := placeProject(t, "demo")
	worktree, gone := mkdir(t, filepath.Join(demo.Path, "wt")), filepath.Join(demo.Path, "removed")
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "", revier.Panel{ID: "3", Dir: worktree})
	c := &core.Core{Runtime: rt}
	projects := []core.Project{demo}

	_, err := c.PlaceTab(t.Context(), projects, core.TabRequest{Kind: revier.PanelShell, Project: "demo", Dir: gone})
	if want := "--dir " + gone + ": not a directory"; err == nil || err.Error() != want || len(rt.Tabs) != 0 {
		t.Fatalf("-p demo --dir %s: err = %v, tabs %+v, want %q and no tab", gone, err, rt.Tabs, want)
	}
	placeTab(t, c, projects, core.TabRequest{Kind: revier.PanelShell, Panel: "3", Dir: gone})
	if dirs := tabDirs(rt); !slices.Equal(dirs, []string{worktree}) {
		t.Errorf("--panel 3 --dir %s: tabs in %q, want one in %s", gone, dirs, worktree)
	}
}

// Without the focus the tab opens and the focus stays where it is, and the
// placed tab carries the address the new agent answers to, which is all a
// script has to find it by (decisions.md D118, D155).
func TestPlaceTabNoFocusGivesTheAddress(t *testing.T) {
	demo := placeProject(t, "demo")
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "")
	c := &core.Core{Runtime: rt}

	placed := placeTab(t, c, []core.Project{demo}, core.TabRequest{Kind: revier.PanelAgent, Project: "demo", NoFocus: true})
	if len(rt.Tabs) != 1 {
		t.Fatalf("tabs = %+v, want one", rt.Tabs)
	}
	if want := core.Address("demo", "home", rt.Tabs[0].Panel); placed.Address != want {
		t.Errorf("address = %q, want %q", placed.Address, want)
	}
	if len(rt.PanelFocuses) != 0 {
		t.Errorf("panel focuses = %v, want none", rt.PanelFocuses)
	}
	if placed = placeTab(t, c, []core.Project{demo}, core.TabRequest{Kind: revier.PanelAgent, Project: "demo"}); placed.Address != "" {
		t.Errorf("with the focus: address = %q, want none", placed.Address)
	}
}

// The placed tab says what its agent came to: a conversation no probe here
// can resume starts empty, and one a probe can is resumed.
func TestPlaceTabReportsTheAgentOutcome(t *testing.T) {
	demo := placeProject(t, "demo")
	rt := hosttest.NewRuntime("kitty")
	rt.Add("demo", "")
	req := core.TabRequest{Kind: revier.PanelAgent, Project: "demo", Resume: core.Resume{Session: "abc-123"}}

	if placed := placeTab(t, &core.Core{Runtime: rt}, []core.Project{demo}, req); placed.Outcome != core.AgentUnresumable {
		t.Errorf("no probe: outcome = %s, want %s", placed.Outcome, core.AgentUnresumable)
	}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	if placed := placeTab(t, c, []core.Project{demo}, req); placed.Outcome != core.AgentResumed {
		t.Errorf("resumable: outcome = %s, want %s", placed.Outcome, core.AgentResumed)
	}
}
