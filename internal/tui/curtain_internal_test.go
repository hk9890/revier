package tui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/curtain"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
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

// The main curtain lists the steps by project, and one line under the list
// says what runs now.
func TestTheMainCurtainListsTheStepsByProject(t *testing.T) {
	m, s := curtainOn(t, true)
	got := screen(shown(t, m, s, halfway))
	for _, want := range []string{"Restoring your session", "2 / 5", "alpha", "bravo", "charlie", "opening 3 of 5: editor …", "Cancel", "ctrl+c cancel · esc hide · tab layout"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "opening 3 of 5") < strings.Index(got, "charlie") {
		t.Errorf("what runs is not under the list:\n%s", got)
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

// noCurtain launches nothing: a run a test feeds by hand opens no curtain.
func noCurtain(revier.Monitor, string, bool) []string { return nil }

// walkReports is every report of a restore or a shutdown of 12 projects with
// those targets, in which some steps fail or are left open; and the run that
// takes them.
func walkReports(mode string, targets []revier.TargetName) (*curtainRun, []core.Progress) {
	broke := errors.New("the window did not appear in the time that the launch waits for it, so nothing is bound to the target")
	var restores []core.RestoreStep
	var closes []core.CloseStep
	for i := range 12 {
		p := revier.ProjectName(fmt.Sprintf("project-%02d", i))
		for _, target := range targets {
			restores = append(restores, core.RestoreStep{Project: p, Target: target, Action: core.RestoreLaunch})
			closes = append(closes, core.CloseStep{Project: p, Target: target})
		}
	}
	var reports []core.Progress
	if mode == "shutdown" {
		reports = append(reports, core.Progress{Phase: core.PhaseRecheck}, core.Progress{Phase: core.PhaseSave})
		for i, step := range closes {
			res := core.CloseResult{CloseStep: step}
			p := core.Progress{Phase: core.PhaseClose, N: i + 1, Of: len(closes), Label: step.Name(), Closed: res}
			reports = append(reports, p)
			switch i % 5 {
			case 2:
				res.Err = broke
			case 3:
				res.Open = true
			}
			p.Done, p.Closed = true, res
			reports = append(reports, p)
		}
		rows, steps := closeRows(closes)
		return newCurtain(noCurtain, mode, rows, steps), append(reports, core.Progress{Phase: core.PhaseWait})
	}
	for i, step := range restores {
		res := core.RestoreResult{RestoreStep: step}
		p := core.Progress{Phase: core.PhaseLaunch, N: i + 1, Of: len(restores), Label: string(step.Target), Restored: res}
		reports = append(reports, p)
		if i%5 == 2 {
			res.Err = broke
		}
		p.Done, p.Restored = true, res
		reports = append(reports, p)
	}
	rows, steps := restoreRows(restores)
	return newCurtain(noCurtain, mode, rows, steps), append(reports, core.Progress{Phase: core.PhaseHome, Label: "project-00"})
}

// corner is where the block's title is drawn, and the block's form.
func corner(m curtainModel) (top, left int, shape curtainShape) {
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	for rows[top] == "" {
		top++
	}
	return top, len(rows[top]) - len(strings.TrimLeft(rows[top], " ")), m.shape()
}

// The block stays where it is, and keeps the form whose change clears the
// screen, from the plan of a walk to its last report, whatever its steps come
// to. A short summary keeps its column.
func TestTheBlockStaysInPlaceWhileTheWalkRuns(t *testing.T) {
	for _, tc := range []struct {
		name          string
		main, compact bool
		targets       []revier.TargetName
	}{
		{"main", true, false, []revier.TargetName{"home", "editor"}},
		{"main wide", true, false, []revier.TargetName{"home", "editor-of-the-project", "diff-of-the-project", "page-of-the-project"}},
		{"compact", true, true, []revier.TargetName{"home", "editor"}},
		{"secondary", false, false, []revier.TargetName{"home", "editor"}},
	} {
		for _, mode := range []string{"restore", "shutdown"} {
			for _, size := range []struct{ width, height int }{{213, 54}, {160, 40}, {101, 18}, {80, 24}} {
				t.Run(fmt.Sprintf("%s %s %dx%d", tc.name, mode, size.width, size.height), func(t *testing.T) {
					r, reports := walkReports(mode, tc.targets)
					m := sized(curtainModel{main: tc.main, compact: tc.compact, th: theme.Default()}, size.width, size.height)
					m.state = r.state
					top, left, shape := corner(m)
					for i, p := range reports {
						r.take(p)
						m.state = r.state
						if gotTop, gotLeft, gotShape := corner(m); gotTop != top || gotLeft != left || gotShape != shape {
							t.Fatalf("report %d (%s): the block is at row %d column %d with %+v, it was at row %d column %d with %+v:\n%s",
								i, m.state.Line, gotTop, gotLeft, gotShape, top, left, shape, ansi.Strip(m.View().Content))
						}
						for _, l := range strings.Split(m.View().Content, "\n") {
							if w := ansi.StringWidth(l); w > size.width {
								t.Fatalf("report %d: a line is %d wide on a monitor of %d: %q", i, w, size.width, ansi.Strip(l))
							}
						}
					}
					m.state.End = &curtain.End{OK: true, Title: "Restore done", Summary: "24 opened"}
					if _, gotLeft, _ := corner(m); gotLeft != left {
						t.Errorf("the end moved the block from column %d to %d", left, gotLeft)
					}
				})
			}
		}
	}
}

// A project's row wider than the bar is centred, and not cut.
func TestAWideRowIsCentred(t *testing.T) {
	r, _ := walkReports("restore", []revier.TargetName{"home", "editor-of-the-project", "diff-of-the-project", "page-of-the-project"})
	m := sized(curtainModel{main: true, th: theme.Default()}, 100, 40)
	m.state = r.state
	for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.Contains(l, "project-00") && !strings.HasSuffix(l, "page-of-the-project") {
			t.Errorf("the row is cut: %q", l)
		}
	}
}

// Before the first report no line says what runs.
func TestNoReportDrawsNoSpinner(t *testing.T) {
	m, s := curtainOn(t, true)
	if got := screen(shown(t, m, s, curtain.State{Mode: "restore"})); strings.Contains(got, "…") {
		t.Errorf("the screen says something runs:\n%s", got)
	}
}

// A note that fits the monitor is whole in the summary.
func TestTheSummaryShowsALongNoteWhole(t *testing.T) {
	m, s := curtainOn(t, true)
	st := ended()
	st.Rows[3].Note = strings.Repeat("why ", 26) + "end"
	m = shown(t, m, s, st)
	if got := screen(m); !strings.Contains(got, st.Rows[3].Note) {
		t.Errorf("the summary cuts a note that fits:\n%s", got)
	}
}

// sized is the curtain after the monitor took a size.
func sized(m curtainModel, width, height int) curtainModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(curtainModel)
}

// buttonDrawnAt fails the test unless the screen draws label where the
// layout says a click finds the button.
func buttonDrawnAt(t *testing.T, m curtainModel, label string) {
	t.Helper()
	b := m.layout().button
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if b.row < 0 || b.row >= len(rows) {
		t.Fatalf("button row %d is off the screen of %d rows", b.row, len(rows))
	}
	if got := ansi.Cut(rows[b.row], b.from, b.to); strings.TrimSpace(got) != label {
		t.Errorf("the screen draws %q where the button is, want %q:\n%s", got, label, strings.Join(rows, "\n"))
	}
}

// A walk with more projects than the monitor has rows keeps the title, the
// bar, the summary, the button and the hint on the screen, keeps the failed
// step in sight, and counts the projects it leaves out.
func TestALongWalkFitsTheMonitor(t *testing.T) {
	m, s := curtainOn(t, true)
	m = sized(m, 160, 16)
	st := ended()
	st.Rows = nil
	for i := range 30 {
		st.Rows = append(st.Rows, curtain.Row{Project: fmt.Sprintf("p%02d", i), Label: "home", State: curtain.RowDone})
	}
	st.Rows[25] = curtain.Row{Project: "p25", Label: "editor", State: curtain.RowFailed, Note: "the window did not appear"}
	m = shown(t, m, s, st)
	if n := strings.Count(m.View().Content, "\n"); n > 16 {
		t.Errorf("the screen has %d lines, the monitor 16", n)
	}
	got := screen(m)
	for _, want := range []string{"Restore finished with problems", "4 / 5", "p25", "editor: the window did not appear", "3 opened · 1 failed · 1 not reached", "Close", "enter or esc close", "more"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	buttonDrawnAt(t, m, "Close")
}

// Rows that exactly fill the room all show, with no line that counts more.
func TestRowsThatFillTheRoomAllShow(t *testing.T) {
	rows := []string{"p00 home", "p01 home", "p02 editor"}
	got := curtainModel{th: theme.Default()}.fit(rows, nil, len(rows))
	if !slices.Equal(got, rows) {
		t.Errorf("fit = %q, want %q", got, rows)
	}
}

// A note of several lines, as an error that joins several, stays on one
// line, so the button is drawn where a click finds it.
func TestANoteOfSeveralLinesKeepsTheButtonInPlace(t *testing.T) {
	st := ended()
	st.Rows[3].Note = "the window did not appear\nthe tab did not open"
	for _, compact := range []bool{false, true} {
		m, s := curtainOn(t, true)
		m.compact = compact
		m = shown(t, m, s, st)
		if got := screen(m); !strings.Contains(got, "the window did not appear; the tab did not open") {
			t.Errorf("compact=%v: screen lacks the note on one line:\n%s", compact, got)
		}
		buttonDrawnAt(t, m, "Close")
	}
}

// A note wider than the monitor is cut to its width, with a mark.
func TestANoteWiderThanTheMonitorIsCut(t *testing.T) {
	m, s := curtainOn(t, true)
	m = sized(m, 80, 40)
	st := ended()
	st.Rows[3].Note = strings.Repeat("too wide ", 20)
	m = shown(t, m, s, st)
	for _, l := range strings.Split(m.View().Content, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("a line is %d wide on a monitor of 80: %q", w, ansi.Strip(l))
		}
	}
	if !strings.Contains(screen(m), "…") {
		t.Errorf("the cut note has no mark:\n%s", screen(m))
	}
	buttonDrawnAt(t, m, "Close")
}
