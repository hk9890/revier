// Layer L2: recording a session and planning its restore are decisions the
// core makes from what a host reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

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
						{Kind: revier.PanelAgent, Command: []string{"claude", "--model", "opus"}},
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

func resumable() *hosttest.ResumableProbe { return hosttest.NewResumableProbe("claude", "claude") }

// The recording is names: which project, which target, and the conversation an
// agent panel holds. A target with no live instance was not open and is left
// out, so restoring opens what was there and nothing else.
func TestSessionRecordsOnlyWhatIsOpen(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude", Vars: map[string]string{"session": "abc-123"}},
	)
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	s := c.Session(context.Background(), report, "revier")

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
	panels := s.Projects[0].Targets[0].Panels
	if len(panels) != 1 {
		t.Fatalf("panels = %+v, want the one agent", panels)
	}
	// Index 0: the first agent panel of the target, not the first panel.
	if panels[0].Index != 0 || panels[0].Harness != "claude" || panels[0].Session != "abc-123" {
		t.Errorf("panel = %+v, want the claude conversation at agent index 0", panels[0])
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
	if s := c.Session(context.Background(), report, ""); len(s.Projects) != 0 {
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
	s := c.Session(context.Background(), report, "")
	for _, tv := range s.Projects[0].Targets {
		if tv.Name == "" {
			t.Errorf("an attachment was recorded: %+v", tv)
		}
	}
	if len(s.Projects[0].Targets) != 1 {
		t.Errorf("targets = %+v, want only home", s.Projects[0].Targets)
	}
}

// A probe without the capability, and one that cannot say, both leave the
// panel unrecorded: it restores empty, which is the whole degradation.
func TestSessionRecordsNoConversationWithoutAResumableProbe(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelAgent, Title: "claude", Vars: map[string]string{"session": "abc-123"}},
	)
	plain := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude"}}}
	report, err := plain.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if panels := plain.Session(context.Background(), report, "").Projects[0].Targets[0].Panels; len(panels) != 0 {
		t.Errorf("panels = %+v, want none from a probe that cannot name one", panels)
	}

	failing := resumable()
	failing.SessionErr = errors.New("the harness did not answer")
	broken := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{failing}}
	report, err = broken.Survey(context.Background(), []core.Project{prepared(t, agentProject())}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if panels := broken.Session(context.Background(), report, "").Projects[0].Targets[0].Panels; len(panels) != 0 {
		t.Errorf("panels = %+v, want none when the probe failed", panels)
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

// The resume reaches the host as a rewritten panel command, with the project's
// own arguments kept: a project that runs its agent with a model flag keeps
// the flag across a restore.
func TestRestoreLaunchesTheAgentOnItsConversation(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())

	resumes := []core.Resume{{Index: 0, Harness: "claude", Session: "abc-123"}}
	if _, err := c.GoResuming(context.Background(), p, "home", nil, resumes); err != nil {
		t.Fatalf("GoResuming: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("Opened = %+v, want one launch", rt.Opened)
	}
	panels := rt.Opened[0].Panels
	if len(panels) != 2 {
		t.Fatalf("panels = %+v, want both", panels)
	}
	if !slices.Equal(panels[0].Command, []string{"zsh"}) {
		t.Errorf("the shell panel was rewritten: %v", panels[0].Command)
	}
	want := []string{"claude", "--model", "opus", "--resume", "abc-123"}
	if !slices.Equal(panels[1].Command, want) {
		t.Errorf("agent command = %v, want %v", panels[1].Command, want)
	}
}

// The realization arrives sharing the prepared project's panels. Writing the
// resume into it would leave the flag on every later keypress.
func TestResumeDoesNotEditTheProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	p := prepared(t, agentProject())

	resumes := []core.Resume{{Index: 0, Harness: "claude", Session: "abc-123"}}
	if _, err := c.GoResuming(context.Background(), p, "home", nil, resumes); err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "--model", "opus"}
	if got := p.Targets[0].Runtime.Panels[1].Command; !slices.Equal(got, want) {
		t.Errorf("the project now reads %v, want %v: the resume was written into it", got, want)
	}
}

// A recording this machine cannot honour is dropped, and that panel starts
// empty. Failing the restore instead would lose nineteen workspaces over one
// harness that is not installed here.
func TestResumeIsDroppedWhenNothingCanHonourIt(t *testing.T) {
	cases := []struct {
		name   string
		core   func(rt *hosttest.FakeRuntime) *core.Core
		resume core.Resume
	}{
		{
			name: "no probe of that harness",
			core: func(rt *hosttest.FakeRuntime) *core.Core {
				return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
			},
			resume: core.Resume{Index: 0, Harness: "opencode", Session: "abc-123"},
		},
		{
			name: "the probe cannot resume",
			core: func(rt *hosttest.FakeRuntime) *core.Core {
				return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude"}}}
			},
			resume: core.Resume{Index: 0, Harness: "claude", Session: "abc-123"},
		},
		{
			name: "no agent panel at that index any more",
			core: func(rt *hosttest.FakeRuntime) *core.Core {
				return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
			},
			resume: core.Resume{Index: 3, Harness: "claude", Session: "abc-123"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := hosttest.NewRuntime("rt")
			c := tc.core(rt)
			if _, err := c.GoResuming(context.Background(), prepared(t, agentProject()), "home", nil, []core.Resume{tc.resume}); err != nil {
				t.Fatalf("GoResuming: %v", err)
			}
			want := []string{"claude", "--model", "opus"}
			if got := rt.Opened[0].Panels[1].Command; !slices.Equal(got, want) {
				t.Errorf("agent command = %v, want the project as written", got)
			}
		})
	}
}
