package tui_test

import (
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// held is a host call a test holds until it lets it go: entered says the
// first call arrived, and release lets it return. Every later call passes.
type held struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newHeld() *held {
	return &held{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *held) hold() {
	h.once.Do(func() {
		close(h.entered)
		<-h.release
	})
}

// walkInBackground splits the batch a run returned into the walk, started
// on its own goroutine as the program starts it, and the wait on the walk's
// reports, which is the batch's last command.
func walkInBackground(t *testing.T, cmd tea.Cmd) (answer <-chan tea.Msg, reports tea.Cmd) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("the run returned %T, want the walk and the wait on its reports", cmd())
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- batch[0]() }()
	return out, batch[len(batch)-1]
}

// readReports feeds n reports of a walk to the model, and returns the wait
// for the next.
func readReports(t *testing.T, m tui.Model, wait tea.Cmd, n int) (tui.Model, tea.Cmd) {
	t.Helper()
	for range n {
		if wait == nil {
			t.Fatal("the model stopped waiting for the walk's reports")
		}
		m, wait = update(m, wait())
	}
	return m, wait
}

var ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

// A shutdown names the step it closes while it closes it. While it runs the
// surface takes no key and no click; ctrl+c cancels it rather than quitting,
// and the surface stays up to say what closed and what was not reached.
func TestAShutdownShowsEachStepHoldsTheInputAndCtrlCCancelsIt(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	wm.Add("editor", "code-project-00")
	editor := newHeld()
	wm.OnClose = func(revier.TargetRef) { editor.hold() }
	m := fullShutdownPlanned(t, c, projects, stateWith(t, nil))

	m, cmd := press(m, "enter")
	answer, wait := walkInBackground(t, cmd)
	<-editor.entered
	// The recheck, the save, and the editor's close begun.
	m, _ = readReports(t, m, wait, 3)
	if body := strings.Join(rows(m), "\n"); !strings.Contains(body, "closing 1 of 2: editor") {
		t.Errorf("rows = %q, want the step that runs named", body)
	}
	if p := pane(m); !strings.Contains(p, "Closing") || !strings.Contains(p, "closing") || !strings.Contains(p, "1 step to go") {
		t.Errorf("pane = %q, want the step that runs and the one to go", p)
	}

	for _, k := range []string{"esc", "enter", "alt+q", "x"} {
		next, out := press(m, k)
		if out != nil || !strings.Contains(barLine(next), "Shutdown") || !strings.Contains(pane(next), "Closing") {
			t.Errorf("%s while the shutdown runs: command %v, screen %q; want it held", k, out != nil, screen(next))
		}
	}
	if next, out := update(m, tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft}); out != nil || !strings.Contains(pane(next), "Closing") {
		t.Error("a click while the shutdown runs did something, want it held")
	}

	m, quit := send(m, ctrlC)
	if quit != nil {
		t.Error("ctrl+c while the shutdown runs returned a command, want the cancel alone and the surface up")
	}
	if foot := footer(m); !strings.Contains(foot, "cancelling") {
		t.Errorf("footer = %q, want the cancel said", foot)
	}
	close(editor.release)
	m, _ = update(m, <-answer)

	if len(rt.Closed) != 0 {
		t.Errorf("runtime closed %v, want project-01's home not reached after the cancel", rt.Closed)
	}
	body := strings.Join(rows(m), "\n")
	for _, want := range []string{"closed 1, 0 still open", "cancelled: 1 step not reached"} {
		if !strings.Contains(body, want) {
			t.Errorf("rows lack %q:\n%s", want, body)
		}
	}
	if p := pane(m); !strings.Contains(p, "Closed") || !strings.Contains(p, "editor") {
		t.Errorf("pane = %q, want the step that closed", p)
	}
	m, _ = press(m, "esc")
	if bar := barLine(m); !strings.Contains(bar, "shutdown alt+q") {
		t.Errorf("bar = %q, want the surface back once the shutdown ended", bar)
	}
	if _, out := send(m, ctrlC); out == nil {
		t.Error("ctrl+c with nothing running returned no command, want it to quit")
	}
}

// A restore shows each target as it walks it and keeps its screen: no key or
// click leaves it while it runs. ctrl+c cancels it before the next target,
// and the pane says what opened and that the rest was not.
func TestARestoreShowsEachStepHoldsTheInputAndCtrlCCancelsIt(t *testing.T) {
	rt, wm, c, projects := world(t, 2)
	root := stateWith(t, nil)
	saved(t, root, "work", 10, session.Project{Name: "project-00", Targets: []session.Target{{Name: "home"}, {Name: "editor"}}})
	home := newHeld()
	rt.OnOpen = func(revier.Realization) { home.hold() }
	m := resize(refreshed(t, c, projects, root, nil), 150, 30)
	m, _ = press(m, "alt+s")

	m, cmd := press(m, "enter")
	answer, wait := walkInBackground(t, cmd)
	if p := pane(m); !strings.Contains(p, "surveying what is open now") {
		t.Errorf("pane = %q, want the survey before the walk said", p)
	}
	<-home.entered
	m, _ = readReports(t, m, wait, 1)
	if p := pane(m); !strings.Contains(p, "Restoring") || !strings.Contains(p, "opening 1 of 2: home") {
		t.Errorf("pane = %q, want the target that opens named", p)
	}

	for _, k := range []string{"esc", "enter", "alt+s", "down"} {
		next, out := press(m, k)
		if out != nil || !strings.Contains(pane(next), "opening 1 of 2: home") {
			t.Errorf("%s while the restore runs: command %v, pane %q; want it held", k, out != nil, pane(next))
		}
	}
	if next, out := update(m, tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft}); out != nil || !strings.Contains(pane(next), "Restoring") {
		t.Error("a click while the restore runs did something, want it held")
	}

	m, quit := send(m, ctrlC)
	if quit != nil {
		t.Error("ctrl+c while the restore runs returned a command, want the cancel alone and the surface up")
	}
	close(home.release)
	m, _ = update(m, <-answer)

	if len(wm.Opened) != 0 {
		t.Errorf("window host opened %+v, want the editor not reached after the cancel", wm.Opened)
	}
	p := pane(m)
	for _, want := range []string{"Restored", "opened 1", "cancelled"} {
		if !strings.Contains(p, want) {
			t.Errorf("pane lacks %q:\n%s", want, p)
		}
	}
	if foot := footer(m); !strings.Contains(foot, "cancelled after 1 target") {
		t.Errorf("footer = %q, want the cancel said", foot)
	}
}
