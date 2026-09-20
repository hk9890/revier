package tui_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// deliver runs a command and feeds its message back, and returns the command
// the model answers with: a close that needs no confirm plans, then runs.
func deliver(m tui.Model, cmd tea.Cmd) (tui.Model, tea.Cmd) {
	next, out := m.Update(cmd())
	return next.(tui.Model), out
}

// del on a project asks first, naming the busy agent, and then closes it as
// a project shutdown does: the session is saved, and the surface is back.
func TestDelClosesAProjectAfterAConfirm(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	m, cmd := press(m, "delete")
	m = run(m, cmd)
	if sub := lines(m)[2]; !strings.Contains(sub, "Close project-01?") {
		t.Fatalf("subtitle = %q, want the confirm", sub)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Close anyway: 1 busy agent") {
		t.Errorf("rows = %q, want the busy agent named", body)
	}

	m, cmd = press(m, "enter")
	m = run(m, cmd)
	if len(rt.Closed) != 1 {
		t.Errorf("runtime closed %v, want the workspace", rt.Closed)
	}
	if all, _ := session.List(root); len(all) != 1 {
		t.Errorf("sessions = %d, want the one saved before the close", len(all))
	}
	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the surface back", bar)
	}
}

// del on a target that holds no agent closes it at once, and nothing else.
func TestDelClosesATargetWithoutAnAgentAtOnce(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	editor := wm.Add("editor", "code-project-01")
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	m, _ = press(m, "tab")
	m, _ = press(m, "down")
	m, cmd := press(m, "delete")
	m, cmd = deliver(m, cmd)
	if cmd == nil {
		t.Fatalf("screen = %q, want the close to run with no confirm", screen(m))
	}
	m, _ = deliver(m, cmd)
	if !slices.Equal(wm.Closed, []revier.TargetRef{editor}) || len(rt.Closed) != 0 {
		t.Errorf("closed %v and %v, want the editor alone", wm.Closed, rt.Closed)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %d, want none saved for one target", len(all))
	}
	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the surface as it was", bar)
	}
}

// del on a target that holds an agent asks first: the conversation ends with
// it.
func TestDelOnATargetWithAnAgentAsks(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "tab")
	m, cmd := press(m, "delete")
	m = run(m, cmd)
	if sub := lines(m)[2]; !strings.Contains(sub, "Close home of project-01?") {
		t.Errorf("subtitle = %q, want the confirm for the target", sub)
	}
}

// del on an agent closes its tab and keeps the workspace. A busy agent is
// asked about first, an idle one closes at once.
func TestDelClosesAnAgentsTab(t *testing.T) {
	for _, status := range []revier.Status{revier.StatusAttention, revier.StatusIdle} {
		rt, _, c, projects := world(t, 2)
		c.Probes[0].(*hosttest.FakeProbe).State.Status = status
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

		m, _ = press(m, "tab")
		m, _ = press(m, "tab")
		m, cmd := press(m, "delete")
		m, cmd = deliver(m, cmd)
		if status == revier.StatusAttention {
			if sub := lines(m)[2]; !strings.Contains(sub, "Close claude of project-01?") {
				t.Fatalf("subtitle = %q, want the confirm for the busy agent", sub)
			}
			m, cmd = press(m, "enter")
		}
		deliver(m, cmd)
		if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"1"}) || len(rt.Closed) != 0 {
			t.Errorf("status %v: closed %v, panels %v; want the agent's panel alone", status, rt.Closed, rt.ClosedPanels)
		}
	}
}

// An agent that starts a turn between the press and the close is not ended
// by a del that would have closed at once: the close is refused, the plan is
// shown with the agent busy, and the next press closes it (decisions.md D99).
func TestDelOnAnAgentThatTurnedBusyAsksFirst(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	probeOf(c).State.Status = revier.StatusIdle
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "tab")
	m, _ = press(m, "tab")
	m, cmd := press(m, "delete")
	m, cmd = deliver(m, cmd)
	if cmd == nil {
		t.Fatalf("screen = %q, want the close to run with no confirm", screen(m))
	}
	probeOf(c).State.Status = revier.StatusRunning
	m, _ = deliver(m, cmd)
	if len(rt.ClosedPanels) != 0 {
		t.Fatalf("panels closed = %v, want none: the agent turned busy", rt.ClosedPanels)
	}
	if sub := lines(m)[2]; !strings.Contains(sub, "Close claude of project-01?") {
		t.Errorf("subtitle = %q, want the confirm for the agent that turned busy", sub)
	}
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "Close anyway: 1 busy agent") {
		t.Errorf("rows = %q, want the busy agent named", body)
	}

	m, _ = press(m, "up")
	m, cmd = press(m, "enter")
	deliver(m, cmd)
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"1"}) {
		t.Errorf("panels closed = %v, want the agent's panel on the second press", rt.ClosedPanels)
	}
}

// del with text right of the query's cursor is the query's forward delete.
func TestDelInsideTheQueryDeletesACharacter(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "p")
	m, _ = press(m, "r")
	m, _ = press(m, "left")
	m, cmd := press(m, "delete")
	if cmd != nil {
		t.Errorf("del inside the query started a close")
	}
	if q := query(m); !strings.Contains(q, "❯ p ") {
		t.Errorf("query = %q, want the r deleted", q)
	}
}

// del on what is not open says so and closes nothing.
func TestDelOnWhatIsNotOpenSaysSo(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "down")
	m, cmd := press(m, "delete")
	m = run(m, cmd)
	if f := footer(m); !strings.Contains(f, "project-00: nothing of it is open here") {
		t.Errorf("footer = %q, want nothing to close", f)
	}
	if len(rt.Closed) != 0 {
		t.Errorf("runtime closed %v, want nothing", rt.Closed)
	}
}

// alt+del on an agent deletes nothing: an agent is in no file.
func TestAltDelOnAnAgentDeletesNothing(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)

	m, _ = press(m, "tab")
	m, _ = press(m, "tab")
	m, _ = press(m, "alt+delete")
	if f := footer(m); !strings.Contains(f, "an agent is in no file") {
		t.Errorf("footer = %q, want the refusal", f)
	}
}

// alt+del on a target of the project's own file asks, and y removes its
// entry from the file.
func TestAltDelRemovesAProjectsOwnTarget(t *testing.T) {
	m, file := notesProject(t, hosttest.NewRuntime("rt"))

	m, _ = press(m, "tab")
	m, _ = press(m, "down")
	m, _ = press(m, "alt+delete")
	if f := footer(m); !strings.Contains(f, `delete target "notes" of alpha?`) || !strings.Contains(f, "alpha.toml") {
		t.Fatalf("footer = %q, want the question naming the file", f)
	}
	m, _ = press(m, "y")
	if body := fileText(t, file); strings.Contains(body, "notes") || !strings.Contains(body, `name = "home"`) {
		t.Errorf("file = %q, want notes gone and home kept; footer %q", body, footer(m))
	}
}

// alt+del on a running target of the project's own file closes it first,
// after a confirm, and then removes its entry.
func TestAltDelClosesARunningTargetFirst(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("notes", "less")
	m, file := notesProject(t, rt)

	m, _ = press(m, "tab")
	m, _ = press(m, "down")
	m, cmd := press(m, "alt+delete")
	if cmd == nil {
		t.Fatalf("footer = %q, want the close planned", footer(m))
	}
	m = run(m, cmd)
	if sub := lines(m)[2]; !strings.Contains(sub, "Close and delete notes of alpha?") {
		t.Fatalf("subtitle = %q, want the confirm naming the delete", sub)
	}
	m, cmd = press(m, "enter")
	m = run(m, cmd)
	if len(rt.Closed) != 1 {
		t.Errorf("runtime closed %v, want notes", rt.Closed)
	}
	if body := fileText(t, file); strings.Contains(body, "notes") {
		t.Errorf("file = %q, want notes gone; footer %q", body, footer(m))
	}
}

// notesProject is the surface over one project file, alpha, with a home and
// a notes target of its own. It returns the file too.
func notesProject(t *testing.T, rt *hosttest.FakeRuntime) (tui.Model, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "alpha.toml")
	body := strings.ReplaceAll(fileProject, "%PATH%", t.TempDir()) + `
[[target]]
name = "notes"
  [target.runtime]
  name = "notes"
  launch = ["less"]
  match = { title = "^notes$" }
`
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(body, "%GIT%", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	projects, err := config.LoadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resize(refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil), 120, 20), file
}

// alt+del on a target of config.toml is refused: it is every project's.
func TestAltDelRefusesASharedTarget(t *testing.T) {
	m, file, _ := projectSurface(t, demoProject, hosttest.NewRuntime("rt"))

	for _, moves := range []int{0, 1} {
		m, _ = press(m, "tab")
		for range moves {
			m, _ = press(m, "down")
		}
		m, _ = press(m, "alt+delete")
		if f := footer(m); !strings.Contains(f, "config.toml's") {
			t.Errorf("row %d: footer = %q, want the refusal", moves, f)
		}
		m, _ = press(m, "esc")
	}
	if body := fileText(t, file); body != demoProject {
		t.Errorf("file = %q, want it unchanged", body)
	}
}

// alt+del on a link unlinks it: the question says the host is not touched,
// and y removes the link's file alone.
func TestAltDelOnALinkUnlinks(t *testing.T) {
	projects := remoteOnDisk(t, "alpha")
	m, _, _ := linkWorld(t, projects, "alpha")
	m = resize(m, 200, 20)

	m, _ = press(m, "alt+delete")
	if f := footer(m); !strings.Contains(f, "unlink alpha?") || !strings.Contains(f, "nothing on buildbox changes") {
		t.Fatalf("footer = %q, want the unlink question", f)
	}
	press(m, "y")
	if _, err := os.Stat(projects[0].File); !os.IsNotExist(err) {
		t.Errorf("the link's file is still there: %v", err)
	}
}

// alt+del on the running home target is refused before anything closes: the
// file cannot lose its home, and the close would end the workspace for a
// delete that then does not happen.
func TestAltDelRefusesTheHomeTargetBeforeClosingIt(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:alpha", "sh")
	m, file := notesProject(t, rt)
	before := fileText(t, file)

	m, _ = press(m, "tab")
	m, cmd := press(m, "alt+delete")
	if cmd != nil {
		t.Fatalf("footer = %q, want the refusal and no close planned", footer(m))
	}
	if f := footer(m); !strings.Contains(f, "home") {
		t.Errorf("footer = %q, want the refusal naming the missing home", f)
	}
	if len(rt.Closed) != 0 || fileText(t, file) != before {
		t.Errorf("closed %v, file changed %v; want neither", rt.Closed, fileText(t, file) != before)
	}
}

// alt+del on a running project waits for a host that could not list: what
// that host holds may be open, and no close reaches it (decisions.md D89).
func TestAltDelWaitsForAHostThatCouldNotList(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	wm.InstancesErr = errors.New("went away")
	m = survey(m)

	m, cmd := press(m, "alt+delete")
	if f := footer(m); cmd != nil || !strings.Contains(f, "wait for the host before deleting") {
		t.Fatalf("footer = %q, want the refusal and no close planned", f)
	}
	if len(rt.Closed) != 0 {
		t.Errorf("runtime closed %v, want nothing", rt.Closed)
	}
}

// alt+del on a target that closed since the last survey has nothing to
// close: it asks as for a target that is not open, and y removes the entry.
func TestAltDelOnATargetClosedSinceAsks(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	notes := rt.Add("notes", "less")
	m, file := notesProject(t, rt)
	rt.Remove(notes)

	m, _ = press(m, "tab")
	m, _ = press(m, "down")
	m, cmd := press(m, "alt+delete")
	m = run(m, cmd)
	if f := footer(m); !strings.Contains(f, `delete target "notes" of alpha?`) {
		t.Fatalf("footer = %q, want the question", f)
	}
	m, _ = press(survey(m), "y")
	if body := fileText(t, file); strings.Contains(body, "notes") {
		t.Errorf("file = %q, want notes gone; footer %q", body, footer(m))
	}
}

// A shared target config.toml refuses reaches no project, so it is in no
// project's targets either: alt+del on a target of the project's own file
// asks its question, rather than reading the file against a shared list the
// loader already threw away (decisions.md D85).
func TestAltDelOnAnOwnTargetBesideARefusedSharedTarget(t *testing.T) {
	refused := sharedTargets + "\n[[target]]\nname = \"web\"\nhome = \"yes\"\n  [target.window]\n  launch = [\"firefox\"]\n  match = { class = \"^firefox$\" }\n"
	own := demoProject + "\n[[target]]\nname = \"notes\"\n  [target.runtime]\n  name = \"notes\"\n  launch = [\"less\"]\n  match = { title = \"^notes$\" }\n"
	start, file, _ := projectSurfaceOver(t, refused, own, hosttest.NewRuntime("rt"))

	// Which pane row notes is on depends on the shared targets beside it, so
	// each row is tried from the surface as it opens.
	var asked tui.Model
	found := false
	for row := range 6 {
		m, _ := press(start, "tab")
		for range row {
			m, _ = press(m, "down")
		}
		if m, _ = press(m, "alt+delete"); strings.Contains(footer(m), `delete target "notes"`) {
			asked, found = m, true
			break
		}
	}
	if !found {
		t.Fatal("no pane row offered to delete the project's own notes target")
	}
	m, _ := press(asked, "y")
	if body := fileText(t, file); strings.Contains(body, "notes") {
		t.Errorf("file = %q, want notes gone; footer %q", body, footer(m))
	}
}
