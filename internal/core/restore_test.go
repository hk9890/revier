// Layer L2: walking a restore is a decision the core makes from what a host
// reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// ledger is state as a restore writes it, in memory, with every write in
// order.
type ledger struct {
	bound  map[revier.ProjectName]core.Bindings
	writes []string
}

func (l *ledger) Bound(p revier.ProjectName) core.Bindings { return l.bound[p] }

func (l *ledger) Pending(revier.ProjectName, revier.TargetName) bool { return false }

func (l *ledger) Launched(p revier.ProjectName, t revier.TargetName, _ time.Time) {
	l.writes = append(l.writes, fmt.Sprintf("launched %s/%s", p, t))
}

func (l *ledger) Landed(p revier.ProjectName, t revier.TargetName, ref revier.TargetRef) {
	if l.bound == nil {
		l.bound = map[revier.ProjectName]core.Bindings{}
	}
	if l.bound[p] == nil {
		l.bound[p] = core.Bindings{}
	}
	l.bound[p][t] = ref
	l.writes = append(l.writes, fmt.Sprintf("landed %s/%s", p, t))
}

// restoreOf surveys the agent project and restores s over it.
func restoreOf(t *testing.T, c *core.Core, s session.Session) (core.Restored, *ledger, error) {
	t.Helper()
	projects := []core.Project{prepared(t, agentProject())}
	report, err := c.Survey(context.Background(), projects, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l := &ledger{}
	out, back := c.Restore(context.Background(), s, report, projects, l)
	return out, l, back
}

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
	report, err := c.Survey(context.Background(), projects, nil, nil)
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
	note := core.AgentsNote("open", []core.AgentOutcome{core.AgentInTab}, errors.New("unused"))
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
	note := core.AgentsNote("opened", []core.AgentOutcome{core.AgentResumed, core.AgentUnresumable, core.AgentEmpty}, nil)
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
