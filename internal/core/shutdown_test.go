// Layer L2: what a shutdown closes, and in what order, is decided from what a
// host reports, so the fake host reports it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
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

// reading is the close every test asks for that is not about the busy guard:
// the normal path, whose recheck reads these projects again before it closes
// anything.
func reading(projects []core.Project) core.ShutdownOpts {
	return core.ShutdownOpts{Projects: projects}
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

	out, _ := c.Shutdown(context.Background(), plan, 0, reading(projects))
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
	_, _ = c.Shutdown(context.Background(), plan, 0, reading(projects))
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

	out, _ := c.Shutdown(context.Background(), plan, 0, reading(projects))
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

	out, _ := c.Shutdown(context.Background(), c.ShutdownPlan(report, "", core.ShutdownAll), 2*core.ClosePoll, reading(projects))
	if closed, open, _ := out.Counts(); closed != 2 || open != 1 || out[2].Note() != "still open" {
		t.Errorf("counts = %d closed, %d open, last %q; want the editor still open", closed, open, out[2].Note())
	}
}

// A close that fails is that step's failure, and the rest still close.
func TestShutdownGoesOnPastAFailedClose(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	rt.CloseErr = errors.New("socket gone")
	out, _ := c.Shutdown(context.Background(), c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll), 0, reading(projects))
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

	out, _ := c.Shutdown(context.Background(), plan, 0, reading(projects))
	if closed, _, failed := out.Counts(); closed != 2 || failed != 1 || out[0].Err == nil {
		t.Errorf("counts = %d closed, %d failed, home err %v; want home failed, notes and editor closed", closed, failed, out[0].Err)
	}
}

// A plan made before its host went is not closed through that host: the TUI
// can switch the runtime between the plan and the confirm.
func TestShutdownLeavesOpenWhatAHostThatWentPlanned(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	out, _ := c.WithRuntime(hosttest.NewRuntime("other")).Shutdown(context.Background(), plan, 0, reading(projects))
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

// del on a target closes that target alone, with the agent it holds.
func TestClosePlanClosesOneTarget(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)

	plan := c.ClosePlan(report, "revier", core.CloseRow{Target: "home"})
	if got := stepNames(plan); !slices.Equal(got, []string{"home"}) || len(plan[0].Agents) != 1 {
		t.Fatalf("plan = %+v, want home with its agent", plan)
	}
	_, _ = c.Shutdown(context.Background(), plan, 0, reading(projects))
	if len(rt.Closed) != 1 || len(wm.Closed) != 0 {
		t.Errorf("runtime closed %v, window host closed %v; want the workspace alone", rt.Closed, wm.Closed)
	}
}

// A tab target closes its tab and leaves the workspace it lives in.
func TestClosePlanClosesATabAlone(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.Add("session:revier", "kitty",
		agent("1", "", ""),
		revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "taskmgr-ui", Vars: map[string]string{core.PanelTargetVar: "tickets"}},
	)
	c := &core.Core{Runtime: rt}
	projects := []core.Project{prepared(t, tabProject())}

	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})
	if got := stepNames(plan); !slices.Equal(got, []string{"tickets#2"}) || plan[0].Action != core.ClosePanel {
		t.Errorf("plan = %+v, want the tab's panel", plan)
	}
}

// groupTab is a workspace whose tickets tab holds a panel group: the panel
// the target is found by, and an agent beside it in the same tab.
func groupTab(t *testing.T, status revier.Status) (*core.Core, *hosttest.FakeRuntime, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	home := agent("1", "", "")
	home.Tab = "t1"
	marked := revier.Panel{ID: "2", Kind: revier.PanelTool, Title: "taskmgr-ui", Tab: "t2", Vars: map[string]string{core.PanelTargetVar: "tickets"}}
	sibling := agent("3", "", "")
	sibling.Tab = "t2"
	rt.Add("session:revier", "kitty", home, marked, sibling)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: status}}
	return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}, rt, []core.Project{prepared(t, tabProject())}
}

// A tab target covers every panel of its tab: its step holds the agent in
// the panel the target is not found by, so a busy one refuses the step and a
// shutdown of the targets alone leaves the tab.
func TestATabStepHoldsTheAgentsOfEveryPanelInTheTab(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusRunning)
	report := survey(t, c, projects, nil)

	plan := c.ClosePlan(report, "revier", core.CloseRow{Target: "tickets"})
	if len(plan) != 1 || plan[0].Action != core.ClosePanel || len(plan[0].Agents) != 1 || plan[0].Agents[0].Panel != "3" || !plan[0].Busy() {
		t.Fatalf("del plan = %+v, want one busy tab step holding the agent in panel 3", plan)
	}
	if !slices.Equal(plan[0].Panels, []revier.PanelID{"2", "3"}) {
		t.Errorf("panels = %v, want both panels of the tab", plan[0].Panels)
	}
	if got := stepNames(c.ShutdownPlan(report, "", core.ShutdownTargets)); len(got) != 0 {
		t.Errorf("targets = %v, want the tab left: it holds an agent", got)
	}
}

// A tab closes whole: every panel of it goes through the panel closer, the
// workspace's own panel stays, and the step is closed.
func TestShutdownClosesTheWholeTab(t *testing.T) {
	c, rt, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})

	out, _ := c.Shutdown(context.Background(), plan, 0, reading(projects))
	if closed, open, failed := out.Counts(); closed != 1 || open != 0 || failed != 0 {
		t.Errorf("counts = %d closed, %d open, %d failed; want the tab closed", closed, open, failed)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2", "3"}) {
		t.Errorf("panels closed = %v, want both panels of the tab", rt.ClosedPanels)
	}
	left := survey(t, c, projects, nil).Instances
	if len(left) != 1 || len(left[0].Panels) != 1 || left[0].Panels[0].ID != "1" {
		t.Errorf("instances after = %+v, want the workspace with its own panel", left)
	}
}

// keepsPanel is a runtime that drops one panel of the tab and leaves the
// other listed, as a runtime that closed the panel without ending it would.
type keepsPanel struct {
	*hosttest.FakeRuntime
	keep revier.PanelID
}

func (k keepsPanel) ClosePanel(ctx context.Context, ref revier.TargetRef, panel revier.PanelID) error {
	if panel == k.keep {
		return nil
	}
	return k.FakeRuntime.ClosePanel(ctx, ref, panel)
}

// A tab is closed only once no panel of it is listed: one panel left behind
// keeps the whole step open.
func TestATabIsClosedOnlyWhenNoPanelOfItIsListed(t *testing.T) {
	c, rt, projects := groupTab(t, revier.StatusIdle)
	c.Runtime = keepsPanel{rt, "3"}
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})

	out, _ := c.Shutdown(context.Background(), plan, 0, reading(projects))
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("panels closed = %v, want panel 2: panel 3 was left listed", rt.ClosedPanels)
	}
	if closed, open, _ := out.Counts(); closed != 0 || open != 1 || out[0].Note() != "still open" {
		t.Errorf("counts = %d closed, %d open, note %q; want still open: panel 3 is listed", closed, open, out[0].Note())
	}
}

// addedTab is a workspace whose declared layout - a shell and an agent - is
// its own tab, with the tab `revier agent new` opened beside it: an agent and
// a shell of its own, neither carrying a target mark.
func addedTab(t *testing.T) (*core.Core, *hosttest.FakeRuntime, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	shell := revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh", Tab: "t1"}
	declared := agent("2", "", "")
	declared.Tab = "t1"
	added := agent("3", "", "")
	added.Tab = "t2"
	beside := revier.Panel{ID: "4", Kind: revier.PanelShell, Title: "zsh", Tab: "t2"}
	rt.Add("session:revier", "kitty", shell, declared, added, beside)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}}
	return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}, rt, []core.Project{prepared(t, agentProject())}
}

// agentOn is the agent view of a panel.
func agentOn(t *testing.T, r core.Report, panel revier.PanelID) revier.AgentView {
	t.Helper()
	for _, a := range r.Views[0].Agents {
		if a.Panel == panel {
			return a
		}
	}
	t.Fatalf("no agent in panel %s: %+v", panel, r.Views[0].Agents)
	return revier.AgentView{}
}

// del on an agent `revier agent new` opened closes the tab it opened, so the
// shell beside it goes too: that tab carries no target mark of its own to be
// closed by.
func TestClosePlanClosesAnAddedAgentWithItsTab(t *testing.T) {
	c, rt, projects := addedTab(t)
	a := agentOn(t, survey(t, c, projects, nil), "3")

	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Agent: a.Ref, Panel: a.Panel})
	if len(plan) != 1 || plan[0].Action != core.ClosePanel || !slices.Equal(plan[0].Panels, []revier.PanelID{"3", "4"}) {
		t.Fatalf("plan = %+v, want the agent tab whole", plan)
	}
	_, _ = c.Shutdown(context.Background(), plan, 0, reading(projects))
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"3", "4"}) {
		t.Errorf("panels closed = %v, want the agent and the shell beside it", rt.ClosedPanels)
	}
	left := survey(t, c, projects, nil).Instances
	if len(left) != 1 || len(left[0].Panels) != 2 {
		t.Errorf("instances after = %+v, want the workspace tab with its shell and agent", left)
	}
}

// A shutdown of the agents alone closes an added agent's tab whole and the
// declared agent as its panel alone: the workspace keeps the shell it
// declares beside it.
func TestShutdownAgentsClosesAnAddedTabAndKeepsTheDeclaredShell(t *testing.T) {
	c, rt, projects := addedTab(t)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAgents)

	if got := stepNames(plan); !slices.Equal(got, []string{"#2", "#3"}) {
		t.Fatalf("plan = %v, want a step for each agent", got)
	}
	if !slices.Equal(plan[0].Panels, []revier.PanelID{"2"}) || !slices.Equal(plan[1].Panels, []revier.PanelID{"3", "4"}) {
		t.Fatalf("panels = %v and %v, want the declared agent alone and the added tab whole", plan[0].Panels, plan[1].Panels)
	}
	_, _ = c.Shutdown(context.Background(), plan, 0, reading(projects))
	left := survey(t, c, projects, nil).Instances
	if len(left) != 1 || len(left[0].Panels) != 1 || left[0].Panels[0].ID != "1" {
		t.Errorf("instances after = %+v, want the workspace shell alone", left)
	}
	if len(rt.ClosedPanels) != 3 {
		t.Errorf("panels closed = %v, want three", rt.ClosedPanels)
	}
}

// An agent closes its panel; the workspace and its shell stay.
func TestClosePlanClosesOneAgent(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusRunning)
	report := survey(t, c, projects, nil)
	a := report.Views[0].Agents[0]

	plan := c.ClosePlan(report, "revier", core.CloseRow{Agent: a.Ref, Panel: a.Panel})
	if got := stepNames(plan); !slices.Equal(got, []string{"#2"}) || plan[0].Action != core.ClosePanel || !plan[0].Busy() {
		t.Errorf("plan = %+v, want the busy agent's panel alone", plan)
	}
}

// A window attached by hand closes on its own.
func TestClosePlanClosesAnAttachedWindow(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	stray := wm.Add("Meld", "meld")
	report := survey(t, c, projects, map[revier.ProjectName][]revier.TargetRef{"revier": {stray}})

	plan := c.ClosePlan(report, "revier", core.CloseRow{Attached: stray})
	if len(plan) != 1 || plan[0].Ref != stray || plan[0].Action != core.CloseInstance {
		t.Errorf("plan = %+v, want the attached window", plan)
	}
}

// A row with nothing open closes nothing.
func TestClosePlanOfWhatIsNotOpenIsEmpty(t *testing.T) {
	c, _, _, _ := openDesktop(t, revier.StatusIdle)
	projects := []core.Project{prepared(t, agentProject())}
	c.Runtime = hosttest.NewRuntime("rt")
	report := survey(t, c, projects, nil)

	for _, row := range []core.CloseRow{{Target: "home"}, {Target: "nothing"}, {Agent: revier.TargetRef{Host: "rt", ID: "1"}, Panel: "2"}} {
		if plan := c.ClosePlan(report, "revier", row); len(plan) != 0 {
			t.Errorf("row %+v: plan = %+v, want none", row, plan)
		}
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

// A tab step is rechecked against every panel of its tab, so an agent that
// turns busy in the panel beside the one that names the step refuses the
// close.
func TestShutdownRefusesABusyAgentBesideTheMarkedPanelOfATab(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})
	if len(plan) != 1 {
		t.Fatalf("plan = %+v, want one tab step", plan)
	}

	c.Probes = []revier.AgentProbe{&hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning}}}
	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects})
	var refused *core.BusyRefusal
	if !errors.As(err, &refused) || out != nil {
		t.Fatalf("shutdown = %+v, %v; want a busy refusal", out, err)
	}
	if len(refused.Plan) != 1 || !refused.Plan[0].Busy() {
		t.Errorf("refused plan = %+v, want the busy agent in panel 3", refused.Plan)
	}
}

// The same tab closes once the agent beside it is idle again, and the close
// is not refused for an agent the plan was drawn with.
func TestShutdownClosesWhenTheRecheckFindsNoBusyAgent(t *testing.T) {
	c, _, projects := groupTab(t, revier.StatusIdle)
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Target: "tickets"})

	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if closed, _, _ := out.Counts(); closed != 1 {
		t.Errorf("counts = %d closed, want the tab closed", closed)
	}
}

// A step whose agents the recheck could not read closes nothing and says why:
// a degraded survey reports no agent, and no agent read is not idle.
func TestShutdownLeavesOpenAStepTheRecheckCannotRead(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if len(rt.Closed) != 0 || len(wm.Closed) != 0 {
		t.Errorf("closed %v and %v, want nothing: no project was surveyed", rt.Closed, wm.Closed)
	}
	if closed, open, _ := out.Counts(); closed != 0 || open != len(plan) {
		t.Errorf("counts = %d closed, %d open; want every step left open", closed, open)
	}
	if note := out[0].Note(); !strings.Contains(note, "no longer surveyed") {
		t.Errorf("note = %q, want the reason the step was left", note)
	}
}

// One project the survey could not reach leaves its own steps open and closes
// every other project's: a refusal disables the smallest thing that is wrong
// (decisions.md D85).
func TestShutdownClosesTheProjectsTheRecheckCouldRead(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	other := agentProject()
	other.Name = "other"
	other.Targets[0].Runtime.Name = "session:other"
	other.Targets[0].Runtime.Match = revier.Match{Title: "^session:other$"}
	other.Targets = other.Targets[:1]
	rt.Add("session:other", "kitty")
	projects = append(projects, prepared(t, other))
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)

	// Only "other" is surveyed again, so every step of "revier" is unread.
	out, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects[1:]})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if closed, open, _ := out.Counts(); closed != 1 || open != 3 {
		t.Errorf("counts = %d closed, %d open; want other's workspace closed and revier's three steps left", closed, open)
	}
	if len(rt.Closed) != 1 || rt.Closed[0].Title != "session:other" {
		t.Errorf("runtime closed %v, want other's workspace alone", rt.Closed)
	}
}

// A force says not to refuse, and nothing else. The plan a refusal hands back
// carries the mark of a step its survey could not read, and the force reads
// everything again rather than carrying that mark: a host answering once more
// closes the step the user asked for.
func TestAForcedCloseReadsAStepARefusalCouldNotRead(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusRunning)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	wm.SetInstancesErr(errors.New("went away"))

	_, err := c.Shutdown(context.Background(), plan, 0, reading(projects))
	var refused *core.BusyRefusal
	if !errors.As(err, &refused) {
		t.Fatalf("shutdown err = %v, want the busy agent to refuse it", err)
	}
	i := slices.IndexFunc(refused.Plan, func(s core.CloseStep) bool { return s.Target == "editor" })
	if i < 0 || refused.Plan[i].Unread == "" {
		t.Fatalf("refused plan = %+v, want the editor marked as unread", refused.Plan)
	}

	wm.SetInstancesErr(nil)
	out, err := c.Shutdown(context.Background(), refused.Plan, 0,
		core.ShutdownOpts{Force: true, Projects: projects})
	if err != nil {
		t.Fatalf("forced shutdown: %v", err)
	}
	if closed, open, _ := out.Counts(); closed != 3 || open != 0 {
		t.Errorf("counts = %d closed, %d open; want the busy agent and the editor closed", closed, open)
	}
	if len(wm.Closed) != 1 {
		t.Errorf("window host closed %v, want the editor: its host answers again", wm.Closed)
	}
}

// The step that would end the calling process goes last, ordered off the
// survey the recheck took: a plan-time listing can name an instance that is
// already gone. A forced close reads that survey too, so it orders the same.
func TestShutdownOrdersItsOwnStepLastFromTheRecheck(t *testing.T) {
	self := func(p revier.Panel) bool { return p.ID == "1" }
	for _, tc := range []struct {
		name   string
		status revier.Status
		force  bool
	}{
		{"unforced", revier.StatusIdle, false},
		{"forced past a busy agent", revier.StatusRunning, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, projects := openDesktop(t, tc.status)
			plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
			if got, want := stepNames(plan), []string{"home", "notes", "editor"}; !slices.Equal(got, want) {
				t.Fatalf("plan = %v, want %v", got, want)
			}

			out, err := c.Shutdown(context.Background(), plan, 0,
				core.ShutdownOpts{Force: tc.force, Projects: projects, Self: self})
			if err != nil {
				t.Fatalf("shutdown: %v", err)
			}
			if got, want := closedNames(out), []string{"notes", "editor", "home"}; !slices.Equal(got, want) {
				t.Errorf("closed in %v, want %v: the workspace this process runs under goes last", got, want)
			}
		})
	}
}

// closedNames names the steps of a shutdown in the order it walked them.
func closedNames(out core.Closed) []string {
	plan := make([]core.CloseStep, len(out))
	for i, r := range out {
		plan[i] = r.CloseStep
	}
	return stepNames(plan)
}

// ctxCloser closes only on a live context, as a host that runs a command
// does: an exec on a context already done fails without reaching the tool.
type ctxCloser struct{ *hosttest.Fake }

func (h ctxCloser) Close(ctx context.Context, ref revier.TargetRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return h.Fake.Close(ctx, ref)
}

// The closes have a budget of their own, so whatever the recheck and the save
// spent of the caller's, every step is still asked to close. Ninety projects
// and a link host that does not answer is where a caller's bound runs out
// before the first close.
func TestShutdownClosesOnItsOwnBudgetAfterASlowSave(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	c.Window = ctxCloser{wm}
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	out, err := c.Shutdown(ctx, plan, 0, core.ShutdownOpts{Projects: projects, Before: func(context.Context, core.Report) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if closed, _, failed := out.Counts(); closed != 3 || failed != 0 {
		t.Errorf("counts = %d closed, %d failed; want every step closed past the caller's deadline", closed, failed)
	}
}

// The save runs on a budget of its own too. Both surveys reach every link
// host, and a caller's bound spent on them left the save asking those hosts
// on a context already done, which failed the save and closed nothing.
func TestShutdownSavesOnItsOwnBudgetAfterASlowRecheck(t *testing.T) {
	c, _, _, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	time.Sleep(30 * time.Millisecond) // the caller's bound passes before the save

	var live bool
	out, err := c.Shutdown(ctx, plan, 0, core.ShutdownOpts{Projects: projects, Before: func(saving context.Context, _ core.Report) error {
		live = saving.Err() == nil
		return saving.Err()
	}})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !live {
		t.Error("the save was handed a context already done, want its own budget")
	}
	if closed, _, _ := out.Counts(); closed != 3 {
		t.Errorf("counts = %d closed, want every step closed", closed)
	}
}

// A caller that cancels cancels the closes too: a deadline that passed before
// them is not a decision, and a cancel is.
func TestShutdownStopsClosingWhenTheCallerCancels(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	c.Window = ctxCloser{wm}
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out, err := c.Shutdown(ctx, plan, 0, core.ShutdownOpts{Projects: projects, Before: func(context.Context, core.Report) error {
		cancel()
		return nil
	}})
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, _, failed := out.Counts(); failed == 0 {
		t.Errorf("counts = %d failed, want the cancelled close to fail", failed)
	}
}

// The recheck reads a step's panels again, not only its agents. A tab that
// gained a panel between the plan and the close closes whole, and an agent
// working in that panel refuses the close like any other: a step drawn from
// the older listing would neither see it nor end it, and the tab would stay
// open while the result called it closed.
func TestTheRecheckReadsTheTabAgainAndRefusesAnAgentAddedToIt(t *testing.T) {
	c, rt, projects := groupTab(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	plan := c.ClosePlan(report, "revier", core.CloseRow{Target: "tickets"})
	if len(plan) != 1 || !slices.Equal(plan[0].Panels, []revier.PanelID{"2", "3"}) {
		t.Fatalf("plan = %+v, want the tab as it was listed", plan)
	}

	// A second harness, so the panel added to the tab is the only busy one:
	// the agent the plan was drawn with stays idle.
	c.Probes = append(c.Probes, &hosttest.FakeProbe{
		Harness: "aider", Marker: "aider",
		State: revier.AgentState{Harness: "aider", Status: revier.StatusRunning},
	})
	rt.AddPanel(report.Instances[0].Ref, revier.Panel{ID: "4", Kind: revier.PanelAgent, Title: "aider", Tab: "t2"})

	out, err := c.Shutdown(context.Background(), plan, 0, reading(projects))
	var refused *core.BusyRefusal
	if !errors.As(err, &refused) || out != nil {
		t.Fatalf("shutdown = %+v, %v; want a busy refusal for the agent added to the tab", out, err)
	}
	if len(refused.Plan) != 1 || !slices.Equal(refused.Plan[0].Panels, []revier.PanelID{"2", "3", "4"}) {
		t.Errorf("refused panels = %v, want the tab as it is now", refused.Plan[0].Panels)
	}
	if len(rt.ClosedPanels) != 0 {
		t.Errorf("closed %v, want nothing closed", rt.ClosedPanels)
	}
}

// Forced, the same tab closes whole: the added panel goes with it, because
// force says not to refuse a busy agent and nothing else.
func TestAForcedCloseClosesTheTabTheRecheckReadAgain(t *testing.T) {
	c, rt, projects := groupTab(t, revier.StatusIdle)
	report := survey(t, c, projects, nil)
	plan := c.ClosePlan(report, "revier", core.CloseRow{Target: "tickets"})
	rt.AddPanel(report.Instances[0].Ref, revier.Panel{ID: "4", Kind: revier.PanelTool, Title: "less", Tab: "t2"})

	if _, err := c.Shutdown(context.Background(), plan, 0, core.ShutdownOpts{Projects: projects, Force: true}); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2", "3", "4"}) {
		t.Errorf("panels closed = %v, want every panel the tab holds now", rt.ClosedPanels)
	}
}

// A shutdown whose every step leaves saves nothing: it changes nothing, so
// there is nothing to record, and a session written from it would say the
// desktop had been closed (decisions.md D99).
func TestAShutdownThatClosesNothingSavesNothing(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	c.Runtime = bareRuntime{hosttest.NewRuntime("rt")}
	c.Window = closeless{wm}
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	if len(plan) == 0 {
		t.Fatal("plan is empty; want steps no host can close")
	}
	for _, s := range plan {
		if !s.Action.Leaves() {
			t.Fatalf("step %s = %v, want every step one no host closes", s.Name(), s.Action)
		}
	}

	saved := false
	opts := reading(projects)
	opts.Before = func(context.Context, core.Report) error { saved = true; return nil }
	out, err := c.Shutdown(context.Background(), plan, 0, opts)
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if saved {
		t.Error("the session was saved; want no save when nothing closes")
	}
	if closed, open, _ := out.Counts(); closed != 0 || open != len(plan) {
		t.Errorf("counts = %d closed, %d open; want every step left open", closed, open)
	}
}

// An instance that names no own panel - every panel carries a target mark and
// none carries the home one, as an instance opened before the mark existed
// can - closes an agent as that panel alone. Reading it as a tab opened
// beside the workspace is a guess, and the guess that is wrong takes the
// shell the layout declares with it (decisions.md D94, D100).
func TestAnAgentClosesAloneWhenTheInstanceNamesNoOwnPanel(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	marked := func(id revier.PanelID, tab string, p revier.Panel) revier.Panel {
		p.ID, p.Tab, p.Vars = id, tab, map[string]string{core.PanelTargetVar: "tickets"}
		return p
	}
	rt.Add("session:revier", "kitty",
		marked("1", "t1", revier.Panel{Kind: revier.PanelTool, Title: "taskmgr-ui"}),
		marked("2", "t2", agent("2", "", "")),
		marked("3", "t2", revier.Panel{Kind: revier.PanelShell, Title: "zsh"}),
	)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: revier.StatusIdle}}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	projects := []core.Project{prepared(t, tabProject())}

	a := agentOn(t, survey(t, c, projects, nil), "2")
	plan := c.ClosePlan(survey(t, c, projects, nil), "revier", core.CloseRow{Agent: a.Ref, Panel: a.Panel})
	if len(plan) != 1 || !slices.Equal(plan[0].Panels, []revier.PanelID{"2"}) {
		t.Fatalf("plan = %+v, want the agent's panel alone", plan)
	}

	if _, err := c.Shutdown(context.Background(), plan, 0, reading(projects)); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("panels closed = %v, want the agent alone: the shell beside it stays", rt.ClosedPanels)
	}
}

// ctxRuntime is a runtime that refuses to list on a context that is done, as
// a host reached over ssh does and the fake by itself does not.
type ctxRuntime struct{ *hosttest.FakeRuntime }

func (c ctxRuntime) Instances(ctx context.Context) ([]revier.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.FakeRuntime.Instances(ctx)
}

// A shutdown whose caller has no time left still closes. The survey the plan
// was drawn from is paid for out of the same bound, and on ninety projects
// with a link host that does not answer it spends most of it; a recheck
// handed the remains reads no host, marks every step unread and closes
// nothing (decisions.md D99).
func TestShutdownRechecksOnItsOwnBudgetAfterASlowPlan(t *testing.T) {
	c, rt, wm, projects := openDesktop(t, revier.StatusIdle)
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	c.Runtime = ctxRuntime{rt}

	spent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	out, err := c.Shutdown(spent, plan, 0, reading(projects))
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if closed, open, failed := out.Counts(); closed != 3 || open != 0 || failed != 0 {
		t.Errorf("counts = %d closed, %d open, %d failed; want everything closed", closed, open, failed)
	}
	if len(rt.Closed) != 2 || len(wm.Closed) != 1 {
		t.Errorf("closed %v and %v, want both workspaces and the window", rt.Closed, wm.Closed)
	}
}

// A caller that cancelled before the call still stops the closes: the budget
// each phase counts for itself drops the caller's deadline and not the
// caller's cancellation, which is a decision about this close.
func TestShutdownStopsWhenTheCallerCancelledBeforeIt(t *testing.T) {
	c, _, wm, projects := openDesktop(t, revier.StatusIdle)
	c.Window = ctxCloser{wm}
	ctx, cancel := context.WithCancel(context.Background())
	plan := c.ShutdownPlan(survey(t, c, projects, nil), "", core.ShutdownAll)
	cancel()

	out, err := c.Shutdown(ctx, plan, 0, reading(projects))
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, _, failed := out.Counts(); failed == 0 {
		t.Errorf("counts = %d failed, want the cancelled close to fail", failed)
	}
}
