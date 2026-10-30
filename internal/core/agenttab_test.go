// Layer L2: opening an agent tab in an open workspace, and finding the
// workspace from a panel, are decisions the core makes from what a host
// reports.
package core_test

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// The key's tab: the project's agent panel on the conversation asked for, and
// the project's shell, both in the directory asked for, opened in the open
// workspace, with the new agent made current.
func TestNewAgentOpensAnAgentTabInTheOpenWorkspace(t *testing.T) {
	c, rt, p, ref := openWorkspace(t)
	dir := t.TempDir()

	outcome, err := newAgent(t, c, p, "home", core.Resume{Session: "abc-123", Dir: dir})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentResumed {
		t.Errorf("outcome = %v, want resumed", outcome)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref || rt.Tabs[0].Vars != nil {
		t.Fatalf("Tabs = %+v, want one tab without a target var in %+v", rt.Tabs, ref)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus", "--resume", "abc-123"}, Dir: dir},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: dir},
	})
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{rt.Tabs[0].Panel}) {
		t.Errorf("panel focuses = %v, want the new agent %s", rt.PanelFocuses, rt.Tabs[0].Panel)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("Opened = %+v, want nothing opened: the tab goes into the open workspace", rt.Opened)
	}
}

// A named agent's tab is the layout's agent panel running that agent, titled
// with its name, beside the layout's shell; every panel carries the agent's
// mark, and the event names it.
func TestNewAgentWithANameOpensThatAgentsTab(t *testing.T) {
	recorded := recording(t)
	c, rt, p, _ := openWorkspace(t)
	c.Ledger = &ledger{}
	p.Agents = []core.NamedAgent{{Name: "review", Command: []string{"claude", "--agent", "review"}}, {Name: "dtop", Command: []string{"dtop"}}}

	outcome, err := newAgent(t, c, p, "home", core.Resume{Agent: "dtop"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentEmpty {
		t.Errorf("outcome = %v, want empty", outcome)
	}
	if len(rt.Tabs) != 1 || !maps.Equal(rt.Tabs[0].Vars, map[string]string{core.PanelAgentVar: "dtop"}) {
		t.Fatalf("Tabs = %+v, want one tab marked revier_agent=dtop", rt.Tabs)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "dtop", Command: []string{"dtop"}, Dir: "/home/user/dev/github/revier"},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: "/home/user/dev/github/revier"},
	})
	want := []revier.Event{{Kind: revier.EventAgentNew, Project: "revier", Target: "home", Agent: "dtop"}}
	if got := recorded(); !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// A named agent a resumable probe claims resumes through that probe, as the
// default agent does.
func TestNewAgentResumesANamedAgentItsProbeClaims(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)
	p.Agents = []core.NamedAgent{{Name: "review", Command: []string{"claude", "--agent", "review"}}}

	outcome, err := newAgent(t, c, p, "home", core.Resume{Agent: "review", Session: "abc-123"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentResumed {
		t.Errorf("outcome = %v, want resumed", outcome)
	}
	if got := rt.Tabs[0].Real.Panels[0].Command; !slices.Equal(got, []string{"claude", "--agent", "review", "--resume", "abc-123"}) {
		t.Errorf("agent command = %q, want the named agent's on the conversation", got)
	}
}

// A name the project does not declare opens nothing, and the refusal names the
// agents it does declare.
func TestNewAgentRefusesAnUnknownName(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)
	p.Agents = []core.NamedAgent{{Name: "review", Command: []string{"review"}}, {Name: "dtop", Command: []string{"dtop"}}}

	outcome, err := newAgent(t, c, p, "home", core.Resume{Agent: "nope"})
	if !errors.Is(err, core.ErrUnknownAgent) || !strings.Contains(err.Error(), "review, dtop") || outcome != core.AgentNotAdded {
		t.Errorf("NewAgent = %v, %v; want ErrUnknownAgent naming review, dtop, and not added", outcome, err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("Tabs = %+v, want nothing opened", rt.Tabs)
	}
}

// With no conversation and no directory, the agent starts empty where the
// workspace starts.
func TestNewAgentStartsEmptyInTheProject(t *testing.T) {
	c, rt, p, _ := openWorkspace(t)

	outcome, err := newAgent(t, c, p, "home", core.Resume{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentEmpty {
		t.Errorf("outcome = %v, want empty", outcome)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus"}, Dir: "/home/user/dev/github/revier"},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: "/home/user/dev/github/revier"},
	})
}

// --resume names a conversation and not its harness. It goes to the harness
// the agent panel runs: a panel no resumable probe claims starts empty, not on
// another harness's resume flag.
func TestNewAgentResumesOnlyTheHarnessTheAgentPanelRuns(t *testing.T) {
	proj := agentProject()
	proj.Targets[3].Runtime.Panels[1] = revier.PanelSpec{Kind: revier.PanelAgent, Title: "opencode", Command: []string{"opencode"}}
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable(), &hosttest.FakeProbe{Harness: "opencode", Marker: "opencode"}}}
	rt.Add("session:revier", "")

	outcome, err := newAgent(t, c, prepared(t, proj), "home", core.Resume{Session: "abc-123"})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if outcome != core.AgentUnresumable {
		t.Errorf("outcome = %v, want unresumable", outcome)
	}
	if got := rt.Tabs[0].Real.Panels[0].Command; !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("agent command = %q, want opencode without a resume flag", got)
	}
}

// A workspace that is not open has no instance to open a tab in, and a target
// with no agent panel has no tab to give. Neither opens anything.
func TestNewAgentRefusesWhatItCannotOpenIn(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())

	if _, err := c.AgentWorkspace(context.Background(), p, "home"); !errors.Is(err, core.ErrNotOpen) {
		t.Errorf("closed workspace: err = %v, want ErrNotOpen", err)
	}
	rt.Add("notes:revier", "")
	outcome, err := newAgent(t, c, p, "notes", core.Resume{})
	if !errors.Is(err, core.ErrNoAgent) || outcome != core.AgentNotAdded {
		t.Errorf("target with no agent panel: %v, %v, want ErrNoAgent and not added", outcome, err)
	}
	if len(rt.Tabs) != 0 || len(rt.Opened) != 0 {
		t.Errorf("Tabs = %+v, Opened = %+v, want nothing", rt.Tabs, rt.Opened)
	}
}

// A runtime without tabs is named, not worked around by opening a second
// workspace.
func TestNewAgentNeedsARuntimeWithTabs(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: bareRuntime{rt}, Probes: []revier.AgentProbe{resumable()}}
	rt.Add("session:revier", "")

	_, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{})
	if !errors.Is(err, core.ErrNoTabsToOpen) || !strings.Contains(err.Error(), "rt") {
		t.Errorf("err = %v, want ErrNoTabsToOpen naming the runtime", err)
	}
}

// The shell key's tab: the target's declared shell alone, in the directory
// asked for, made current in the open workspace.
func TestNewShellOpensTheDeclaredShellInTheOpenWorkspace(t *testing.T) {
	c, rt, p, ref := openWorkspace(t)
	w, err := c.AgentWorkspace(context.Background(), p, "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	dir := t.TempDir()

	if err := c.NewShell(context.Background(), w, dir); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref {
		t.Fatalf("Tabs = %+v, want one tab in %+v", rt.Tabs, ref)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: dir}})
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{rt.Tabs[0].Panel}) {
		t.Errorf("panel focuses = %v, want the new shell %s", rt.PanelFocuses, rt.Tabs[0].Panel)
	}
}

// A target that declares no shell still gets one: the runtime's own, in the
// target's directory.
func TestNewShellFallsBackToTheRuntimesShell(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	rt.Add("notes:revier", "")
	p := prepared(t, agentProject())
	w, err := c.AgentWorkspace(context.Background(), p, "notes")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}

	if err := c.NewShell(context.Background(), w, ""); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	if len(rt.Tabs) != 1 {
		t.Fatalf("Tabs = %+v, want one tab", rt.Tabs)
	}
	samePanels(t, "tab", rt.Tabs[0].Real.Panels, []revier.PanelSpec{{Kind: revier.PanelShell, Dir: "/home/user/dev/github/revier"}})
}

func TestNewShellNeedsARuntimeWithTabs(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: bareRuntime{rt}}
	rt.Add("session:revier", "")
	w, err := c.AgentWorkspace(context.Background(), prepared(t, agentProject()), "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	if err := c.NewShell(context.Background(), w, ""); !errors.Is(err, core.ErrNoTabsToOpen) {
		t.Errorf("err = %v, want ErrNoTabsToOpen", err)
	}
}

// An agent asked for a link opens here, as a tab of the link's workspace: the
// ssh that runs the agent on the host, with the conversation to resume as its
// arguments, since only the host can resume it.
func TestAnAgentTabOfALinkIsTheSSHPanelWithTheResume(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("far", "")
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": hosttest.NewRemote("buildbox")}}
	w, err := c.AgentWorkspace(context.Background(), linkProject(t), "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	outcome, err := c.NewAgent(context.Background(), w, core.Resume{Session: "abc-123", Dir: "/srv/far/wt", Harness: "claude"})
	if err != nil || outcome != core.AgentResumed {
		t.Fatalf("NewAgent = %v, %v; want resumed", outcome, err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != ref {
		t.Fatalf("tabs = %+v, want one in %v", rt.Tabs, ref)
	}
	got := rt.Tabs[0].Real.Panels[0]
	want := sshArgv(revier.PanelAgent, "--resume", "abc-123", "--dir", "/srv/far/wt")
	if !slices.Equal(got.Command, want) || got.Dir != "" {
		t.Errorf("agent panel = %+v, want %q and no directory here", got, want)
	}
	// The shell beside it starts on the host too, in the same directory.
	if shell := rt.Tabs[0].Real.Panels[1]; !slices.Equal(shell.Command, sshArgv(revier.PanelShell, "--dir", "/srv/far/wt")) {
		t.Errorf("shell panel = %q, want the directory carried to the host", shell.Command)
	}

	// A word a shell would read is not sent, and the agent starts empty: the
	// conversation without its directory would carry on in the wrong checkout.
	for _, r := range []core.Resume{{Session: "abc; rm -rf /"}, {Session: "abc-123", Dir: "/srv/far/my wt"}} {
		outcome, err = c.NewAgent(context.Background(), w, r)
		if err != nil || outcome != core.AgentUnresumable {
			t.Errorf("NewAgent(%+v) = %v, %v; want unresumable", r, outcome, err)
		}
		if got := rt.Tabs[len(rt.Tabs)-1].Real.Panels[0].Command; !slices.Equal(got, sshArgv(revier.PanelAgent)) {
			t.Errorf("agent panel = %q, want %q", got, sshArgv(revier.PanelAgent))
		}
	}
}

// A shell asked for a link opens here too, as the ssh that runs the shell on
// the host: the shell panel of the tab the link's home lists.
func TestAShellTabOfALinkIsTheSSHPanel(t *testing.T) {
	c, rt, _, pane := linked(t)
	w, err := c.AgentWorkspace(context.Background(), linkProject(t), "home")
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	if err := c.NewShell(context.Background(), w, ""); err != nil {
		t.Fatalf("NewShell: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != pane {
		t.Fatalf("tabs = %+v, want one in %v", rt.Tabs, pane)
	}
	if got := rt.Tabs[0].Real.Panels; len(got) != 1 || got[0].Kind != revier.PanelShell || !slices.Equal(got[0].Command, sshArgv(revier.PanelShell)) {
		t.Errorf("shell tab = %+v, want the one shell panel running %q", got, sshArgv(revier.PanelShell))
	}
}

// twoWorkspaces is agentProject and a second project, each with its home
// open, holding the given panels.
func twoWorkspaces(t *testing.T, revierPanels, otherPanels []revier.Panel) (*core.Core, *hosttest.FakeRuntime, []core.Project, revier.TargetRef, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	other := agentProject()
	other.Name, other.Path = "other", "/home/user/dev/other"
	other.Targets[0].Runtime.Name, other.Targets[0].Runtime.Match.Title = "session:other", "^session:other$"
	projects := []core.Project{prepared(t, agentProject()), prepared(t, other)}
	a := rt.Add("session:revier", "", revierPanels...)
	b := rt.Add("session:other", "", otherPanels...)
	return c, rt, projects, a, b
}

// The key knows only the window it was pressed in. The workspace holding that
// panel is found, with its project and target.
func TestPanelOwnerFindsTheWorkspaceHoldingAPanel(t *testing.T) {
	c, _, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}, {ID: "2"}}, []revier.Panel{{ID: "7"}})

	w, err := c.PanelOwner(context.Background(), projects, "7")
	if err != nil {
		t.Fatalf("PanelOwner: %v", err)
	}
	if w.Project.Name != "other" || w.Target != "home" || w.Ref != other {
		t.Errorf("owner = %s:%s %+v, want other:home %+v", w.Project.Name, w.Target, w.Ref, other)
	}
}

// One instance backs two targets, and only the second declares an agent
// panel. The key opens its tab from that one, not from the first target in
// the file, which has no tab to give.
func TestPanelOwnerTakesTheTargetThatDeclaresAnAgent(t *testing.T) {
	proj := agentProject()
	plain := *proj.Targets[1].Runtime
	plain.Match.Title = proj.Targets[0].Runtime.Match.Title
	proj.Targets[1].Runtime = &plain
	proj.Targets[0], proj.Targets[1] = proj.Targets[1], proj.Targets[0]
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	ref := rt.Add("session:revier", "", revier.Panel{ID: "5"})

	w, err := c.PanelOwner(context.Background(), []core.Project{prepared(t, proj)}, "5")
	if err != nil {
		t.Fatalf("PanelOwner: %v", err)
	}
	if w.Target != "home" || w.Ref != ref {
		t.Errorf("owner = %s %+v, want home %+v", w.Target, w.Ref, ref)
	}
	if _, err := c.NewAgent(context.Background(), w, core.Resume{}); err != nil {
		t.Errorf("NewAgent: %v, want the tab opened", err)
	}
}

// Two workspaces holding one id - one per kitty process - on a runtime that
// cannot say which the id means are refused rather than guessed at.
func TestPanelOwnerRefusesAnIDTwoWorkspacesHold(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	if _, err := c.PanelOwner(context.Background(), projects, "1"); !errors.Is(err, core.ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}

// finding is a runtime that says which instance a panel id means, as kitty
// does from the process the command was started in.
type finding struct {
	*hosttest.FakeRuntime
	ref revier.TargetRef
}

func (f finding) FindPanel([]revier.Instance, revier.PanelID) (revier.TargetRef, error) {
	return f.ref, nil
}

// A runtime that says which instance an id means decides between two
// workspaces holding it; one that finds nothing is named.
func TestPanelOwnerTakesTheInstanceTheRuntimeFinds(t *testing.T) {
	_, rt, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "1"}})

	c := &core.Core{Runtime: finding{rt, other}}
	if w, err := c.PanelOwner(context.Background(), projects, "1"); err != nil || w.Project.Name != "other" || w.Ref != other {
		t.Errorf("owner = %s %+v, %v, want other %+v", w.Project.Name, w.Ref, err, other)
	}
	c = &core.Core{Runtime: finding{rt, revier.TargetRef{}}}
	if _, err := c.PanelOwner(context.Background(), projects, "1"); !errors.Is(err, core.ErrNoPanel) {
		t.Errorf("nothing found: err = %v, want ErrNoPanel", err)
	}
	c = &core.Core{Runtime: finding{rt, rt.Add("a kitty window of no project", "")}}
	if _, err := c.PanelOwner(context.Background(), projects, "1"); !errors.Is(err, core.ErrNoWorkspace) {
		t.Errorf("found in no workspace: err = %v, want ErrNoWorkspace", err)
	}
}

// tabAtPanel runs TabAtPanel and reports the workspace it handed to in, or
// the zero one where in was not called.
func tabAtPanel(t *testing.T, c *core.Core, projects []core.Project, panel revier.PanelID, dir string, outside core.Outside) (core.Workspace, error) {
	t.Helper()
	var got core.Workspace
	err := c.TabAtPanel(context.Background(), projects, panel, revier.PanelShell, dir, core.OutsideKey{Outside: outside}, func(w core.Workspace, _ string) error {
		got = w
		return nil
	})
	return got, err
}

// A shell tab starts in the directory given, else in the panel's own
// directory, each while it is one: a worktree removed under the panel's
// shell, given by a key or reported by kitty, leaves the tab where in starts
// it. Outside a workspace the plain tab starts there too.
func TestTabAtPanelStartsInThePanelsDirectory(t *testing.T) {
	here, gone, given := t.TempDir(), filepath.Join(t.TempDir(), "removed"), t.TempDir()
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1", Dir: here}, {ID: "2", Dir: gone}}, nil)
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9", Dir: here})

	for _, tc := range []struct {
		panel     revier.PanelID
		dir, want string
	}{
		{"1", "", here},
		{"1", given, given},
		{"2", "", ""},
		{"1", gone, here},
		{"2", gone, ""},
	} {
		got := "unset"
		err := c.TabAtPanel(context.Background(), projects, tc.panel, revier.PanelShell, tc.dir, core.OutsideKey{Outside: core.OutsideTab}, func(_ core.Workspace, dir string) error {
			got = dir
			return nil
		})
		if err != nil || got != tc.want {
			t.Errorf("panel %s, dir %q: in got %q, %v, want %q", tc.panel, tc.dir, got, err, tc.want)
		}
	}

	if _, err := tabAtPanel(t, c, projects, "9", "", core.OutsideTab); err != nil {
		t.Fatalf("TabAtPanel outside: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Real.Dir != here || rt.Tabs[0].Real.Panels[0].Dir != here {
		t.Errorf("tabs = %+v, want one plain tab in %s", rt.Tabs, here)
	}
}

// An agent tab starts in the directory given, else where in starts it, never
// in the panel's own directory: an agent pressed from a worktree starts in
// the project, where it can enter any worktree. Outside a workspace the key
// opens a plain shell tab, which starts in the panel's directory.
func TestTabAtPanelStartsAnAgentOutsideThePanelsDirectory(t *testing.T) {
	worktree, gone, given := t.TempDir(), filepath.Join(t.TempDir(), "removed"), t.TempDir()
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1", Dir: worktree}}, nil)
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9", Dir: worktree})

	for _, tc := range []struct {
		dir, want string
	}{
		{"", ""},
		{given, given},
		{gone, ""},
	} {
		got := "unset"
		err := c.TabAtPanel(context.Background(), projects, "1", revier.PanelAgent, tc.dir, core.OutsideKey{Outside: core.OutsideTab}, func(_ core.Workspace, dir string) error {
			got = dir
			return nil
		})
		if err != nil || got != tc.want {
			t.Errorf("dir %q: in got %q, %v, want %q", tc.dir, got, err, tc.want)
		}
	}

	err := c.TabAtPanel(context.Background(), projects, "9", revier.PanelAgent, "", core.OutsideKey{Outside: core.OutsideTab}, func(core.Workspace, string) error {
		t.Fatal("in ran for a window of no project")
		return nil
	})
	if err != nil {
		t.Fatalf("TabAtPanel outside: %v", err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Real.Dir != worktree {
		t.Errorf("tabs = %+v, want one plain tab in %s", rt.Tabs, worktree)
	}
}

// A key pressed in a workspace opens its tab there, through in, and nothing
// beside it.
func TestTabAtPanelInAWorkspaceOpensThere(t *testing.T) {
	c, rt, projects, _, other := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, []revier.Panel{{ID: "7"}})

	w, err := tabAtPanel(t, c, projects, "7", "", core.OutsideTab)
	if err != nil || w.Project.Name != "other" || w.Ref != other {
		t.Errorf("TabAtPanel = %s %+v, %v, want other %+v", w.Project.Name, w.Ref, err, other)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none beside the workspace", rt.Tabs)
	}
}

// A key pressed in a terminal window that is no project's workspace is not
// refused: with outside "tab" it opens a plain tab of the runtime's own shell
// beside the window, in --dir, and goes to it. No project's in runs.
func TestTabAtPanelOutsideAWorkspaceOpensAPlainTab(t *testing.T) {
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)
	loose := rt.Add("a kitty window of no project", "", revier.Panel{ID: "9"})

	notes := t.TempDir()
	w, err := tabAtPanel(t, c, projects, "9", notes, core.OutsideTab)
	if err != nil {
		t.Fatalf("TabAtPanel: %v", err)
	}
	if !w.Ref.IsZero() {
		t.Errorf("in ran with %+v, want it not called", w.Ref)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != loose {
		t.Fatalf("tabs = %+v, want one in %v", rt.Tabs, loose)
	}
	tab := rt.Tabs[0]
	if len(tab.Real.Panels) != 1 || tab.Real.Panels[0].Kind != revier.PanelShell || len(tab.Real.Panels[0].Command) != 0 || tab.Real.Panels[0].Dir != notes {
		t.Errorf("tab = %+v, want one shell panel with no command in %s", tab.Real.Panels, notes)
	}
	if len(tab.Vars) != 0 {
		t.Errorf("vars = %v, want none: the tab is no project's", tab.Vars)
	}
	if !slices.Equal(rt.PanelFocuses, []revier.PanelID{tab.Panel}) {
		t.Errorf("focused %v, want the new tab's panel %s", rt.PanelFocuses, tab.Panel)
	}
}

// With outside "none" the key does nothing there, and that is no failure.
func TestTabAtPanelOutsideAWorkspaceWithNoneDoesNothing(t *testing.T) {
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)
	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9"})

	if w, err := tabAtPanel(t, c, projects, "9", "", core.OutsideNone); err != nil || !w.Ref.IsZero() {
		t.Errorf("TabAtPanel = %+v, %v, want nothing done and no error", w.Ref, err)
	}
	if len(rt.Tabs) != 0 || len(rt.PanelFocuses) != 0 {
		t.Errorf("tabs = %+v, focuses = %v, want none", rt.Tabs, rt.PanelFocuses)
	}
}

// outsideAgents are config.toml's named agents as a key outside a workspace
// reads them: the first is the default agent's.
var outsideAgents = []core.NamedAgent{
	{Name: "claude", Command: []string{"claude"}},
	{Name: "dtop", Command: []string{"dtop", "--fast"}},
}

// keyOutside presses a key of kind, asking for agent, in a window of no
// project with outside "agent", and returns the one tab it opened, in dir.
func keyOutside(t *testing.T, kind revier.PanelKind, agent string, agents []core.NamedAgent) (revier.PanelSpec, error) {
	t.Helper()
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)
	loose := rt.Add("a kitty window of no project", "", revier.Panel{ID: "9"})
	dir := t.TempDir()
	err := c.TabAtPanel(context.Background(), projects, "9", kind, dir, core.OutsideKey{Outside: core.OutsideAgent, Agent: agent, Agents: agents}, func(core.Workspace, string) error {
		t.Fatal("in ran for a window of no project")
		return nil
	})
	if err != nil {
		if len(rt.Tabs) != 0 {
			t.Errorf("tabs = %+v, want none beside a refusal", rt.Tabs)
		}
		return revier.PanelSpec{}, err
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != loose || len(rt.Tabs[0].Real.Panels) != 1 || len(rt.Tabs[0].Vars) != 0 {
		t.Fatalf("tabs = %+v, want one tab of one panel and no vars in %v", rt.Tabs, loose)
	}
	tab := rt.Tabs[0]
	if tab.Real.Dir != dir || tab.Real.Panels[0].Dir != dir || !slices.Equal(rt.PanelFocuses, []revier.PanelID{tab.Panel}) {
		t.Errorf("tab = %+v, focuses %v, want it in %s and focused", tab, rt.PanelFocuses, dir)
	}
	return tab.Real.Panels[0], nil
}

// With outside "agent" the agent key opens a tab that runs config.toml's
// first named agent, a named agent's key that agent, and the shell key the
// plain tab outside "tab" opens.
func TestTabAtPanelOutsideAWorkspaceWithAgentRunsTheAgent(t *testing.T) {
	for _, tc := range []struct {
		kind  revier.PanelKind
		agent string
		want  revier.PanelSpec
	}{
		{revier.PanelAgent, "", revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"claude"}}},
		{revier.PanelAgent, "dtop", revier.PanelSpec{Kind: revier.PanelAgent, Title: "dtop", Command: []string{"dtop", "--fast"}}},
		{revier.PanelShell, "", revier.PanelSpec{Kind: revier.PanelShell}},
	} {
		got, err := keyOutside(t, tc.kind, tc.agent, outsideAgents)
		if err != nil || got.Kind != tc.want.Kind || got.Title != tc.want.Title || !slices.Equal(got.Command, tc.want.Command) {
			t.Errorf("%s key %q: panel %+v, %v, want %+v", tc.kind, tc.agent, got, err, tc.want)
		}
	}
}

// With outside "agent" and no [[agent]] in config.toml the agent key has no
// agent to run, and opens the plain tab.
func TestTabAtPanelOutsideAWorkspaceWithAgentAndNoneDeclaredOpensAPlainTab(t *testing.T) {
	got, err := keyOutside(t, revier.PanelAgent, "", nil)
	if err != nil || got.Kind != revier.PanelShell || len(got.Command) != 0 {
		t.Errorf("panel %+v, %v, want the plain shell", got, err)
	}
}

// With outside "agent" a command that reads a project template, and a name
// config.toml does not declare, are refused and open no tab: no project holds
// the window to render the template against.
func TestTabAtPanelOutsideAWorkspaceWithAgentRefusesATemplate(t *testing.T) {
	templated := []core.NamedAgent{{Name: "dtop", Command: []string{"dtop", "{{.Path}}"}}}
	if _, err := keyOutside(t, revier.PanelAgent, "", templated); err == nil || !strings.Contains(err.Error(), "reads a project template") || !strings.Contains(err.Error(), "dtop") {
		t.Errorf("template: err = %v, want the agent named and the template refused", err)
	}
	if _, err := keyOutside(t, revier.PanelAgent, "nope", outsideAgents); !errors.Is(err, core.ErrUnknownAgent) {
		t.Errorf("unknown name: err = %v, want ErrUnknownAgent", err)
	}
}

// Every failure other than a window outside a workspace is returned, so the
// key's own fallback runs: a panel the runtime does not hold, a plain tab
// that does not open, and no runtime at all.
func TestTabAtPanelReturnsEveryOtherFailure(t *testing.T) {
	c, rt, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)
	if _, err := tabAtPanel(t, c, projects, "42", "", core.OutsideTab); !errors.Is(err, core.ErrNoPanel) {
		t.Errorf("unknown panel: err = %v, want ErrNoPanel", err)
	}

	rt.Add("a kitty window of no project", "", revier.Panel{ID: "9"})
	rt.OpenTabErr = errors.New("kitty is gone")
	if _, err := tabAtPanel(t, c, projects, "9", "", core.OutsideTab); err == nil || !strings.Contains(err.Error(), "kitty is gone") {
		t.Errorf("tab fails: err = %v, want it returned", err)
	}

	if _, err := tabAtPanel(t, &core.Core{}, projects, "9", "", core.OutsideTab); !errors.Is(err, core.ErrNoHost) {
		t.Errorf("no runtime: err = %v, want ErrNoHost", err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none", rt.Tabs)
	}
}

// An id no workspace holds is named.
func TestPanelOwnerNamesAnUnknownPanel(t *testing.T) {
	c, _, projects, _, _ := twoWorkspaces(t, []revier.Panel{{ID: "1"}}, nil)

	_, err := c.PanelOwner(context.Background(), projects, "42")
	if !errors.Is(err, core.ErrNoPanel) || !strings.Contains(err.Error(), "42") {
		t.Errorf("err = %v, want ErrNoPanel naming 42", err)
	}
}

// -p names a project; the target is the one that declares an agent panel.
func TestAgentTargetIsTheTargetDeclaringAnAgent(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	name, err := c.AgentTarget(prepared(t, agentProject()))
	if err != nil || name != "home" {
		t.Errorf("AgentTarget = %q, %v, want home", name, err)
	}

	two := agentProject()
	second := *two.Targets[0].Runtime
	second.Name, second.Match.Title = "second", "^second$"
	two.Targets[1].Runtime = &second
	if _, err := c.AgentTarget(prepared(t, two)); err == nil {
		t.Error("two targets with an agent panel: want an error asking for one")
	}
}

// A key pressed in a kitty workspace on GNOME raises the OS window around the
// new agent through the window host. kitty's own focus of an unfocused OS
// window is an "is ready" notice, not the window.
func TestNewAgentRaisesTheOSWindow(t *testing.T) {
	rt, wm, homeWm, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{resumable()}}

	if _, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{}); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != homeWm {
		t.Errorf("window focuses = %v, want the workspace's OS window %v raised", wm.Focuses, homeWm)
	}
}

// With no OS window to raise, nothing opens: the tab would come up behind, and
// its focus would be the notice a focus with no raise becomes
// (decisions.md D63).
func TestNewAgentOpensNothingItCannotRaise(t *testing.T) {
	rt, _, _, _ := osWindowHosts()
	c := &core.Core{Runtime: rt, Window: hosttest.New("wm"), Probes: []revier.AgentProbe{resumable()}}

	outcome, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{})
	if !errors.Is(err, core.ErrUnraisable) || outcome != core.AgentNotAdded {
		t.Errorf("%v, %v, want ErrUnraisable and not added", outcome, err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("Tabs = %+v, want none opened", rt.Tabs)
	}
}

// A restore lays a recorded conversation over the declared agent panel only
// when that panel runs its harness. A claude conversation over an opencode
// panel would start `opencode --resume <claude id>`: the agent starts empty
// instead, and the restore says why.
func TestRestoreDoesNotResumeAConversationIntoAnotherHarness(t *testing.T) {
	proj := agentProject()
	proj.Targets[3].Runtime.Panels[1] = revier.PanelSpec{Kind: revier.PanelAgent, Title: "opencode", Command: []string{"opencode"}}
	c := &core.Core{Probes: []revier.AgentProbe{resumable(), &hosttest.FakeProbe{Harness: "opencode", Marker: "opencode"}}}

	resumes := []core.Resume{{Harness: "claude", Session: "abc-123"}}
	panels := launched(t, c, proj, resumes)
	if got := panels[1].Command; !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("agent command = %q, want opencode as declared", got)
	}
	if got := c.Resumes(prepared(t, proj), "home", resumes); !slices.Equal(got, []core.AgentOutcome{core.AgentUnresumable}) {
		t.Errorf("outcomes = %v, want unresumable", got)
	}
}
