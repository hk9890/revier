package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/hk9890/revier/internal/tui"
)

// barCell is a terminal cell inside the first button of the action bar: the
// bar's row is under the header and the query line, and its first button
// starts a column into the frame's content.
func barCell(t *testing.T, m tui.Model) (x, y int) {
	t.Helper()
	mr, mc := margins(m)
	return mc + 2 + 2, mr + 1 + 2
}

// The bar stands under the query, and every button says its key: the bar is
// a second way to what the keyboard already reaches.
func TestTheActionBarNamesEveryButtonAndItsKey(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	bar := lines(m)[2]
	for _, want := range []string{"new", "alt+n", "link", "alt+r", "config", "alt+c"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar = %q, want it to name %q", bar, want)
		}
	}
}

// The pointer resting on a button lights it as the selected row is lit, and
// moving off it puts the button back.
func TestThePointerLightsTheButtonUnderIt(t *testing.T) {
	// The surface renders without colour where no terminal is attached, and
	// the highlight is a colour. No test runs in parallel with this one.
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	x, y := barCell(t, m)

	plain := m.View()
	m = motion(m, x, y)
	if m.View() == plain {
		t.Error("the surface is unchanged with the pointer on a button")
	}
	m = motion(m, x, y+3) // down onto the rows
	if back := m.View(); back != plain {
		t.Errorf("the surface with the pointer off the bar is not the surface before it:\n%q\n%q", back, plain)
	}
}

// One click on a button runs it: a button has no state worth selecting.
func TestOneClickOnAButtonOpensItsScreen(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	x, y := barCell(t, m)
	m = clickAt(m, x, y) // the first button, "new"
	if head := lines(m)[0]; !strings.Contains(head, "add a project") {
		t.Errorf("header = %q after a click on the new button, want the new-project screen", head)
	}
}

// alt+n reaches the same screen as the button, and Esc leaves it.
func TestAltNOpensAndEscapesTheNewProjectScreen(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	if head := lines(m)[0]; !strings.Contains(head, "add a project") {
		t.Fatalf("header = %q after alt+n, want the new-project screen", head)
	}
	m, _ = press(m, "esc")
	if head := lines(m)[0]; !strings.Contains(head, "projects") {
		t.Errorf("header = %q after esc, want the surface back", head)
	}
}

// The screen writes the project file `revier new` writes, names it after the
// directory, and the project is a row at once.
func TestTheNewProjectScreenWritesTheProjectFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, dir)
	m, _ = press(m, "enter")

	if _, err := os.Stat(filepath.Join(root, "projects", "widget.toml")); err != nil {
		t.Fatalf("no project file written: %v\n%s", err, footer(m))
	}
	if row := selectedRow(t, m); !strings.Contains(row, "widget") {
		t.Errorf("selected %q after adding, want the new project", row)
	}
}

// A directory that is not there is refused, and nothing is written.
func TestTheNewProjectScreenRefusesAMissingDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, filepath.Join(root, "nowhere"))
	m, _ = press(m, "enter")

	if f := footer(m); !strings.Contains(f, "not a directory here") {
		t.Errorf("footer = %q, want the directory refused", f)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "projects")); err == nil && len(entries) > 0 {
		t.Errorf("wrote %v for a directory that is not there", entries)
	}
}

// alt+c needs an editor to hand the file to, and says so when there is none.
func TestTheConfigButtonNeedsAnEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+c")
	if f := footer(m); !strings.Contains(f, "$EDITOR is not set") {
		t.Errorf("footer = %q, want the missing editor named", f)
	}
}

// motion is the pointer moving over a terminal cell, pressing nothing.
func motion(m tui.Model, x, y int) tui.Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion})
	return next.(tui.Model)
}

// typeInto types text into the screen's field, a rune at a time.
func typeInto(m tui.Model, text string) tui.Model {
	for _, r := range text {
		m, _ = press(m, string(r))
	}
	return m
}
