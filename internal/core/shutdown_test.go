// Layer L2: what a shutdown closes, and in what order, is decided from what a
// host reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/pkg/revier"
)

// openDesktop is agentProject with every target open: the workspace holds a
// shell and an agent in the given status, notes runs on its own, and the editor
// is a window.
func openDesktop(t *testing.T, status revier.Status) (*core.Core, *hosttest.FakeRuntime, *hosttest.Fake, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh"},
		agent("2", "abc-123", ""),
	)
	rt.Add("notes:revier", "kitty", revier.Panel{ID: "3", Kind: revier.PanelTool, Title: "less"})
	wm := hosttest.New("wm")
	wm.Add("revier - code", "code")
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: status}}
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{probe}}
	return c, rt, wm, []core.Project{prepared(t, agentProject())}
}

func survey(t *testing.T, c *core.Core, projects []core.Project, attached map[revier.ProjectName][]revier.TargetRef) core.Report {
	t.Helper()
	r, err := c.Survey(context.Background(), projects, nil, attached)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return r
}

func stepNames(plan []core.CloseStep) []string {
	var out []string
	for _, s := range plan {
		name := string(s.Target)
		if s.Panel != "" {
			name += "#" + string(s.Panel)
		}
		out = append(out, name)
	}
	return out
}

// A full shutdown closes every open target through the host that lists it,
// and the agent in the workspace ends with the workspace.
func TestShutdownAllClosesEveryOpenTarget(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	if got, want := stepNames(plan), []string{"home", "notes", "editor"}; !slices.Equal(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	if len(plan[0].Agents) != 1 || plan[0].Busy() {
		t.Errorf("home step = %+v, want its one idle agent", plan[0])
	}

	out := c.Shutdown(context.Background(), plan, 0)
	if closed, open, failed := out.Counts(); closed != 3 || open != 0 || failed != 0 {
		t.Errorf("counts = %d closed, %d open, %d failed; want 3 closed", closed, open, failed)
	}
	if len(rt.Closed) != 2 || len(wm.Closed) != 1 {
		t.Errorf("runtime closed %v, window host closed %v; want two and one", rt.Closed, wm.Closed)
	}
	if left := survey(t, c, projects, nil); len(left.Instances) != 0 {
		t.Errorf("instances after = %+v, want none", left.Instances)
	}
}

// Only the agents: each agent panel closes, and the workspace that held it
// stays with its shell.
func TestShutdownAgentsClosesOnlyTheAgentPanels(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAgents)

	if got := stepNames(plan); !slices.Equal(got, []string{"#2"}) || plan[0].Action != core.ClosePanel {
		t.Fatalf("plan = %+v, want the agent panel alone", plan)
	}
	c.Shutdown(context.Background(), plan, 0)
	if len(rt.Closed) != 0 || len(wm.Closed) != 0 || !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("closed %v and %v, panels %v; want panel 2 only", rt.Closed, wm.Closed, rt.ClosedPanels)
	}
}

// Only the targets: whatever holds no agent closes, and the workspace with
// the agent stays, so no conversation ends.
func TestShutdownTargetsLeavesWhatHoldsAnAgent(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownTargets)

	if got, want := stepNames(plan), []string{"notes", "editor"}; !slices.Equal(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}
}

// An agent that works or waits for an answer makes its step busy: the caller
// refuses on it unless forced.
func TestShutdownPlanMarksABusyAgent(t *testing.T) {
	for _, status := range []revier.Status{revier.StatusRunning, revier.StatusAttention} {
		c, _, _, projects := openDesktop(t, status)
		plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
		if busy := core.Busy(plan); len(busy) != 1 || busy[0].Target != "home" {
			t.Errorf("status %v: busy = %v, want home", status, stepNames(busy))
		}
	}
}

// A project shutdown closes that project's targets and nothing of another's.
func TestShutdownOfOneProjectLeavesTheOthers(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	other := agentProject()
	other.Name = "other"
	other.Targets[0].Runtime.Name = "session:other"
	other.Targets[0].Runtime.Match = revier.Match{Title: "^session:other$"}
	other.Targets = other.Targets[:1]
	rt.Add("session:other", "kitty")
	projects = append(projects, prepared(t, other))

	plan := c.ShutdownPlan(survey(t, c, projects, nil), "other", core.ShutdownAll)
	if len(plan) != 1 || plan[0].Project != "other" {
		t.Errorf("plan = %+v, want other's home alone", plan)
	}
}

// An instance two projects hold stays open when one of them shuts down: the
// other still uses it. A full shutdown closes it once.
func TestShutdownOfOneProjectKeepsWhatAnotherHolds(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	other := agentProject()
	other.Name = "other"
	other.Targets = other.Targets[:1]
	projects = append(projects, prepared(t, other))
	report := survey(t, c, projects, nil)

	if got, want := stepNames(c.ShutdownPlan(report, "revier", core.ShutdownAll)), []string{"notes", "editor"}; !slices.Equal(got, want) {
		t.Errorf("revier alone = %v, want %v, the shared workspace kept", got, want)
	}
	if got := c.ShutdownPlan(report, "revier", core.ShutdownAgents); len(got) != 0 {
		t.Errorf("revier's agents = %v, want the shared workspace's agent kept", stepNames(got))
	}
	if got, want := stepNames(c.ShutdownPlan(report, "", core.ShutdownAll)), []string{"home", "notes", "editor"}; !slices.Equal(got, want) {
		t.Errorf("everything = %v, want %v", got, want)
	}
}

// A window attached to the project by hand is the project's, and closes with
// it.
func TestShutdownClosesAnAttachedWindow(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	stray := wm.Add("Meld", "meld")
	attached := map[revier.ProjectName][]revier.TargetRef{"revier": {stray}}

	plan := c.ShutdownPlan(survey(t, c, projects, attached), "", core.ShutdownTargets)
	if !slices.ContainsFunc(plan, func(s core.CloseStep) bool { return s.Ref == stray && s.Target == "" }) {
		t.Errorf("plan = %+v, want the attached window in it", plan)
	}
}

// closeless is a window host without the capability to close.
type closeless struct{ revier.WindowController }

// A host that cannot close is not asked to: its step is named as left open.
func TestShutdownLeavesOpenWhatItsHostCannotClose(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	c.Window = closeless{wm}
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	out := c.Shutdown(context.Background(), plan, 0)
	if len(wm.Closed) != 0 {
		t.Errorf("window host closed %v, want nothing asked of it", wm.Closed)
	}
	if closed, open, _ := out.Counts(); closed != 2 || open != 1 {
		t.Errorf("counts = %d closed, %d open; want 2 and 1", closed, open)
	}
	if note := out[2].Note(); note != "left open: its host cannot close it" {
		t.Errorf("note = %q", note)
	}
}

// An application may keep its window to ask about unsaved work. The shutdown
// waits for it, and then names it as still open.
func TestShutdownNamesAWindowThatStayed(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	wm.Refuses = map[string]bool{report.Windows[0].Ref.ID: true}

	out := c.Shutdown(context.Background(), c.ShutdownPlan(report, "", core.ShutdownAll), 2*core.ClosePoll)
	if closed, open, _ := out.Counts(); closed != 2 || open != 1 || out[2].Note() != "still open" {
		t.Errorf("counts = %d closed, %d open, last %q; want the editor still open", closed, open, out[2].Note())
	}
}

// A close that fails is that step's failure, and the rest still close.
func TestShutdownGoesOnPastAFailedClose(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	rt.CloseErr = errors.New("socket gone")
	out := c.Shutdown(context.Background(), c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll), 0)
	if closed, _, failed := out.Counts(); failed != 2 || closed != 1 {
		t.Errorf("counts = %d closed, %d failed; want the editor closed past two failures", closed, failed)
	}
}

// A close that fails on something already gone is closed: the listing, not
// Close, says whether it went.
func TestShutdownCountsAFailedCloseOfWhatIsGoneAsClosed(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)
	rt.Remove(plan[1].Ref)
	rt.CloseErr = errors.New("no such window")

	out := c.Shutdown(context.Background(), plan, 0)
	if closed, _, failed := out.Counts(); closed != 2 || failed != 1 || out[0].Err == nil {
		t.Errorf("counts = %d closed, %d failed, home err %v; want home failed, notes and editor closed", closed, failed, out[0].Err)
	}
}

// A plan made before its host went is not closed through that host: the TUI
// can switch the runtime between the plan and the confirm.
func TestShutdownLeavesOpenWhatAHostThatWentPlanned(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	out := c.WithRuntime(hosttest.NewRuntime("other")).Shutdown(context.Background(), plan, 0)
	if closed, open, _ := out.Counts(); closed != 1 || open != 2 || len(wm.Closed) != 1 {
		t.Errorf("counts = %d closed, %d open, window host closed %v; want the editor closed and both workspaces left", closed, open, wm.Closed)
	}
}

// A tab lives in its workspace. A full shutdown closes the workspace once;
// closing only the targets closes the tab alone when the workspace holds an
// agent.
func TestShutdownClosesATabWithItsWorkspaceOrAlone(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:revier", "kitty",
		agent("1", "", ""),
		revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "taskmgr-ui", Vars: map[string]string{core.PanelTargetVar: "tickets"}},
	)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude"}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	projects := []core.Project{prepared(t, tabProject())}
	report := survey(t, c, projects, nil)

	if got := stepNames(c.ShutdownPlan(report, "", core.ShutdownAll)); !slices.Equal(got, []string{"home"}) {
		t.Errorf("all = %v, want the workspace once", got)
	}
	if got := stepNames(c.ShutdownPlan(report, "", core.ShutdownTargets)); !slices.Equal(got, []string{"tickets#2"}) {
		t.Errorf("targets = %v, want the tab alone", got)
	}
}

// The terminal a shutdown runs in closes last, so the process lives until
// everything else is closed.
func TestCloseLastPutsTheCallersOwnTerminalAtTheEnd(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	plan := c.ShutdownPlan(report, "", core.ShutdownAll)

	self := func(p revier.Panel) bool { return p.ID == "1" }
	if got, want := stepNames(core.CloseLast(plan, report.Instances, self)), []string{"notes", "editor", "home"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// A shutdown saves what is open, once: a second shutdown of the same desktop
// finds the newest session already holds it.
func TestSaveChangedSavesOnlyWhatChanged(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	c.Probes = []revier.AgentProbe{resumable()}
	root := t.TempDir()
	at := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

	first, saved, _, err := c.SaveChanged(context.Background(), root, survey(t, c, projects, nil), "revier", at)
	if err != nil || !saved {
		t.Fatalf("first save = %v, %v; want a save", saved, err)
	}
	if _, saved, _, _ := c.SaveChanged(context.Background(), root, survey(t, c, projects, nil), "", at.Add(time.Minute)); saved {
		t.Error("second save wrote a session, want the same desktop left unsaved")
	}
	rt.Remove(survey(t, c, projects, nil).Views[0].Targets[1].Ref)
	second, saved, _, err := c.SaveChanged(context.Background(), root, survey(t, c, projects, nil), "", at.Add(2*time.Minute))
	if err != nil || !saved || second.ID == first.ID {
		t.Errorf("save after a change = %v, %v, id %q; want a new session", saved, err, second.ID)
	}
	all, _ := session.List(root)
	if len(all) != 2 {
		t.Errorf("sessions = %d, want 2", len(all))
	}
}

// Nothing open is nothing to save: an empty session would become the newest,
// the one a plain restore opens.
func TestSaveChangedSavesNothingWhenNothingIsOpen(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	projects := []core.Project{prepared(t, agentProject())}
	root := t.TempDir()
	if _, saved, _, err := c.SaveChanged(context.Background(), root, survey(t, c, projects, nil), "", time.Now()); saved || err != nil {
		t.Errorf("save = %v, %v; want none", saved, err)
	}
}
