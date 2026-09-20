package tui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// The shutdown button is on the bar with its key. A full shutdown plans every
// project, names the busy agent on the button that confirms, saves the
// session, closes everything, and stays up to say what came of it.
func TestAltQFullShutdownSavesThenClosesEverything(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the shutdown button and its key", bar)
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
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "closed 1, 0 still open") || !strings.Contains(body, "saved as session") {
		t.Errorf("rows = %q, want the result and the save", body)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want none", left)
	}
	if all, _ := session.List(root); len(all) != 1 {
		t.Errorf("sessions = %d, want the one saved before the close", len(all))
	}
	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the surface back after the result", bar)
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
	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the surface", bar)
	}
}

// A shutdown's answer starts no survey: the timer's chain shows what closed,
// and a survey started here would be a second chain that never ends.
func TestAShutdownStartsNoSecondSurvey(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	m, cmd = press(m, "enter")
	if _, after := m.Update(cmd()); after != nil {
		t.Error("the shutdown's answer returned a command, want none: the timer's survey shows the result")
	}
}

// probeOf is the world's one agent probe, whose state a test changes between
// the plan and the confirm.
func probeOf(c *core.Core) *hosttest.FakeProbe { return c.Probes[0].(*hosttest.FakeProbe) }

// fullShutdownPlanned opens the wizard on a full shutdown and shows its plan.
func fullShutdownPlanned(t *testing.T, c *core.Core, projects []core.Project, root string) tui.Model {
	t.Helper()
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	return run(m, cmd)
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

// A host that stops answering between the plan and the confirm closes
// nothing: a survey that lists no agent has not shown them idle.
func TestConfirmClosesNothingWhenTheRecheckCannotRead(t *testing.T) {
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
	if foot := strings.Join(lines(m), "\n"); !strings.Contains(foot, "could not be read again") || !strings.Contains(foot, "nothing closed") {
		t.Errorf("screen = %q, want the reason nothing closed", foot)
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
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "closed 1, 0 still open") {
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
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "closed 1, 0 still open") {
		t.Errorf("rows = %q, want the plan closed", body)
	}
	if left, _ := rt.Instances(t.Context()); len(left) != 0 {
		t.Errorf("instances = %+v, want none", left)
	}
}
