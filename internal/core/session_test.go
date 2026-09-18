// Layer L2: recording a session and planning its restore are decisions the
// core makes from what a host reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// agentProject is a workspace whose home holds an agent beside a shell, which
// is the layout a resume has to find its way back into.
func agentProject() revier.Project {
	return revier.Project{
		Name: "revier",
		Path: "/home/hans/dev/github/revier",
		Targets: []revier.Target{
			{
				Name: "home", Home: true,
				Runtime: &revier.Realization{
					Name:  "session:revier",
					Match: revier.Match{Title: "^session:revier$"},
					Panels: []revier.PanelSpec{
						{Kind: revier.PanelShell, Command: []string{"zsh"}},
						{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus"}},
					},
				},
			},
			{
				Name: "notes",
				Runtime: &revier.Realization{
					Name: "notes:revier", Launch: []string{"less"},
					Match: revier.Match{Title: "^notes:revier$"},
				},
			},
			{
				Name: "editor",
				Window: &revier.Realization{
					Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
				},
			},
		},
	}
}

func resumable() *hosttest.FakeResumableProbe { return hosttest.NewResumableProbe("claude", "claude") }

// agent is a live panel the fake probe claims, holding a conversation when id
// is not empty, in dir when dir is not empty.
func agent(panel revier.PanelID, id, dir string) revier.Panel {
	vars := map[string]string{}
	if id != "" {
		vars["session"] = id
	}
	if dir != "" {
		vars["dir"] = dir
	}
	return revier.Panel{ID: panel, Kind: revier.PanelAgent, Title: "claude", Vars: vars}
}

// launched restores one target from a recording and returns the panels the
// host was asked to open.
func launched(t *testing.T, c *core.Core, proj revier.Project, resumes []core.Resume) []revier.PanelSpec {
	t.Helper()
	return restored(t, c, proj, resumes).Opened[0].Panels
}

// restored restores one target from a recording and returns the runtime, for
// the panels it opened and the agent tabs it added after.
func restored(t *testing.T, c *core.Core, proj revier.Project, resumes []core.Resume) *hosttest.FakeRuntime {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c.Runtime = rt
	if _, err := c.GoResuming(context.Background(), prepared(t, proj), "home", nil, resumes); err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("Opened = %+v, want one launch", rt.Opened)
	}
	return rt
}

// The recording is names: which project, which target, and what each agent was
// doing. A target with no live instance was not open and is left out, so
// restoring opens what was there and nothing else.
func TestSessionRecordsOnlyWhatIsOpen(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh"},
		agent("2", "abc-123", "/home/hans/dev/github/revier"),
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s, _ := c.Session(context.Background(), report, "revier")

	if s.Current != "revier" {
		t.Errorf("Current = %q, want the focused project", s.Current)
	}
	if len(s.Projects) != 1 {
		t.Fatalf("Projects = %+v, want one", s.Projects)
	}
	names := []revier.TargetName{}
	for _, tv := range s.Projects[0].Targets {
		names = append(names, tv.Name)
	}
	// notes is declared and not running; editor has no host here at all.
	if !slices.Equal(names, []revier.TargetName{"home"}) {
		t.Errorf("targets = %v, want only the live one", names)
	}
	want := []session.Agent{{Harness: "claude", Session: "abc-123", Dir: "/home/hans/dev/github/revier"}}
	if got := s.Projects[0].Targets[0].Agents; !slices.Equal(got, want) {
		t.Errorf("agents = %+v, want %+v", got, want)
	}
}

// Every agent is recorded in the order the runtime lists it, the one with no
// conversation too: the order is what a restore lays agents out by, and one
// left out would move every agent after it.
func TestSessionRecordsEveryAgentInOrder(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		agent("1", "first", "/a"),
		revier.Panel{ID: "2", Kind: revier.PanelShell, Title: "zsh"},
		agent("3", "", ""),
		agent("4", "third", "/a/.claude/worktrees/tui"),
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps := c.Session(context.Background(), report, "")

	want := []session.Agent{
		{Harness: "claude", Session: "first", Dir: "/a"},
		{Harness: "claude"},
		{Harness: "claude", Session: "third", Dir: "/a/.claude/worktrees/tui"},
	}
	if got := s.Projects[0].Targets[0].Agents; !slices.Equal(got, want) {
		t.Errorf("agents = %+v, want %+v", got, want)
	}
	if gaps.Unnamed != 1 {
		t.Errorf("Unnamed = %d, want the one with no conversation", gaps.Unnamed)
	}
}

// A project with nothing open is not in the file: restoring it would open a
// workspace the save did not have.
func TestSessionLeavesOutAProjectWithNothingOpen(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Session(context.Background(), report, ""); len(s.Projects) != 0 {
		t.Errorf("Projects = %+v, want none", s.Projects)
	}
}

// An attachment is a live id with no launch argv anywhere in the model. It
// cannot be recorded, and recording it under a name it does not have would
// promise a restore that cannot happen.
func TestSessionLeavesOutAttachedInstances(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	stray := rt.Add("some tool nobody declared", "meld")
	c := &core.Core{Runtime: rt}

	attached := map[revier.ProjectName][]revier.TargetRef{"revier": {stray}}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, attached)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps := c.Session(context.Background(), report, "")
	if gaps.Attached != 1 {
		t.Errorf("gaps.Attached = %d, want the one attachment counted as left out", gaps.Attached)
	}
	for _, tv := range s.Projects[0].Targets {
		if tv.Name == "" {
			t.Errorf("an attachment was recorded: %+v", tv)
		}
	}
	if len(s.Projects[0].Targets) != 1 {
		t.Errorf("targets = %+v, want only home", s.Projects[0].Targets)
	}
}

// A probe without the capability, and one that cannot say, both record the
// agent with no conversation: it restores empty, which is the whole
// degradation. Either way the save counts the agent as unnamed, so the gap is
// said before the reboot.
func TestSessionRecordsNoConversationWithoutAResumableProbe(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", agent("1", "abc-123", "/a"))
	noConversation := []session.Agent{{Harness: "claude"}}

	plain := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude"}}}
	report, err := plain.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps := plain.Session(context.Background(), report, "")
	if got := s.Projects[0].Targets[0].Agents; !slices.Equal(got, noConversation) {
		t.Errorf("agents = %+v, want the agent with no conversation from a probe that cannot name one", got)
	}
	if gaps.Unnamed != 1 {
		t.Errorf("Unnamed = %d, want the one agent", gaps.Unnamed)
	}

	failing := resumable()
	failing.SessionErr = errors.New("the harness did not answer")
	broken := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{failing}}
	report, err = broken.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps = broken.Session(context.Background(), report, "")
	if got := s.Projects[0].Targets[0].Agents; !slices.Equal(got, noConversation) {
		t.Errorf("agents = %+v, want the agent with no conversation when the probe failed", got)
	}
	if gaps.Unnamed != 1 {
		t.Errorf("Unnamed = %d, want the one agent", gaps.Unnamed)
	}
	// The failure is said, with the probe's name, so an agent a broken probe
	// could not name is not taken for one no listing matched.
	if len(gaps.Failed) != 1 || !errors.Is(gaps.Failed[0], failing.SessionErr) || !strings.HasPrefix(gaps.Failed[0].Error(), "claude: ") {
		t.Errorf("Failed = %v, want the probe's error, named", gaps.Failed)
	}
}

// The probe may answer with a process - `claude agents --json` - so a save
// asks it once for every agent it claimed, across every project, not once per
// panel. An agent with an id and one without are told apart in that one
// answer.
func TestSessionAsksEachProbeOnceForTheWholeSave(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh"},
		agent("2", "abc-123", ""),
	)
	rt.Add("session:other", "kitty", agent("3", "", ""), agent("4", "def-456", ""))
	other := agentProject()
	other.Name = "other"
	other.Targets[0].Runtime.Name = "session:other"
	other.Targets[0].Runtime.Match = revier.Match{Title: "^session:other$"}
	probe := resumable()
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject()), prepared(t, other)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, gaps := c.Session(context.Background(), report, "")

	if probe.Calls != 1 {
		t.Errorf("Sessions was called %d times, want once for the whole save", probe.Calls)
	}
	if gaps.Unnamed != 1 {
		t.Errorf("Unnamed = %d, want the one agent with no id", gaps.Unnamed)
	}
	if len(s.Projects) != 2 {
		t.Fatalf("projects = %+v, want both", s.Projects)
	}
	got := map[revier.ProjectName][]session.Agent{}
	for _, p := range s.Projects {
		got[p.Name] = p.Targets[0].Agents
	}
	if want := []session.Agent{{Harness: "claude", Session: "abc-123"}}; !slices.Equal(got["revier"], want) {
		t.Errorf("revier agents = %+v, want %+v", got["revier"], want)
	}
	if want := []session.Agent{{Harness: "claude"}, {Harness: "claude", Session: "def-456"}}; !slices.Equal(got["other"], want) {
		t.Errorf("other agents = %+v, want %+v", got["other"], want)
	}
}

// The plan is the whole of restore's policy, and every outcome but the launch
// is a normal one the command steps over.
func TestRestorePlanClassifiesEveryRecordedTarget(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty") // home is up; notes is not
	c := &core.Core{Runtime: rt}      // no window host, so editor has none

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := session.Session{Projects: []session.Project{
		{Name: "revier", Targets: []session.Target{
			{Name: "home"}, {Name: "notes"}, {Name: "editor"}, {Name: "gone"},
		}},
		{Name: "deleted-since", Targets: []session.Target{{Name: "home"}}},
	}}

	want := []core.RestoreAction{
		core.RestoreRunning,   // home, already up: restore leaves it alone
		core.RestoreLaunch,    // notes, declared and not running
		core.RestoreNoHost,    // editor, window-only on a headless machine
		core.RestoreNoTarget,  // gone, no longer declared
		core.RestoreNoProject, // the project file is not here any more
	}
	plan := c.RestorePlan(s, report)
	if len(plan) != len(want) {
		t.Fatalf("plan = %+v, want %d steps", plan, len(want))
	}
	for i, step := range plan {
		if step.Action != want[i] {
			t.Errorf("step %d (%s/%s) = %v, want %v", i, step.Project, step.Target, step.Action, want[i])
		}
	}
}

// Order is the file's and nothing reorders it: a launch is bound to the window
// that appears after it, so the caller has to walk the plan one at a time.
func TestRestorePlanKeepsTheFileOrder(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "notes"}, {Name: "home"},
	}}}}
	plan := c.RestorePlan(s, report)
	if plan[0].Target != "notes" || plan[1].Target != "home" {
		t.Errorf("plan = %+v, want the file's order", plan)
	}
}

// A dry run says what becomes of every recorded agent before anything opens,
// from the same realization a launch resolves, so the two cannot disagree.
func TestResumesSaysWhatBecomesOfEachAgent(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())
	worktree := t.TempDir()

	home := c.Resumes(p, "home", []core.Resume{
		{Harness: "claude", Session: "abc-123", Dir: worktree},
		{Harness: "claude", Session: "def-456", Dir: worktree + "/removed"},
		{Harness: "claude"},
	})
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentDirGone, core.AgentEmpty}; !slices.Equal(home, want) {
		t.Errorf("home agents = %v, want %v", home, want)
	}
	// notes launches a pager and declares no agent panel to start one from.
	notes := c.Resumes(p, "notes", []core.Resume{{Harness: "claude", Session: "ghi-789"}})
	if want := []core.AgentOutcome{core.AgentDropped}; !slices.Equal(notes, want) {
		t.Errorf("notes agents = %v, want %v", notes, want)
	}
}

// A target a window host realizes here has no panels, whatever its runtime
// realization declares: the launch drops its agents, and the dry run says so.
func TestResumesLaysAgentsOverTheRealizationThatWins(t *testing.T) {
	proj := agentProject()
	proj.Targets[0].Window = &revier.Realization{Launch: []string{"kitty"}, Match: revier.Match{Class: "^kitty$"}}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: hosttest.New("wm"), Probes: []revier.AgentProbe{resumable()}}

	got := c.Resumes(prepared(t, proj), "home", []core.Resume{{Harness: "claude", Session: "abc-123"}})
	if want := []core.AgentOutcome{core.AgentDropped}; !slices.Equal(got, want) {
		t.Errorf("agents = %v, want %v", got, want)
	}
}

// The resume reaches the host as a rewritten panel command, with the project's
// own arguments kept, started in the directory the agent worked in: a project
// that runs its agent with a model flag keeps the flag across a restore, and a
// worktree agent carries on in its worktree.
func TestRestoreLaunchesTheAgentOnItsConversation(t *testing.T) {
	c := &core.Core{Probes: []revier.AgentProbe{resumable()}}
	worktree := t.TempDir()

	panels := launched(t, c, agentProject(), []core.Resume{{Harness: "claude", Session: "abc-123", Dir: worktree}})
	if len(panels) != 2 {
		t.Fatalf("panels = %+v, want the layout", panels)
	}
	if !slices.Equal(panels[0].Command, []string{"zsh"}) || panels[0].Dir != "/home/hans/dev/github/revier" {
		t.Errorf("the shell panel was rewritten: %+v", panels[0])
	}
	want := []string{"claude", "--model", "opus", "--resume", "abc-123"}
	if !slices.Equal(panels[1].Command, want) {
		t.Errorf("agent command = %v, want %v", panels[1].Command, want)
	}
	if panels[1].Dir != worktree {
		t.Errorf("agent starts in %q, want its worktree %q", panels[1].Dir, worktree)
	}
}

// samePanels reports every difference between two panel lists.
func samePanels(t *testing.T, what string, got, want []revier.PanelSpec) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %+v, want %d panels", what, got, len(want))
	}
	for i := range want {
		if !slices.Equal(got[i].Command, want[i].Command) || got[i].Dir != want[i].Dir ||
			got[i].Kind != want[i].Kind || got[i].Title != want[i].Title {
			t.Errorf("%s panel %d = %+v, want %+v", what, i, got[i], want[i])
		}
	}
}

// tabPanels is the panels of each agent tab a runtime was asked to open.
func tabPanels(rt *hosttest.FakeRuntime) [][]revier.PanelSpec {
	var out [][]revier.PanelSpec
	for _, tab := range rt.Tabs {
		out = append(out, tab.Real.Panels)
	}
	return out
}

// Agents past the ones the layout declares were opened beside it by hand. Once
// the workspace is open, each gets an agent tab, in order - the tab `revier
// agent new` opens: the project's agent panel on its own conversation and the
// project's shell, both in the agent's directory. The panel that was current
// when the workspace opened is current again after them.
func TestRestoreOpensTheAgentsPastTheLayoutAsAgentTabs(t *testing.T) {
	c := &core.Core{Probes: []revier.AgentProbe{resumable()}}
	root, worktree := t.TempDir(), t.TempDir()

	rt := hosttest.NewRuntime("rt")
	c.Runtime = rt
	res, err := c.GoResuming(context.Background(), prepared(t, agentProject()), "home", nil, []core.Resume{
		{Harness: "claude", Session: "declared", Dir: root},
		{Harness: "claude", Session: "by-hand", Dir: worktree},
		{Harness: "claude"},
	})
	if err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	samePanels(t, "opened", rt.Opened[0].Panels, []revier.PanelSpec{
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: "/home/hans/dev/github/revier"},
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus", "--resume", "declared"}, Dir: root},
	})
	tabs := tabPanels(rt)
	if len(tabs) != 2 {
		t.Fatalf("tabs = %+v, want an agent tab for each agent past the layout", tabs)
	}
	project := "/home/hans/dev/github/revier"
	samePanels(t, "first tab", tabs[0], []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus", "--resume", "by-hand"}, Dir: worktree},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: worktree},
	})
	samePanels(t, "second tab", tabs[1], []revier.PanelSpec{
		{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus"}, Dir: project},
		{Kind: revier.PanelShell, Command: []string{"zsh"}, Dir: project},
	})
	if n := len(rt.PanelFocuses); n == 0 || rt.PanelFocuses[n-1] != hosttest.OpenedPanel {
		t.Errorf("panel focuses = %v, want the workspace's current panel last", rt.PanelFocuses)
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentResumed, core.AgentEmpty}; !slices.Equal(res.Agents, want) {
		t.Errorf("Agents = %v, want %v", res.Agents, want)
	}
}

// A worktree removed since the save: resumed anywhere else, the conversation
// carries on in the wrong checkout and its edits land there. The agent starts
// empty where the workspace starts instead.
func TestRestoreStartsAnAgentEmptyWhenItsDirectoryIsGone(t *testing.T) {
	c := &core.Core{Probes: []revier.AgentProbe{resumable()}}
	gone := t.TempDir() + "/removed-worktree"

	rt := restored(t, c, agentProject(), []core.Resume{
		{Harness: "claude", Session: "declared", Dir: gone},
		{Harness: "claude", Session: "by-hand", Dir: gone},
	})
	declared := rt.Opened[0].Panels[1]
	if !slices.Equal(declared.Command, []string{"claude", "--model", "opus"}) || declared.Dir != "/home/hans/dev/github/revier" {
		t.Errorf("declared agent = %+v, want it empty where the workspace starts", declared)
	}
	tabs := tabPanels(rt)
	if len(tabs) != 1 {
		t.Fatalf("tabs = %+v, want the agent past the layout still opened", tabs)
	}
	if tab := tabs[0][0]; !slices.Equal(tab.Command, []string{"claude", "--model", "opus"}) || tab.Dir != "/home/hans/dev/github/revier" {
		t.Errorf("tab agent = %+v, want it empty where the workspace starts", tab)
	}
}

// A restore and `revier agent new` build an agent tab through the same code:
// the tab a restore opens for an agent is the tab agent new opens for the same
// conversation and directory.
func TestRestoreOpensTheTabAgentNewOpens(t *testing.T) {
	worktree := t.TempDir()
	r := core.Resume{Harness: "claude", Session: "by-hand", Dir: worktree}

	c := &core.Core{Probes: []revier.AgentProbe{resumable()}}
	restoredTab := tabPanels(restored(t, c, agentProject(), []core.Resume{{Harness: "claude"}, r}))[0]

	rt := hosttest.NewRuntime("rt")
	c = &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	rt.Add("session:revier", "")
	if _, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{Session: "by-hand", Dir: worktree}); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	samePanels(t, "agent new tab", tabPanels(rt)[0], restoredTab)
}

// A tab that fails after the workspace opened does not fail the launch: the
// workspace is focused and returned for binding, and the agents past the
// layout are named as not added, with the reason.
func TestRestoreKeepsTheWorkspaceWhenAnAgentTabFails(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.OpenTabErr = errors.New("kitty went away")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	res, err := c.GoResuming(context.Background(), prepared(t, agentProject()), "home", nil, []core.Resume{
		{Harness: "claude", Session: "a"}, {Harness: "claude", Session: "b"}, {Harness: "claude", Session: "c"},
	})
	if err != nil {
		t.Fatalf("GoResuming: %v, want the open workspace kept", err)
	}
	if res.Ref.IsZero() || len(rt.Focuses) != 1 || rt.Focuses[0] != res.Ref {
		t.Errorf("Ref = %+v, focuses = %+v, want the workspace returned and focused", res.Ref, rt.Focuses)
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentNotAdded, core.AgentNotAdded}; !slices.Equal(res.Agents, want) {
		t.Errorf("Agents = %v, want %v", res.Agents, want)
	}
	if res.AgentErr == nil || !strings.Contains(res.AgentErr.Error(), "kitty went away") {
		t.Errorf("AgentErr = %v, want the reason", res.AgentErr)
	}
	if n := len(rt.PanelFocuses); n == 0 || rt.PanelFocuses[n-1] != hosttest.OpenedPanel {
		t.Errorf("panel focuses = %v, want the workspace's current panel again after the failed tab", rt.PanelFocuses)
	}
}

// The launch reports what it did with each recorded agent, which is what the
// restore prints: a count taken from the recording would say resumed for an
// agent the launch dropped.
func TestGoResumingReportsWhatBecameOfEachAgent(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Probes: []revier.AgentProbe{resumable()}}
	res, err := c.GoResuming(context.Background(), prepared(t, agentProject()), "home", nil, []core.Resume{
		{Harness: "claude", Session: "abc-123"},
		{Harness: "opencode", Session: "def-456"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []core.AgentOutcome{core.AgentResumed, core.AgentUnresumable}; !slices.Equal(res.Agents, want) {
		t.Errorf("Agents = %v, want %v", res.Agents, want)
	}
}

// The realization arrives sharing the prepared project's panels. Writing the
// resume into it would leave the flag on every later keypress.
func TestResumeDoesNotEditTheProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())

	resumes := []core.Resume{{Harness: "claude", Session: "abc-123", Dir: t.TempDir()}, {Harness: "claude", Session: "def-456"}}
	if _, err := c.GoResuming(context.Background(), p, "home", nil, resumes); err != nil {
		t.Fatal(err)
	}
	panels := p.Targets[0].Runtime.Panels
	if len(panels) != 2 {
		t.Errorf("the project now has %d panels, want its 2: a tab was added to it", len(panels))
	}
	want := []string{"claude", "--model", "opus"}
	if got := panels[1]; !slices.Equal(got.Command, want) || got.Dir != "/home/hans/dev/github/revier" {
		t.Errorf("the project now reads %+v, want %v: the resume was written into it", got, want)
	}
}

// A recording this machine cannot honour is dropped, and that agent starts
// empty. Failing the restore instead would lose nineteen workspaces over one
// harness that is not installed here.
func TestResumeIsDroppedWhenNothingCanHonourIt(t *testing.T) {
	cases := []struct {
		name   string
		probes []revier.AgentProbe
		resume core.Resume
	}{
		{
			name:   "no probe of that harness",
			probes: []revier.AgentProbe{resumable()},
			resume: core.Resume{Harness: "opencode", Session: "abc-123"},
		},
		{
			name:   "the probe cannot resume",
			probes: []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude"}},
			resume: core.Resume{Harness: "claude", Session: "abc-123"},
		},
		{
			name:   "no conversation recorded",
			probes: []revier.AgentProbe{resumable()},
			resume: core.Resume{Harness: "claude"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panels := launched(t, &core.Core{Probes: tc.probes}, agentProject(), []core.Resume{tc.resume})
			if !slices.Equal(panels[0].Command, []string{"zsh"}) {
				t.Errorf("shell command = %v, want the project as written", panels[0].Command)
			}
			if want := []string{"claude", "--model", "opus"}; !slices.Equal(panels[1].Command, want) {
				t.Errorf("agent command = %v, want the project as written", panels[1].Command)
			}
		})
	}
}

// A save with nothing open is refused with a normal outcome, not written: an
// empty session would become the newest, the one a plain restore opens. With
// something open, the save is written and stamped as asked.
func TestSaveSessionRefusesAnEmptySave(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	projects := []core.Project{prepared(t, agentProject())}
	root := t.TempDir()
	if _, _, _, err := c.SaveSession(context.Background(), root, survey(t, c, projects, nil), "", "before", time.Now()); !errors.Is(err, core.ErrNothingOpen) {
		t.Errorf("save = %v, want ErrNothingOpen", err)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want none written", all)
	}

	c, _, _, projects = openDesktop(t, revier.StatusIdle)
	at := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)
	stored, path, _, err := c.SaveSession(context.Background(), root, survey(t, c, projects, nil), "revier", "before", at)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if stored.Name != "before" || !stored.At.Equal(at) || stored.Current != "revier" || len(stored.Projects) == 0 {
		t.Errorf("stored = %+v, want the name, moment and current project as given", stored)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path %s: %v, want the file written", path, err)
	}
}
