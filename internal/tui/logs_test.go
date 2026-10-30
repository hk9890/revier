package tui_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/tui"
)

// logLine is one line of the log as slog writes it.
func logLine(t *testing.T, at time.Time, level, msg string, pid int, attrs map[string]any) string {
	t.Helper()
	rec := map[string]any{"time": at, "level": level, "msg": msg, "pid": pid, "cmd": "tui"}
	for k, v := range attrs {
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// writeLog writes the day file of the log that holds day.
func writeLog(t *testing.T, root string, day time.Time, lines ...string) {
	t.Helper()
	dir := logging.Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "revier-"+day.Format("2006-01-02")+".log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// logged is a surface of two projects over a log of three lines this
// process wrote around the hand of the clock, and one another process wrote:
// the line of an error the footer shows at the hand is "the failure".
func logged(t *testing.T) (tui.Model, []core.Project) {
	t.Helper()
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m, hand := clocked(resize(refreshed(t, c, projects, root, nil), 120, 30))
	me := os.Getpid()
	writeLog(t, root, *hand,
		logLine(t, hand.Add(-3*time.Second), "ERROR", "an older one", me, nil),
		logLine(t, hand.Add(-2*time.Second), "ERROR", "the failure", me, map[string]any{"project": "project-00", "err": "fatal: not found\nsecond line"}),
		logLine(t, hand.Add(-time.Second), "INFO", "went fine", me, nil),
		logLine(t, hand.Add(-500*time.Millisecond), "WARN", "another process", me+1, nil),
		logLine(t, hand.Add(time.Second), "WARN", "a later one", me, nil),
	)
	return m, projects
}

// textCell is the terminal cell where text starts on the line that carries
// it.
func textCell(t *testing.T, m tui.Model, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(drawn(m), "\n") {
		if x := column(line, text); x >= 0 {
			return x, y
		}
	}
	t.Fatalf("no line carries %q:\n%s", text, drawn(m))
	return 0, 0
}

// alt+l lists the warnings and errors of the log, newest first, and Esc goes
// back to the surface it stood over.
func TestAltLListsTheLogNewestFirstAndEscGoesBack(t *testing.T) {
	m, _ := logged(t)
	m, _ = press(m, "alt+l")
	if top := barLine(m); !strings.Contains(top, "Logs") {
		t.Fatalf("top line = %q, want the logs screen", top)
	}
	if rule := ruleLine(m); !strings.Contains(rule, "4 lines") {
		t.Errorf("rule = %q, want the four warnings and errors counted", rule)
	}
	body := strings.Join(rows(m), "\n")
	order := []string{"a later one", "another process", "the failure", "an older one"}
	at := -1
	for _, msg := range order {
		i := strings.Index(body, msg)
		if i <= at {
			t.Fatalf("rows are not %v, newest first:\n%s", order, body)
		}
		at = i
	}
	if strings.Contains(body, "went fine") || strings.Contains(body, "second line") {
		t.Errorf("rows show an INFO line or more than an error's first line:\n%s", body)
	}
	m, _ = press(m, "esc")
	if rule := ruleLine(m); !strings.Contains(rule, "2/2") {
		t.Errorf("rule = %q after esc, want the project list", rule)
	}
}

// The screen goes back to the screen it was opened from, on Esc and on the
// key that opened it.
func TestTheLogsScreenGoesBackToTheScreenItCameFrom(t *testing.T) {
	m, _ := logged(t)
	m, _ = press(m, "alt+s")
	for _, leave := range []string{"esc", "alt+l"} {
		m, _ = press(m, "alt+l")
		if top := barLine(m); !strings.Contains(top, "Logs") {
			t.Fatalf("top line = %q, want the logs screen over the sessions", top)
		}
		m, _ = press(m, leave)
		if top := barLine(m); !strings.Contains(top, "Sessions") {
			t.Errorf("top line = %q after %s, want the sessions screen", top, leave)
		}
	}
}

// A failed operation names itself in the footer, and a click on it or alt+l
// opens the screen on the line it was logged as: the newest this process
// wrote by the time the footer took it.
func TestAFailedCloneOpensTheLogOnItsLine(t *testing.T) {
	m, projects := logged(t)
	failed, _ := m.Cloned(projects[0], errors.New("fatal: not found\nsecond line"))
	if got, want := strings.TrimSpace(footer(failed)), "clone failed: project-00 (alt+l for details)"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	x, y := textCell(t, failed, "clone failed")
	clicked := clickAt(failed, x, y)
	keyed, _ := press(failed, "alt+l")
	for how, m := range map[string]tui.Model{"a click": clicked, "alt+l": keyed} {
		if top := barLine(m); !strings.Contains(top, "Logs") {
			t.Fatalf("%s: top line = %q, want the logs screen", how, top)
		}
		if row := selectedRow(t, m); !strings.Contains(row, "the failure") {
			t.Errorf("%s: cursor on %q, want the clone's line", how, row)
		}
	}

	m, _ = press(keyed, "enter")
	for _, want := range []string{"ERROR the failure", "project", "project-00", "fatal: not found", "second line", "pid"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("the line in full lacks %q:\n%s", want, screen(m))
		}
	}
	m, _ = press(m, "esc")
	if row := selectedRow(t, m); !strings.Contains(row, "the failure") {
		t.Errorf("esc from the line: cursor on %q, want the same line", row)
	}
}

// With no error in the footer the screen opens on the newest line.
func TestTheLogsScreenOpensOnTheNewestLine(t *testing.T) {
	m, _ := logged(t)
	m, _ = press(m, "alt+l")
	if row := selectedRow(t, m); !strings.Contains(row, "a later one") {
		t.Errorf("cursor on %q, want the newest line", row)
	}
}

// "logs" at the top of the pane opens the screen, and one click on a line
// shows it in full.
func TestLogsInThePaneOpensTheScreen(t *testing.T) {
	m, _ := logged(t)
	m = resize(m, 140, 30)
	x, y := paneCell(t, m, "logs Project")
	m = clickAt(m, x, y)
	if top := barLine(m); !strings.Contains(top, "Logs") {
		t.Fatalf("top line = %q after a click on logs, want the logs screen", top)
	}
	x, y = textCell(t, m, "an older one")
	m = clickAt(m, x, y)
	if s := screen(m); !strings.Contains(s, "ERROR an older one") || !strings.Contains(s, "pid") {
		t.Errorf("one click on a line did not show it in full:\n%s", s)
	}
}

// A plan that arrives while the logs screen stands over the shutdown wizard
// is the wizard's: Esc goes back to the plan, not to a survey nobody asks for
// again.
func TestAShutdownPlanThatArrivesUnderTheLogsScreenIsKept(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	m, _ = press(m, "alt+q")
	m, plan := press(m, "enter")
	if !strings.Contains(drawn(m), "surveying") {
		t.Fatalf("the wizard does not wait for its plan:\n%s", drawn(m))
	}
	m, _ = press(m, "alt+l")
	m = run(m, plan)
	m, _ = press(m, "esc")
	if s := drawn(m); strings.Contains(s, "surveying") {
		t.Errorf("the wizard still waits for the plan that arrived under the logs screen:\n%s", s)
	}
}

// A start dialog whose shutdown left the record while the logs screen stood
// over it is gone when Esc goes back: it would offer a stale restore.
func TestAStartDialogUnderTheLogsScreenGoesWithItsShutdown(t *testing.T) {
	m, _, root, _ := shutDownWorld(t)
	m, _ = press(m, "alt+l")
	if err := (&state.State{}).Save(root); err != nil {
		t.Fatal(err)
	}
	m = survey(m)
	if top := barLine(m); !strings.Contains(top, "Logs") {
		t.Fatalf("top line = %q, want the logs screen kept over the survey", top)
	}
	m, _ = press(m, "esc")
	if top := barLine(m); strings.Contains(top, "Welcome back") {
		t.Errorf("top line = %q after esc, want the surface: no shutdown is on record", top)
	}
}

// An error that comes while the logs screen is up names no key: on this
// screen the key closes it, and the screen read the log before the error.
func TestAnErrorOnTheLogsScreenNamesNoKey(t *testing.T) {
	m, projects := logged(t)
	m, _ = press(m, "alt+l")
	m, _ = m.Cloned(projects[0], errors.New("boom"))
	if got, want := strings.TrimSpace(footer(m)), "clone failed: project-00"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
}

// A go a target refuses keeps the refusal's own words in the footer: they
// say why, and nothing failed.
func TestARefusedGoSaysWhyInTheFooter(t *testing.T) {
	_, _, c, projects := world(t, 1)
	c.Window = nil
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 150, 30)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on the editor ran nothing")
	}
	if f := footer(run(m, cmd)); !strings.Contains(f, "no host can realize this target") || strings.Contains(f, "failed") {
		t.Errorf("footer = %q, want the refusal's own words", f)
	}
}

// The screen the logs screen opened over keeps its focus: Esc comes back to
// the pane's cursor where it was.
func TestTheLogsScreenKeepsThePaneCursor(t *testing.T) {
	m, _ := logged(t)
	m, _ = press(m, "alt+t")
	m, _ = press(m, "down")
	was := paneCursor(m)
	if was == "" {
		t.Fatal("alt+t put no cursor in the pane")
	}
	m, _ = press(m, "alt+l")
	m, _ = press(m, "esc")
	if got := paneCursor(m); got != was {
		t.Errorf("pane cursor = %q after the logs screen, want %q", got, was)
	}
}

// The lines that could not be read are counted on the rule, which a
// terminal of 80 columns still shows whole.
func TestTheUnreadableLinesAreCountedOnANarrowTerminal(t *testing.T) {
	_, _, c, projects := world(t, 2)
	root := stateWith(t, nil)
	m, hand := clocked(resize(refreshed(t, c, projects, root, nil), 80, 24))
	writeLog(t, root, *hand, logLine(t, hand.Add(-time.Second), "ERROR", "one", os.Getpid(), nil), "not json\n")
	m, _ = press(m, "alt+l")
	if rule := ruleLine(m); !strings.Contains(rule, "1 line, 1 unreadable") {
		t.Errorf("rule = %q, want the unreadable line counted", rule)
	}
}

// The pointer lights "logs" in the pane and the error in the footer: both
// are clicked.
func TestThePointerLightsLogsAndTheFootersError(t *testing.T) {
	inColour(t)
	m, projects := logged(t)
	m = resize(m, 140, 30)
	m, _ = m.Cloned(projects[0], errors.New("boom"))
	for _, text := range []string{"logs", "clone failed"} {
		x, y := textCell(t, m, text)
		if before := drawn(m); drawn(motion(m, x, y)) == before {
			t.Errorf("the surface is unchanged with the pointer on %q", text)
		}
	}
}
