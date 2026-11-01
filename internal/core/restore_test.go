// Layer L2: walking a restore is a decision the core makes from what a host
// reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// A restore over a desktop that is half up opens what is missing and leaves
// the rest alone, so running it twice opens nothing twice.
func TestRestoreOpensOnlyWhatIsNotUp(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	c := &core.Core{Runtime: rt}
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home"}, {Name: "notes"},
	}}}}

	out, l, back := restoreOf(t, c, s)
	if back != nil {
		t.Errorf("back = %v, want none with no current project", back)
	}
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "notes:revier" {
		t.Fatalf("Opened = %+v, want notes alone", rt.Opened)
	}
	if out[0].Action != core.RestoreRunning || out[0].Note() != "running" {
		t.Errorf("home = %+v (%q), want it left alone as running", out[0], out[0].Note())
	}
	if out[1].Ref.IsZero() || out[1].Note() != "opened" {
		t.Errorf("notes = %+v (%q), want it opened", out[1], out[1].Note())
	}
	if opened, pending, failed := out.Counts(); opened != 1 || pending != 0 || failed != 0 {
		t.Errorf("counts = %d, %d, %d, want 1 opened", opened, pending, failed)
	}
	if !slices.Equal(l.writes, []string{"landed revier/notes"}) {
		t.Errorf("writes = %v, want the landing of notes", l.writes)
	}

	again, _, _ := restoreOf(t, c, s)
	if opened, _, _ := again.Counts(); opened != 0 || len(rt.Opened) != 1 {
		t.Errorf("a second restore opened %d, Opened = %d, want nothing more", opened, len(rt.Opened))
	}
}

// The state record has each launch of a restore with what came of its
// agents, then the session restored with what it holds and its counts.
func TestARestoreRecordsEachStepAndItsAgents(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Probes: []revier.AgentProbe{resumable()}}
	s := session.Session{ID: "2026-10-16T23-54-55", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home", Agents: []session.Agent{{Harness: "claude", Session: "abc-123"}}}, {Name: "notes"},
	}}}}

	_, l, _ := restoreOf(t, c, s)
	steps := l.noted(record.KindRestoreStep)
	want := []record.Outcome{{Harness: "claude", Session: "abc-123", Outcome: "resumed"}}
	if len(steps) != 2 || steps[0].Target != "home" || steps[0].Ref == "" || !slices.Equal(steps[0].Agents, want) || steps[1].Target != "notes" {
		t.Errorf("restore steps = %+v, want home with its agent resumed, then notes", steps)
	}
	restored := l.noted(record.KindSessionRestored)
	if len(restored) != 1 || restored[0].Saved != s.ID || restored[0].Counts["opened"] != 2 || len(restored[0].Projects) != 1 {
		t.Errorf("session restored = %+v, want %s with its project and 2 opened", restored, s.ID)
	}
}

// One target that does not open is named, and the steps after it still run.
func TestAFailedStepDoesNotStopTheRestore(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.OpenErr = errors.New("no socket")
	c := &core.Core{Runtime: rt}
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home"}, {Name: "notes"},
	}}}}

	out, _, _ := restoreOf(t, c, s)
	if len(rt.Opened) != 2 {
		t.Errorf("Opened = %d, want both steps tried", len(rt.Opened))
	}
	if _, _, failed := out.Counts(); failed != 2 {
		t.Errorf("failed = %d, want 2", failed)
	}
	if !strings.Contains(out[1].Note(), "no socket") {
		t.Errorf("note = %q, want the failure named", out[1].Note())
	}
}

// A window that appears after its launch is recorded as launched before the
// wait and landed after it, as a keypress records it.
func TestARestoredWindowIsLaunchedThenLanded(t *testing.T) {
	wm := hosttest.New("wm")
	wm.Detached = true
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	s := session.Session{Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "editor"}}}}}

	out, l, _ := restoreOf(t, c, s)
	if out[0].Ref.IsZero() {
		t.Errorf("editor = %+v, want it bound to the window that appeared", out[0])
	}
	if want := []string{"launched revier/editor", "landed revier/editor"}; !slices.Equal(l.writes, want) {
		t.Errorf("writes = %v, want %v", l.writes, want)
	}
}

// A window a restore step binds is not raised: the return home is the one
// raise a restore makes, so the desktop does not flip through every window it
// opens. A press still raises the window it binds.
func TestARestoreStepDoesNotRaiseTheWindowItBinds(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	home := rt.Add("session:revier", "kitty")
	wm := hosttest.New("wm")
	wm.Detached = true
	c := &core.Core{Runtime: rt, Window: wm}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "editor"}}}}}

	out, _, back := restoreOf(t, c, s)
	if back != nil {
		t.Fatalf("back = %v", back)
	}
	if out[0].Ref.IsZero() {
		t.Fatalf("editor = %+v, want it bound", out[0])
	}
	if len(wm.Focuses) != 0 {
		t.Errorf("window focuses = %v, want none: the bound window stays where the desktop put it", wm.Focuses)
	}
	if n := len(rt.Focuses); n == 0 || rt.Focuses[n-1] != home {
		t.Errorf("runtime focuses = %v, want the restore to end on home", rt.Focuses)
	}
}

// A terminal window a restore step opens is not raised either, though a press
// that opens one raises it: the return home is still the one raise.
func TestARestoreStepDoesNotRaiseTheTerminalWindowItOpens(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	rt.Add("session:revier", "kitty")
	wm := hosttest.New("wm")
	homeWm := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	rt.OnOpen = func(revier.Realization) {
		wm.AddInstance(revier.Instance{Title: "notes:revier", Class: "kitty", PID: 1002})
	}
	c := &core.Core{Runtime: rt, Window: wm}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{{Name: "notes"}}}}}

	out, _, back := restoreOf(t, c, s)
	if back != nil || len(out) == 0 || out[0].Err != nil || out[0].Ref.IsZero() {
		t.Fatalf("restore = %+v, back = %v, want notes opened", out, back)
	}
	if !slices.Equal(wm.Focuses, []revier.TargetRef{homeWm}) {
		t.Errorf("window focuses = %v, want the return home's alone", wm.Focuses)
	}
}

// A restore ends on the project the save was left on.
func TestRestoreEndsOnTheSavedCurrentProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	home := rt.Add("session:revier", "kitty")
	c := &core.Core{Runtime: rt}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "notes"},
	}}}}

	if _, _, back := restoreOf(t, c, s); back != nil {
		t.Fatalf("back = %v", back)
	}
	if n := len(rt.Focuses); n == 0 || rt.Focuses[n-1].ID != home.ID {
		t.Errorf("Focuses = %+v, want it to end on home", rt.Focuses)
	}
}

// A preview says what a restore would do and does none of it.
func TestRestorePreviewOpensNothing(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	projects := []core.Project{prepared(t, agentProject())}
	report, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "home", Agents: []session.Agent{{Harness: "claude", Session: "abc-123"}}},
	}}}}

	out := c.RestorePreview(s, report, projects)
	if len(rt.Opened) != 0 || len(rt.Focuses) != 0 {
		t.Errorf("Opened = %v, Focuses = %v, want nothing touched", rt.Opened, rt.Focuses)
	}
	if want := "would open, 1 agent resumed"; out[0].Note() != want {
		t.Errorf("note = %q, want %q", out[0].Note(), want)
	}
}

// An agent that ran in a tab target is named with its own reason. The reason
// for a dropped agent - no agent panel declared - would send the reader to look
// for a declaration a tab cannot have.
func TestAgentsNoteNamesAnAgentInATabTarget(t *testing.T) {
	note := core.AgentsNote("open", []core.AgentOutcome{core.AgentInTab}, nil, errors.New("unused"))
	if want := "open, 1 agent not resumed: it ran in a tab target"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
	if strings.Contains(note, "no agent panel declared") {
		t.Errorf("note %q gives the dropped agent's reason", note)
	}
}

// A conversation that was recorded and cannot be resumed here is named. Said
// nothing, it reads as an agent that never had one.
func TestAgentsNoteNamesAnUnresumableConversation(t *testing.T) {
	note := core.AgentsNote("opened", []core.AgentOutcome{core.AgentResumed, core.AgentUnresumable, core.AgentEmpty}, nil, nil)
	if want := "opened, 1 agent resumed, 1 agent empty: no probe here resumes its harness in its panel"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// Every gap a save found is one line, and a save with none says nothing.
func TestSessionGapsNotesEveryGap(t *testing.T) {
	if notes := (core.SessionGaps{}).Notes(); len(notes) != 0 {
		t.Errorf("notes = %q, want none", notes)
	}
	gaps := core.SessionGaps{
		Unnamed: 2, InTab: []string{"revier:tickets"},
		Failed: []error{errors.New("claude: not found")}, Attached: 1,
	}
	want := []string{
		"2 agents without a conversation id, to be restored empty",
		"1 agent in a tab target, to be restored without its conversation: revier:tickets",
		"could not ask claude: not found",
		"1 attached instance not recorded; they have no name to be reopened by",
	}
	if notes := gaps.Notes(); !slices.Equal(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

// A project last acted on and closed before the save is not reopened: the
// restore launches nothing its plan did not show.
func TestRestoreDoesNotLaunchACurrentHomeItDidNotRecord(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	s := session.Session{Current: "revier", Projects: []session.Project{{Name: "revier", Targets: []session.Target{
		{Name: "notes"},
	}}}}

	if _, _, back := restoreOf(t, c, s); back != nil {
		t.Fatalf("back = %v", back)
	}
	if len(rt.Opened) != 1 || rt.Opened[0].Name != "notes:revier" {
		t.Errorf("opened = %+v, want notes alone: home was not open at the save", rt.Opened)
	}
}
