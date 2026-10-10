package tui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// Under its head the pane shows what the agent's panel shows, and follows it.
func TestThePaneMirrorsTheAgentsPanel(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen\nsecond line\n\n\n"},
		listed{project: "alpha", status: revier.StatusRunning, on: "two", screen: "the other panel"})
	body := pane(m)
	if !strings.Contains(body, "first screen") || !strings.Contains(body, "second line") || strings.Contains(body, "the other panel") {
		t.Fatalf("pane does not mirror the panel of the agent under the cursor:\n%s", body)
	}
	rt.Screens["1"] = "the screen moved on"
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "the screen moved on") || strings.Contains(body, "first screen") {
		t.Errorf("pane does not follow the panel:\n%s", body)
	}
	m, _ = press(m, "down")
	m = m.Mirrored()
	if body := pane(m); !strings.Contains(body, "the other panel") {
		t.Errorf("pane does not mirror the agent the cursor moved to:\n%s", body)
	}
}

// An agent that comes under the cursor is drawn with its own screen pending,
// and not over the screen of the agent the cursor left.
func TestTheScreenOfTheAgentTheCursorLeftIsNotShownUnderTheNext(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen"},
		listed{project: "alpha", status: revier.StatusRunning, on: "two", screen: "the other panel"})
	m, _ = press(m, "down")
	body := pane(m)
	if !strings.Contains(body, "Agent    two") || strings.Contains(body, "first screen") || !strings.Contains(body, "reading...") {
		t.Errorf("pane shows another agent's screen under this one's head:\n%s", body)
	}
}

// A mirror that comes back into view holds a screen read before it left: it
// is read again at once, and not a tick later.
func TestTheMirrorIsReadAgainWhenItComesBackIntoView(t *testing.T) {
	m, _, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "first screen"})
	m, _ = press(m, "alt+h")
	m = m.MirrorTicked()
	asked := m.MirrorAsked()
	m, _ = press(m, "esc")
	if got := m.MirrorAsked(); got != asked+1 {
		t.Errorf("reads asked for = %d after the screen over the mirror left, want %d", got, asked+1)
	}
}

// A wheel up over a screen with nothing above it has no line to hold: the
// mirror follows the end again, and reads the scrollback no more.
func TestAWheelWithNothingAboveTheScreenLeavesTheMirrorFollowing(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: "only line"})
	border := paneBorder(t, m)
	next, cmd := m.Update(tea.MouseMsg{X: border + 4, Y: 12, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("the wheel over the mirror asked for nothing, want a read of the scrollback")
	}
	m = run(next.(tui.Model), cmd)

	var screen []string
	for i := range 80 {
		screen = append(screen, fmt.Sprintf("row %02d", i))
	}
	rt.Screens["1"] = strings.Join(screen, "\n")
	m = m.Mirrored()
	if last := rt.ScreenReads[len(rt.ScreenReads)-1]; last.Scrollback {
		t.Errorf("last read = %+v, want the screen alone once nothing is above it", last)
	}
	if body := pane(m); !strings.Contains(body, "row 79") {
		t.Errorf("pane does not follow the end of the screen:\n%s", body)
	}
}

// A screen longer than the pane shows its end; the wheel scrolls back into
// the panel's scrollback, which is read only then.
func TestTheWheelScrollsTheMirrorIntoTheScrollback(t *testing.T) {
	var screen []string
	for i := range 80 {
		screen = append(screen, fmt.Sprintf("row %02d", i))
	}
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusRunning, on: "one", screen: strings.Join(screen[60:], "\n")})
	if body := pane(m); !strings.Contains(body, "row 79") || strings.Contains(body, "row 59") {
		t.Fatalf("pane does not show the end of the screen:\n%s", body)
	}
	for _, r := range rt.ScreenReads {
		if r.Scrollback {
			t.Fatalf("reads = %+v, want none of the scrollback before the user scrolls", rt.ScreenReads)
		}
	}
	rt.Screens["1"] = strings.Join(screen, "\n")
	border := paneBorder(t, m)
	next, cmd := m.Update(tea.MouseMsg{X: border + 4, Y: 12, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("the wheel over the mirror asked for nothing, want a read of the scrollback")
	}
	m = run(next.(tui.Model), cmd)
	if last := rt.ScreenReads[len(rt.ScreenReads)-1]; !last.Scrollback {
		t.Errorf("last read = %+v, want the scrollback", last)
	}
	if body := pane(m); strings.Contains(body, "row 79") || !strings.Contains(body, "row 76") {
		t.Errorf("pane is not scrolled up by a notch:\n%s", body)
	}
}

// A runtime that cannot read a panel leaves the mirror a note, and the head
// stands.
func TestAPanelThatCannotBeReadLeavesANote(t *testing.T) {
	m, rt, _ := listedWorld(t, 160, 30,
		listed{project: "alpha", status: revier.StatusIdle, on: "one", screen: "never shown"})
	rt.ScreenErr = core.ErrNoScreen
	m, _ = press(m, "down")
	m = switched(switched(m)).Mirrored()
	body := pane(m)
	if !strings.Contains(body, "cannot be read") || !strings.Contains(body, "Agent    one") {
		t.Errorf("pane = %q, want the head and a note in the mirror's place", body)
	}
}

// The mirror keeps colour and drops everything else a screen carries: a line
// too long continues on the next in the style it was cut in, and every line
// is closed.
func TestScreenLinesKeepColourAndNothingElse(t *testing.T) {
	red := "\x1b[31m"
	got := tui.ScreenLines(red+"abcdef\x1b[39m\n"+
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\ \x1b[2Jcleared\x1b[>4;2m\r\n"+
		"\x1b[1;38;5;208mbold\x1b[0m plain\n\n\n", 4)
	want := []string{
		red + "abcd\x1b[m",
		red + "ef\x1b[39m\x1b[m",
		"link",
		" cle",
		"ared",
		"\x1b[1;38;5;208mbold\x1b[0m\x1b[m",
		" pla",
		"in",
	}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q\nwant    %q", got, want)
	}
}

// A character is as wide as the terminal draws it with what joins it: an
// emoji and its variation selector take two cells together, and the line is
// cut for two.
func TestScreenLinesMeasureACharacterWithWhatJoinsIt(t *testing.T) {
	got := tui.ScreenLines("⚠️ab", 3)
	want := []string{"⚠️a", "b"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}
