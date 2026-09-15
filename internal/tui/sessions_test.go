package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// saved writes a session into the state root, saved at the given minute.
func saved(t *testing.T, root, name string, minute int, projects ...session.Project) session.Session {
	t.Helper()
	s, _, err := session.Save(root, session.Session{
		Name: name, At: time.Date(2026, 9, 15, 18, minute, 0, 0, time.Local), Projects: projects,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// homeOf is a recorded project with its home target open.
func homeOf(name string) session.Project {
	return session.Project{Name: revier.ProjectName(name), Targets: []session.Target{{Name: "home"}}}
}

// The sessions button is on the bar with its key, and the screen it opens
// lists the saved sessions newest first, each with its name and counts.
func TestAltSListsTheSavedSessionsNewestFirst(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	older := saved(t, root, "before-reboot", 10, homeOf("project-00"), homeOf("project-01"))
	// Saved in the same second as the older one, so its id carries a suffix.
	newer := saved(t, root, "", 10, homeOf("project-00"))
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)

	if bar := barLine(m); !strings.Contains(bar, "sessions") || !strings.Contains(bar, "alt+s") {
		t.Errorf("bar = %q, want the sessions button and its key", bar)
	}
	m, _ = press(m, "alt+s")
	if bar := barLine(m); !strings.Contains(bar, "Sessions") || !strings.Contains(bar, "save alt+s") {
		t.Errorf("top line = %q, want the screen's name and its save button", bar)
	}
	if rule := ruleLine(m); !strings.Contains(rule, "2 sessions") {
		t.Errorf("rule = %q, want 2 sessions", rule)
	}
	body := rows(m)
	if !strings.Contains(body[0], newer.ID) || !strings.Contains(body[1], older.ID) {
		t.Fatalf("rows = %q, want the newer session first", body)
	}
	if !strings.Contains(body[1], "before-reboot") || strings.Contains(body[1], "projects") {
		t.Errorf("row = %q, want the name, and the counts left to the pane", body[1])
	}
	// The name is a column: it starts in one place on every row, a dash for
	// the session with none.
	column := func(row, name string) int {
		at := strings.Index(row, name)
		if at < 0 {
			t.Fatalf("row = %q, want %q in it", row, name)
		}
		return lipgloss.Width(row[:at])
	}
	if a, b := column(body[0], "—"), column(body[1], "before-reboot"); a != b {
		t.Errorf("rows = %q, want the names aligned, at %d and %d", body[:2], a, b)
	}
	if holds := pane(m); !strings.Contains(holds, "1 project · 1 target · 0 agents") {
		t.Errorf("pane = %q, want the counts of the session under the cursor", holds)
	}

	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "new") {
		t.Errorf("top line = %q after esc, want the surface's bar", bar)
	}
}

// With nothing saved the screen says how to save.
func TestTheSessionsScreenWithNoneSaysHowToSave(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	m, _ = press(m, "alt+s")
	if view := m.View(); !strings.Contains(view, "No saved sessions") {
		t.Errorf("screen = %q, want it to say there are none", view)
	}
}

// The pane says what restoring the session under the cursor would do here
// now, project by project: open what is down and leave what is up, and for
// each target the agents it held, with their conversation, their directory
// and what becomes of them.
func TestTheSessionPaneShowsTheRestorePlan(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	down, up := homeOf("project-00"), homeOf("project-01")
	down.Targets[0].Agents = []session.Agent{{Harness: "claude", Session: "abc12345-6789", Dir: "/w/feature"}}
	up.Targets[0].Agents = []session.Agent{{Harness: "claude"}}
	saved(t, root, "work", 10, down, up)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")

	body := pane(m)
	lines := strings.Split(body, "\n")
	// at is the index of the first line from `from` with every part in it.
	at := func(from int, parts ...string) int {
		for i := from; i < len(lines); i++ {
			all := true
			for _, p := range parts {
				all = all && strings.Contains(lines[i], p)
			}
			if all {
				return i
			}
		}
		t.Fatalf("no line from %d holds %q:\n%s", from, parts, body)
		return -1
	}
	g := theme.Default().Glyphs
	down0 := at(0, "project-00")
	home0 := at(down0, g.Start+" open", "home", g.Stopped+" stopped")
	// The world's home declares no agent panel, so the agent cannot start in it.
	agent0 := at(home0, g.Skip+" skip", "claude", "stopped", "no agent panel to start it in")
	up1 := at(agent0, "project-01")
	home1 := at(up1, g.Keep+" keep", "home", g.Running+" running")
	// The agent kept is the one running, and says what it is doing.
	at(home1, g.Keep+" keep", "claude", "needs you", "needs a decision")
	if len(rt.Opened) != 0 {
		t.Errorf("Opened = %v, want the plan to open nothing", rt.Opened)
	}
}

// An agent that resumes is not running yet, so its row says the conversation
// it resumes, and the directory where that is not the project's own.
func TestAPlannedResumeShowsItsConversation(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.NewResumableProbe("claude", "claude")}}
	worktree := t.TempDir()
	projects, err := core.Prepare([]revier.Project{{Name: "work", Path: "/p/work", Targets: []revier.Target{{
		Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "work", Match: revier.Match{Title: "^work$"},
			Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	root := stateWith(t, nil)
	work := homeOf("work")
	work.Targets[0].Agents = []session.Agent{{Harness: "claude", Session: "3f2a9c1e-77b0", Dir: worktree}}
	saved(t, root, "", 10, work)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")

	g := theme.Default().Glyphs
	body := pane(m)
	want := g.Start + " resume"
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, want) && strings.Contains(line, "claude") {
			// The directory wraps under the text, as an activity line does.
			flat := strings.Join(strings.Fields(body), "")
			if !strings.Contains(line, "3f2a9c1e-77b0 in") || !strings.Contains(flat, strings.ReplaceAll(worktree, " ", "")) {
				t.Errorf("agent row = %q, want its conversation and its directory", line)
			}
			return
		}
	}
	t.Errorf("no %q row for the agent:\n%s", want, body)
}

// Enter restores the session: the target that is down opens, the one that is
// up is left, the landing is written to state, and the pane says what came of
// each.
func TestEnterRestoresTheSession(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	s := saved(t, root, "work", 10, homeOf("project-00"), homeOf("project-01"))
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")

	m, cmd := press(m, "enter")
	if foot := footer(m); !strings.Contains(foot, "restoring "+s.ID) {
		t.Errorf("footer = %q, want the restore said as running", foot)
	}
	if again, _ := press(m, "enter"); !strings.Contains(footer(again), "still running") {
		t.Errorf("footer = %q, want a second restore refused while one runs", footer(again))
	}
	// The restore is a batch: the walk, and the wait that takes its writes
	// into the surface. The walk runs first, as its writes come before the
	// wait can read them.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("enter returned %T, want the walk and the wait on its writes", cmd())
	}
	for _, each := range batch {
		m = run(m, each)
	}

	if len(rt.Opened) != 1 || rt.Opened[0].Name != "session:project-00" {
		t.Errorf("Opened = %+v, want project-00's workspace alone", rt.Opened)
	}
	body := pane(m)
	for _, want := range []string{"Restored", "opened", "kept", "opened 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("pane lacks %q:\n%s", want, body)
		}
	}
	st, err := state.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.Bound["project-00"]["home"].IsZero() {
		t.Errorf("state bound = %+v, want project-00's home landed", st.Bound)
	}
}

// The save button names a session and saves what is open now; the new session
// is a row under the cursor, and the pane says what the save could not record.
func TestTheSaveButtonSavesWhatIsOpenUnderAName(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")
	m, _ = press(m, "alt+s")
	if bar := barLine(m); !strings.Contains(bar, "Save the projects open now") {
		t.Fatalf("top line = %q, want the name step", bar)
	}
	m = typeInto(m, "before")
	m, cmd := press(m, "enter")
	m = run(m, cmd)

	all, err := session.List(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("sessions = %+v, %v, want the one saved", all, err)
	}
	if all[0].Name != "before" || len(all[0].Projects) != 1 || all[0].Projects[0].Name != "project-01" {
		t.Errorf("saved %+v, want project-01 under the name before", all[0])
	}
	if _, err := os.Stat(filepath.Join(session.Dir(root), all[0].ID+".toml")); err != nil {
		t.Errorf("session file: %v", err)
	}
	if row := rows(m)[0]; !strings.Contains(row, "before") {
		t.Errorf("row = %q, want the new session", row)
	}
	// The world's agent has a probe that cannot name its conversation.
	if body := pane(m); !strings.Contains(body, "1 agent without a conversation id") {
		t.Errorf("pane = %q, want the gap said", body)
	}
}

// A save with nothing open writes nothing and says so.
func TestASaveWithNothingOpenWritesNothing(t *testing.T) {
	rt, _, c, projects := world(t, 2)
	insts, _ := rt.Instances(t.Context())
	for _, inst := range insts {
		rt.Remove(inst.Ref)
	}
	root := stateWith(t, nil)
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")
	m, _ = press(m, "alt+s")
	m, cmd := press(m, "enter")
	m = run(m, cmd)

	if foot := footer(m); !strings.Contains(foot, "nothing is open") {
		t.Errorf("footer = %q, want it to say nothing is open", foot)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want none written", all)
	}
}

// One click on the save button is the name step, as its key is.
func TestOneClickOnTheSaveButtonAsksForTheName(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	m, _ = press(m, "alt+s")

	line := barLine(m)
	mr, _ := margins(m)
	x := strings.Index(line, "save") + 1 // the line keeps its margin
	m = clickAt(m, x, mr)
	if bar := barLine(m); !strings.Contains(bar, "Save the projects open now") {
		t.Errorf("top line = %q after a click on save, want the name step", bar)
	}
}
