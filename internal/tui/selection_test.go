package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/tui"
)

func mouseAt(m tui.Model, x, y int, action tea.MouseAction) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
	return next.(tui.Model), cmd
}

// dragged is the left button pressed on one cell and moved to another, not
// yet released.
func dragged(m tui.Model, fromX, fromY, toX, toY int) tui.Model {
	m, _ = mouseAt(m, fromX, fromY, tea.MouseActionPress)
	m, _ = mouseAt(m, toX, toY, tea.MouseActionMotion)
	return m
}

// A drag stands over the screen as it was when the drag began: a change
// underneath, here a resize, reaches the screen only after the release.
func TestADragFreezesTheScreenUntilRelease(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	top := rowTop(m)
	plain := ansi.Strip(m.View())

	m = dragged(m, 4, top, 12, top+1)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(tui.Model)
	if got := ansi.Strip(m.View()); got != plain {
		t.Errorf("the screen changed during a drag:\n%s\nwant:\n%s", got, plain)
	}

	m, cmd := mouseAt(m, 12, top+1, tea.MouseActionRelease)
	if cmd == nil {
		t.Error("the release copied nothing")
	}
	if got := ansi.Strip(m.View()); got == plain {
		t.Error("the screen is still the frozen one after the release")
	}
}

// The copy is the box between the two corners, not whole lines, and the
// footer says how much was copied until the next press.
func TestAReleaseCopiesTheBoxAndSaysSo(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	top := rowTop(m)
	screen := strings.Split(ansi.Strip(m.View()), "\n")
	var want []string
	for _, line := range screen[top : top+2] {
		want = append(want, strings.TrimRight(ansi.Cut(line, 4, 13), " "))
	}
	text := strings.Join(want, "\n")
	if strings.TrimSpace(text) == "" {
		t.Fatalf("the box over rows %d-%d holds no text:\n%s", top, top+1, strings.Join(screen, "\n"))
	}

	m = dragged(m, 12, top+1, 4, top) // corners either way round
	m, _ = mouseAt(m, 4, top, tea.MouseActionRelease)
	if got, want := footer(m), fmt.Sprintf("Copied %d characters.", len([]rune(text))); !strings.Contains(got, want) {
		t.Errorf("footer = %q after copying %q, want %q", got, text, want)
	}
	m, _ = press(m, "down")
	if got := footer(m); strings.Contains(got, "Copied") {
		t.Errorf("footer = %q after the next key, want the legend back", got)
	}
}

// The box is drawn over the frozen screen without moving anything beside it.
func TestTheBoxLeavesEveryLineItsWidth(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	top := rowTop(m)
	before := strings.Split(m.View(), "\n")

	after := strings.Split(dragged(m, 4, top, 30, top+2).View(), "\n")
	if len(after) != len(before) {
		t.Fatalf("the frozen screen has %d lines, want %d", len(after), len(before))
	}
	for i := range before {
		if a, b := ansi.StringWidth(after[i]), ansi.StringWidth(before[i]); a != b {
			t.Errorf("line %d is %d cells wide with the box, want %d", i, a, b)
		}
	}
	if !strings.Contains(after[top], "\x1b[0;7m") {
		t.Errorf("line %d = %q, want the box in reverse video", top, after[top])
	}
}

// A drag that begins on a button selects and runs nothing: a button runs on a
// release where it was pressed.
func TestADragFromAButtonRunsNothing(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	x, y := barCell(t, m)

	m = dragged(m, x, y, x+6, y)
	m, _ = mouseAt(m, x+6, y, tea.MouseActionRelease)
	if head := barLine(m); strings.Contains(head, "Add a project") {
		t.Errorf("top line = %q after a drag from the new button, want the bar", head)
	}
}

// Esc drops a selection mid-drag, and the release after it copies nothing.
func TestEscDropsTheSelection(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	top := rowTop(m)
	plain := m.View()

	m = dragged(m, 4, top, 12, top+1)
	m, _ = press(m, "esc")
	if m.View() != plain {
		t.Error("the box is still drawn after esc")
	}
	m, cmd := mouseAt(m, 12, top+1, tea.MouseActionRelease)
	if cmd != nil {
		t.Error("the release after esc copied")
	}
	if strings.Contains(footer(m), "Copied") {
		t.Errorf("footer = %q, want nothing copied", footer(m))
	}
}
