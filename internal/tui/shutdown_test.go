package tui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// The shutdown's button is on the sessions screen, not on the surface's bar,
// and its key opens the wizard from the surface. A full shutdown plans every
// project, names the busy agent on the button that confirms, saves the
// session as a shutdown's, records the shutdown, closes everything, says what
// came of it, and quits on the next key.
func TestAltQFullShutdownSavesThenClosesEverything(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	if bar := barLine(m); strings.Contains(bar, "shutdown") {
		t.Errorf("bar = %q, want no shutdown button on the surface", bar)
	}
	m, _ = press(m, "alt+q")
	if bar := barLine(m); !strings.Contains(bar, "Shutdown") || !strings.Contains(screen(m), "Full shutdown") {
		t.Fatalf("screen = %q, want the wizard's first step", screen(m))
	}

	m, cmd := press(m, "enter")
	m = run(m, cmd)
	// The world's agent waits for an answer, so the confirm says so.
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shut down anyway: 1 busy agent") {
		t.Errorf("rows = %q, want the busy agent named on the confirm", body)
	}
	if plan := pane(m); !strings.Contains(plan, "Shutdown plan") || !strings.Contains(plan, "project-01") || !strings.Contains(plan, "home") {
		t.Errorf("pane = %q, want project-01's home in the plan", plan)
	}

	m, cmd = press(m, "enter")
	m = run(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shutdown complete: closed 1. It is safe to leave.") || !strings.Contains(body, "saved as session") ||
		!strings.Contains(body, "revier quits") {
		t.Errorf("rows = %q, want the result, the save and the quit", body)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want none", left)
	}
	all, _ := session.List(root)
	if len(all) != 1 || all[0].Reason != session.ReasonShutdown {
		t.Fatalf("sessions = %+v, want the one saved before the close, saved by a shutdown", all)
	}
	if sd := shutDownOnRecord(t, root); sd == nil || sd.Session != all[0].ID {
		t.Errorf("shutdown on record = %+v, want one naming session %s", sd, all[0].ID)
	}
	// The logs key too: a screen opened now would close with the surface.
	for _, k := range []string{"esc", "alt+l"} {
		if _, quit := press(m, k); quit == nil || quit() != tea.Quit() {
			t.Errorf("%s on the result of a full shutdown did not quit", k)
		}
	}
}

// The sessions screen's bar has the save and the shutdown buttons. Its
// shutdown opens the wizard, and Esc from the wizard's first step goes back to
// the sessions screen.
func TestTheSessionsScreenOpensTheShutdown(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "alt+s")
	if bar := barLine(m); !strings.Contains(bar, "save alt+s") || !strings.Contains(bar, "shutdown alt+q") {
		t.Fatalf("bar = %q, want the save and the shutdown buttons", bar)
	}
	m, _ = press(m, "alt+q")
	if sub := lines(m)[2]; !strings.Contains(sub, "What to shut down?") {
		t.Fatalf("subtitle = %q, want the wizard's first step", sub)
	}
	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "Sessions") {
		t.Errorf("bar = %q, want the sessions screen back", bar)
	}
	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the surface", bar)
	}
}

// A full shutdown says what came of it, and quits once the wait is over.
func TestAFullShutdownQuitsAfterItsResult(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)
	m, cmd := press(m, "enter")
	m, after := deliver(m, cmd)
	if after == nil {
		t.Fatal("the full shutdown's answer returned no command, want the wait before the quit")
	}
	if _, quit := update(m, tui.ShutQuit()); quit == nil || quit() != tea.Quit() {
		t.Error("the end of the wait did not quit")
	}
}

// A full shutdown whose recheck left nothing to close saved and recorded
// nothing, so the surface stays up: the next start would offer no restore.
func TestAFullShutdownThatClosedNothingStaysUp(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)
	rt.InstancesErr = errors.New("socket gone")
	m, cmd := press(m, "enter")
	m, after := deliver(m, cmd)
	if after != nil {
		t.Error("the answer returned a command, want no wait before a quit")
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "nothing was saved") || strings.Contains(body, "revier quits") {
		t.Errorf("rows = %q, want the result and no quit", body)
	}
	if sd := shutDownOnRecord(t, root); sd != nil {
		t.Errorf("shutdown on record = %+v, want none", sd)
	}
}

// A project shutdown of only the targets closes the project's window and
// keeps the workspace that holds the agent.
func TestProjectShutdownOfOnlyTargetsKeepsTheAgent(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	wm.Add("editor", "code-project-01")
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "alt+q")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "project-01") || strings.Contains(body, "project-00") {
		t.Fatalf("rows = %q, want the one project with something open", body)
	}
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if plan := pane(m); !strings.Contains(plan, "editor") || strings.Contains(plan, "home") {
		t.Errorf("pane = %q, want the editor alone in the plan", plan)
	}

	m, cmd = press(m, "enter")
	run(m, cmd)
	if len(wm.Closed) != 1 {
		t.Errorf("window host closed %v, want the editor", wm.Closed)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 1 {
		t.Errorf("instances = %+v, want the workspace kept", left)
	}
}

// A plan that answers after the choice changed is not the plan confirmed: a
// full shutdown's survey that arrives on a project's confirm step is dropped.
func TestAStalePlanIsNotShownForAnotherChoice(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "alt+q")
	m, full := press(m, "enter")
	m, _ = press(m, "esc")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	m, _ = press(m, "down")
	m, targets := press(m, "enter")

	m = run(m, full)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "surveying") {
		t.Errorf("rows = %q, want the project's plan still awaited", body)
	}
	m = run(m, targets)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Nothing to close") {
		t.Errorf("rows = %q, want the project's targets plan, which closes nothing", body)
	}
}

// Esc walks the wizard back one step at a time, and off it from the first.
func TestEscWalksTheShutdownWizardBack(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "alt+q")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")
	if sub := lines(m)[2]; !strings.Contains(sub, "What of project-01?") {
		t.Fatalf("subtitle = %q, want the scope step", sub)
	}
	m, _ = press(m, "esc")
	if sub := lines(m)[2]; !strings.Contains(sub, "Which project?") {
		t.Errorf("subtitle = %q, want the project step", sub)
	}
	m, _ = press(m, "esc")
	if sub := lines(m)[2]; !strings.Contains(sub, "What to shut down?") {
		t.Errorf("subtitle = %q, want the first step", sub)
	}
	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "sessions alt+s") {
		t.Errorf("bar = %q, want the surface", bar)
	}
}

// A project's shutdown answer starts no survey: the timer's chain shows what
// closed, and a survey started here would be a second chain that never ends.
// It records no shutdown, and the surface stays up.
func TestAProjectShutdownStartsNoSecondSurveyAndStaysUp(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	m, _ = press(m, "alt+q")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	m, cmd = press(m, "enter")
	m, after := deliver(m, cmd)
	if after != nil {
		t.Error("the shutdown's answer returned a command, want none: the timer's survey shows the result")
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shutdown complete: closed 1. It is safe to leave.") || strings.Contains(body, "revier quits") {
		t.Errorf("rows = %q, want the result and no quit", body)
	}
	if sd := shutDownOnRecord(t, root); sd != nil {
		t.Errorf("shutdown on record = %+v, want none for one project", sd)
	}
	if m, _ = press(m, "esc"); !strings.Contains(barLine(m), "sessions alt+s") {
		t.Errorf("bar = %q after Esc on a project's result, want the surface back", barLine(m))
	}
}

// An agent that starts a turn while the confirm is on screen is not closed
// on a plan that showed it idle: the confirm closes nothing, saves nothing,
// and shows the plan again with the agent busy.
func TestConfirmClosesNothingWhenAnAgentTurnedBusy(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusIdle}
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)
	if body := strings.Join(rows(m), "\n"); strings.Contains(body, "busy agent") {
		t.Fatalf("rows = %q, want a plan with no busy agent", body)
	}

	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: "editing"}
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if left, _ := rt.Instances(t.Context()); len(left) != 1 {
		t.Errorf("instances = %+v, want the workspace still open", left)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %d, want no save for a shutdown that closed nothing", len(all))
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shut down anyway: 1 busy agent") {
		t.Errorf("rows = %q, want the plan again with the agent busy", body)
	}
	if plan := pane(m); !strings.Contains(plan, "Shutdown plan") || !strings.Contains(plan, "editing") {
		t.Errorf("pane = %q, want the plan with the agent's current activity", plan)
	}
	if foot := strings.Join(lines(m), "\n"); !strings.Contains(foot, "an agent turned busy since the plan was shown") {
		t.Errorf("screen = %q, want the reason nothing closed", foot)
	}
	// The cursor is off the close it refused, so the Enter that forces it is
	// aimed and not the second half of a double press.
	if row := rows(m)[1]; !strings.Contains(row, theme.Default().Glyphs.Cursor) || !strings.Contains(row, "Cancel") {
		t.Errorf("row under the cursor = %q, want Cancel", row)
	}

	m, _ = press(m, "up")
	m, cmd = press(m, "enter")
	run(m, cmd)
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want the plan shown busy to close on the next confirm", left)
	}
}

// The session saved before the close is the recheck's survey, not the one
// the plan was drawn from: a window the user closed by hand while the confirm
// was on screen is gone, and a restore must not open it again.
func TestTheSaveBeforeTheCloseRecordsWhatIsOpenAtTheConfirm(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusIdle}
	editor := wm.Add("editor", "code-project-00")
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)
	if plan := pane(m); !strings.Contains(plan, "project-00") {
		t.Fatalf("pane = %q, want project-00's editor in the plan", plan)
	}

	wm.Remove(editor)
	m, cmd := press(m, "enter")
	run(m, cmd)

	all, err := session.List(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("sessions = %+v, %v; want the one saved before the close", all, err)
	}
	if names := savedProjects(all[0]); !slices.Equal(names, []revier.ProjectName{"project-01"}) {
		t.Errorf("saved projects = %v, want project-01 alone: the editor was closed by hand", names)
	}
}

// A confirm that forces a busy agent saves from the same fresh survey as any
// other close: forcing says not to refuse, not to work from the listing the
// plan was drawn from.
func TestTheForcedConfirmSavesWhatIsOpenAtTheConfirm(t *testing.T) {
	_, wm, c, projects := world(t, 2)
	editor := wm.Add("editor", "code-project-00")
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shut down anyway: 1 busy agent") {
		t.Fatalf("rows = %q, want the busy agent named, so the confirm is the force", body)
	}

	wm.Remove(editor)
	m, cmd := press(m, "enter")
	run(m, cmd)

	all, err := session.List(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("sessions = %+v, %v; want the one saved before the forced close", all, err)
	}
	if names := savedProjects(all[0]); !slices.Equal(names, []revier.ProjectName{"project-01"}) {
		t.Errorf("saved projects = %v, want project-01 alone: the editor was closed by hand", names)
	}
}

// savedProjects names the projects a session recorded.
func savedProjects(s session.Session) []revier.ProjectName {
	var out []revier.ProjectName
	for _, p := range s.Projects {
		out = append(out, p.Name)
	}
	return out
}

// A host that stops answering between the plan and the confirm closes
// nothing of its own: a survey that lists no agent has not shown them idle.
// The result names each step it left and why, and nothing was saved, because
// a shutdown that closes nothing changes nothing to record.
func TestConfirmLeavesOpenWhatTheRecheckCannotRead(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusIdle}
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)

	rt.InstancesErr = errors.New("no server running")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	rt.InstancesErr = nil
	if left, _ := rt.Instances(t.Context()); len(left) != 1 {
		t.Errorf("instances = %+v, want the workspace still open", left)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %d, want no save for a shutdown that closed nothing", len(all))
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "closed 0, 1 still open") {
		t.Errorf("rows = %q, want the step left open", body)
	}
	if plan := pane(m); !strings.Contains(plan, "no server running") {
		t.Errorf("pane = %q, want the reason the step was left", plan)
	}
}

// A confirm whose agents are as the plan showed them closes the plan.
func TestConfirmWithUnchangedAgentsCloses(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusIdle}
	root := stateWith(t, nil)
	m := fullShutdownPlanned(t, c, projects, root)

	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shutdown complete: closed 1. It is safe to leave.") {
		t.Errorf("rows = %q, want the plan closed", body)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want none", left)
	}
}

// A confirm of a plan that already names a busy agent is forced, and is not
// checked again: the agent, busy still in another way, is closed.
func TestForcedConfirmClosesAnAgentStillBusy(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	m := fullShutdownPlanned(t, c, projects, stateWith(t, nil))
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shut down anyway: 1 busy agent") {
		t.Fatalf("rows = %q, want the forced confirm", body)
	}

	probeOf(c).State = revier.AgentState{Harness: "claude", Status: revier.StatusRunning, Activity: "editing"}
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Shutdown complete: closed 1. It is safe to leave.") {
		t.Errorf("rows = %q, want the plan closed", body)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want none", left)
	}
}
