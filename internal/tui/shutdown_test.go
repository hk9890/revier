package tui_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/session"
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
