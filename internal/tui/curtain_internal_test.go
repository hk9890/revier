package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/theme"
)

// curtainOn is a curtain on a real socket, sized as a monitor, and the
// server at the other end. The runtime directory is the test's own and short
// enough for a socket path.
func curtainOn(t *testing.T, main bool) (curtainModel, *curtain.Server) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	s, err := curtain.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c, err := curtain.Dial(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	m := NewCurtain(c, main, theme.Default()).(curtainModel)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(curtainModel), s
}

// shown is the curtain after it took st as the surface sent it.
func shown(t *testing.T, m curtainModel, s *curtain.Server, st curtain.State) curtainModel {
	t.Helper()
	s.Send(st)
	next, _ := m.Update(m.waitState()())
	return next.(curtainModel)
}

// screen is the curtain's text, without its colours and margins.
func screen(m curtainModel) string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// keyed is the curtain after a key, and the command it answered with.
func keyed(m curtainModel, k tea.KeyPressMsg) (curtainModel, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(curtainModel), cmd
}

var (
	ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
	tab   = tea.KeyPressMsg{Code: tea.KeyTab}
)

// sent runs the command a press answered with and returns what reached the
// surface.
func sent(t *testing.T, s *curtain.Server, cmd tea.Cmd) curtain.Command {
	t.Helper()
	if cmd == nil {
		t.Fatal("the press sent nothing")
	}
	cmd()
	select {
	case got := <-s.Commands():
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no command reached the surface")
	}
	return ""
}

// halfway is a restore with one project done, one opening and one not
// begun.
var halfway = curtain.State{
	Mode: "restore", Line: "opening 3 of 5: editor", N: 2, Of: 5,
	Rows: []curtain.Row{
		{Project: "alpha", Label: "home", State: curtain.RowDone},
		{Project: "alpha", Label: "editor", State: curtain.RowDone},
		{Project: "bravo", Label: "home", State: curtain.RowDone},
		{Project: "bravo", Label: "editor", State: curtain.RowRunning},
		{Project: "charlie", Label: "home", State: curtain.RowWaiting},
	},
}

// ended is halfway ended with a step that failed and one not reached.
func ended() curtain.State {
	st := halfway
	st.Rows = append([]curtain.Row(nil), halfway.Rows...)
	st.Rows[3] = curtain.Row{Project: "bravo", Label: "editor", State: curtain.RowFailed, Note: "the window did not appear"}
	st.Rows[4].State = curtain.RowSkipped
	st.N = 4
	st.End = &curtain.End{Title: "Restore finished with problems", Summary: "3 opened · 1 failed · 1 not reached"}
	return st
}

// The main curtain lists the steps by project, and the note of the one that
// runs says what runs now.
func TestTheMainCurtainListsTheStepsByProject(t *testing.T) {
	m, s := curtainOn(t, true)
	got := screen(shown(t, m, s, halfway))
	for _, want := range []string{"Restoring your session", "2 / 5", "alpha", "bravo", "charlie", "opening 3 of 5: editor …", "Cancel", "ctrl+c cancel · esc hide · tab layout"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "bravo") && !strings.Contains(l, "opening 3 of 5") {
			t.Errorf("the line of the project that runs lacks what runs: %q", l)
		}
	}
}

// Tab turns the main curtain to one line of what runs, and back.
func TestTabTurnsTheMainCurtainCompact(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, halfway)
	m, _ = keyed(m, tab)
	got := screen(m)
	if strings.Contains(got, "charlie") || !strings.Contains(got, "opening 3 of 5: editor …") {
		t.Errorf("compact screen = \n%s\nwant what runs and no project list", got)
	}
	m, _ = keyed(m, tab)
	if !strings.Contains(screen(m), "charlie") {
		t.Errorf("a second tab left the list off:\n%s", screen(m))
	}
}

// ctrl+c cancels, and the curtain says it cancels at once; a second ctrl+c
// quits the surface.
func TestCtrlCCancelsThenQuits(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, halfway)
	m, cmd := keyed(m, ctrlC)
	if got := sent(t, s, cmd); got != curtain.Cancel {
		t.Errorf("first ctrl+c sent %q, want cancel", got)
	}
	got := screen(m)
	for _, want := range []string{"Restoring your session · cancelling", "Cancelling…", "stops after the step that runs · ctrl+c again quits now"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	_, cmd = keyed(m, ctrlC)
	if got := sent(t, s, cmd); got != curtain.Quit {
		t.Errorf("second ctrl+c sent %q, want quit", got)
	}
}

// A cancel the surface took from another curtain shows here too, and the
// next ctrl+c here quits.
func TestACancelTheSurfaceSaysShows(t *testing.T) {
	m, s := curtainOn(t, false)
	st := halfway
	st.Cancelling = true
	m = shown(t, m, s, st)
	if got := screen(m); !strings.Contains(got, "cancelling") {
		t.Errorf("screen = \n%s\nwant it cancelling", got)
	}
	_, cmd := keyed(m, ctrlC)
	if got := sent(t, s, cmd); got != curtain.Quit {
		t.Errorf("ctrl+c while cancelling sent %q, want quit", got)
	}
}

// A click on Cancel cancels; a click beside it does nothing.
func TestAClickOnCancelCancels(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, halfway)
	b := m.layout().button
	if b.row < 0 {
		t.Fatal("the running curtain has no button")
	}
	if _, cmd := m.Update(tea.MouseClickMsg{X: b.to, Y: b.row, Button: tea.MouseLeft}); cmd != nil {
		t.Error("a click beside the button sent a command")
	}
	next, cmd := m.Update(tea.MouseClickMsg{X: b.from, Y: b.row, Button: tea.MouseLeft})
	if got := sent(t, s, cmd); got != curtain.Cancel {
		t.Errorf("click sent %q, want cancel", got)
	}
	if !strings.Contains(screen(next.(curtainModel)), "Cancelling…") {
		t.Errorf("screen after the click:\n%s", screen(next.(curtainModel)))
	}
}

// Esc hides the curtains while the walk goes on.
func TestEscHidesTheCurtain(t *testing.T) {
	for _, main := range []bool{true, false} {
		m, s := curtainOn(t, main)
		m = shown(t, m, s, halfway)
		if _, cmd := keyed(m, esc); sent(t, s, cmd) != curtain.Hide {
			t.Errorf("main=%v: esc sent no hide", main)
		}
	}
}

// A walk that failed leaves the summary: each step with its note, and a
// Close that Enter, Esc and a click press.
func TestAFailedWalkShowsTheSummaryAndClose(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, ended())
	got := screen(m)
	for _, want := range []string{"Restore finished with problems", "3 opened · 1 failed · 1 not reached", "editor: the window did not appear", "not reached", "Close", "enter or esc close"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	for _, k := range []tea.KeyPressMsg{enter, esc} {
		if _, cmd := keyed(m, k); sent(t, s, cmd) != curtain.Close {
			t.Errorf("%s on the summary sent no close", k)
		}
	}
	b := m.layout().button
	if _, cmd := m.Update(tea.MouseClickMsg{X: b.from, Y: b.row, Button: tea.MouseLeft}); sent(t, s, cmd) != curtain.Close {
		t.Error("a click on Close sent no close")
	}
	m, _ = keyed(m, tab)
	if got := screen(m); !strings.Contains(got, "bravo editor: the window did not appear") {
		t.Errorf("compact summary lacks the failed step:\n%s", got)
	}
}

// A walk that succeeded says so and nothing else: the surface lifts the
// curtain.
func TestASuccessShowsTheDoneTitleAlone(t *testing.T) {
	m, s := curtainOn(t, true)
	st := halfway
	st.N, st.Rows = 5, nil
	st.End = &curtain.End{OK: true, Title: "Restore done", Summary: "5 opened"}
	m = shown(t, m, s, st)
	got := screen(m)
	if !strings.Contains(got, "Restore done") || strings.Contains(got, "Cancel") || strings.Contains(got, "Close") {
		t.Errorf("screen = \n%s\nwant the done title and no button", got)
	}
	for _, k := range []tea.KeyPressMsg{enter, esc, ctrlC} {
		if _, cmd := keyed(m, k); cmd != nil {
			t.Errorf("%s after a success sent a command", k)
		}
	}
}

// Another monitor's curtain shows the title and the bar, and sends the
// reader to the main monitor.
func TestASecondaryCurtainShowsTheBarAlone(t *testing.T) {
	m, s := curtainOn(t, false)
	got := screen(shown(t, m, s, halfway))
	if !strings.Contains(got, "Restoring your session") || !strings.Contains(got, "2 / 5") || !strings.Contains(got, "details on the main monitor") {
		t.Errorf("screen = \n%s\nwant the title, the bar and the pointer to the main monitor", got)
	}
	if strings.Contains(got, "alpha") || strings.Contains(got, "Cancel") {
		t.Errorf("screen = \n%s\nwant no steps and no button", got)
	}
}

// The curtain quits as soon as its socket ends.
func TestTheCurtainQuitsWhenTheSocketEnds(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, halfway)
	s.Close()
	_, cmd := m.Update(m.waitState()())
	if cmd == nil {
		t.Fatal("the socket's end returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the socket's end did not quit the curtain")
	}
}

// A state that changes the form of the block, as the summary does, clears
// the screen before it is drawn; one that keeps the form does not.
func TestAStateThatChangesTheFormClearsTheScreen(t *testing.T) {
	m, s := curtainOn(t, true)
	m = shown(t, m, s, halfway)
	for _, tc := range []struct {
		name  string
		st    curtain.State
		clear bool
	}{
		{"same form", halfway, false},
		{"summary", ended(), true},
	} {
		s.Send(tc.st)
		next, cmd := m.Update(m.waitState()())
		m = next.(curtainModel)
		// A state waits on the socket, so the wait the answer holds returns.
		s.Send(tc.st)
		_, batch := cmd().(tea.BatchMsg)
		if batch != tc.clear {
			t.Errorf("%s: clears the screen = %v, want %v", tc.name, batch, tc.clear)
		}
		if batch {
			m = shown(t, m, s, tc.st)
		}
	}
}
