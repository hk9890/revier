package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// barCell is a terminal cell inside the first button of the action bar: the
// bar is the surface's first line, and its first label starts one column into
// the content.
func barCell(t *testing.T, m tui.Model) (x, y int) {
	t.Helper()
	mr, mc := margins(m)
	return mc + 1, mr
}

// The bar is the top line, and every button says its key: the bar is
// a second way to what the keyboard already reaches.
func TestTheActionBarNamesEveryButtonAndItsKey(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	bar := barLine(m)
	for _, want := range []string{"new", "alt+n", "remote", "alt+r", "config", "alt+c", "help", "alt+h"} {
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
	if head := barLine(m); !strings.Contains(head, "Add a project") {
		t.Errorf("top line = %q after a click on the new button, want the new-project screen", head)
	}
}

// alt+n reaches the same screen as the button, and Esc leaves it.
func TestAltNOpensAndEscapesTheNewProjectScreen(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	if head := barLine(m); !strings.Contains(head, "Add a project") {
		t.Fatalf("top line = %q after alt+n, want the new-project screen", head)
	}
	m, _ = press(m, "esc")
	if r := ruleLine(m); !strings.Contains(r, "2/2") {
		t.Errorf("rule = %q after esc, want the surface back", r)
	}
}

// The screen writes the project file `revier new` writes, names it after the
// directory, and the project is a row at once.
func TestTheNewProjectScreenWritesTheProjectFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	noTools(t)
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

// noTools empties PATH, so the screen starts neither git nor mise: this
// layer starts no process, and mise's trust list is left as it was.
func noTools(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// Enter on a row chosen under the field adds that row's folder, not the
// folder the field names.
func TestEnterAddsTheChosenDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	noTools(t)
	base := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(filepath.Join(base, "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 200, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, base+"/")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")

	if _, err := os.Stat(filepath.Join(root, "projects", "widget.toml")); err != nil {
		t.Fatalf("no project file for the chosen folder: %v\n%s", err, footer(m))
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "base.toml")); err == nil {
		t.Error("wrote the project for the folder the field names")
	}
}

// The home directory is refused, as `revier new` refuses it: it would own
// every folder no other project claims.
func TestTheHomeDirectoryIsRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("HOME", t.TempDir())
	noTools(t)
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "~/")
	m, _ = press(m, "enter")

	if f := footer(m); !strings.Contains(f, "home directory") {
		t.Errorf("footer = %q, want the home directory refused", f)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "projects")); err == nil && len(entries) > 0 {
		t.Errorf("wrote %v for the home directory", entries)
	}
}

// With shared targets in config.toml of which none is home, the written file
// declares a home of its own and nothing else, so the screen names that home
// and the shared targets rather than promise the template's agent, shell and
// editor.
func TestTheNewProjectScreenNamesItsOwnHomeAndTheSharedTargets(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 1)
	shared := []map[string]any{{
		"name": "browser",
		"window": map[string]any{
			"launch": []any{"firefox"}, "match": map[string]any{"class": "^firefox$"},
		},
	}}
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{Targets: shared}, time.Second, theme.Default(), "")
	m = resize(m, 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "~/dev/widget")

	body := strings.Join(lines(m), "\n")
	if !strings.Contains(body, "its own home and shared targets: browser") {
		t.Errorf("screen = %q, want its own home and the shared targets named", body)
	}
	if strings.Contains(body, "an agent, a shell and an editor") {
		t.Errorf("screen = %q, want no promise of the template's targets", body)
	}
}

// With a shared home the written file declares no targets, so the screen
// names the shared targets alone.
func TestTheNewProjectScreenNamesTheSharedTargetsWhenOneIsHome(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 1)
	shared := []map[string]any{{
		"name": "home", "home": true,
		"runtime": map[string]any{
			"name": "session:{{.Name}}", "match": map[string]any{"title": "^session:{{.Name}}$"},
		},
	}}
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{Targets: shared}, time.Second, theme.Default(), "")
	m = resize(m, 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "~/dev/widget")

	body := strings.Join(lines(m), "\n")
	if !strings.Contains(body, "with the shared targets: home") || strings.Contains(body, "its own home") {
		t.Errorf("screen = %q, want the shared targets alone named", body)
	}
}

// An alt chord is a key, not text: pressed on the new-project screen, or on the
// list, it types nothing.
func TestAnAltChordTypesNothing(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+x")
	if q := query(m); strings.Contains(q, "x") {
		t.Errorf("query = %q after alt+x, want nothing typed", q)
	}
	m, _ = press(m, "alt+n")
	m = typeInto(m, "/tmp/w")
	m, _ = press(m, "alt+c")
	m, _ = press(m, "alt+x")
	if head := barLine(m); !strings.Contains(head, "Add a project") {
		t.Fatalf("top line = %q, want the new-project screen still up", head)
	}
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "❯ /tmp/w ") {
		t.Errorf("screen = %q, want the path as typed and no letter of an alt chord", body)
	}
}

// A full path that is not there is asked about: Esc writes nothing, and
// Enter creates the folder and the project.
func TestAMissingFullPathIsCreatedAfterAsking(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	noTools(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, dir)
	m, _ = press(m, "enter")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "is not there") {
		t.Fatalf("screen = %q, want the folder asked about", body)
	}
	m, _ = press(m, "esc")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("esc created the folder")
	}
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("no folder created: %v\n%s", err, footer(m))
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "fresh.toml")); err != nil {
		t.Fatalf("no project file written: %v", err)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "fresh") {
		t.Errorf("selected %q after adding, want the new project", row)
	}
}

// A name goes on to the folders the projects live in, the fullest first and
// the home directory among them; a folder that is there is added at once.
func TestANameIsAddedInTheChosenFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	noTools(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "widget")
	m, _ = press(m, "enter")
	body := strings.Join(lines(m), "\n")
	if p, h := strings.Index(body, "/p/widget"), strings.Index(body, "~/widget  (already there)"); p < 0 || h < p {
		t.Fatalf("screen = %q, want /p, where both projects live, over ~", body)
	}
	m, _ = press(m, "down")
	m, _ = press(m, "enter")

	file, err := os.ReadFile(filepath.Join(root, "projects", "widget.toml"))
	if err != nil {
		t.Fatalf("no project file written: %v\n%s", err, footer(m))
	}
	if !strings.Contains(string(file), `path = "~/widget"`) {
		t.Errorf("project file = %q, want the folder in the home directory", file)
	}
}

// A name whose folder is not there is asked about, and Esc goes back to the
// folders.
func TestANameInAMissingFolderIsAsked(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "widget")
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "is not there") || !strings.Contains(body, "    /p/widget ") {
		t.Fatalf("screen = %q, want the folder asked about", body)
	}
	m, _ = press(m, "esc")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "~/widget") {
		t.Errorf("screen = %q, want the folders back", body)
	}
}

// Tab completes the part the subdirectories share, then the one chosen. A
// hidden directory is not offered until a dot is typed.
func TestTabCompletesADirectory(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
	base := t.TempDir()
	for _, d := range []string{"alpha", "alps", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 200, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, base+"/")
	body := strings.Join(lines(m), "\n")
	if !strings.Contains(body, base+"/beta") || strings.Contains(body, ".hidden") {
		t.Errorf("screen = %q, want the subdirectories without the hidden one", body)
	}
	m = typeInto(m, "a")
	m, _ = press(m, "tab")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "❯ "+base+"/alp ") {
		t.Fatalf("screen = %q, want the shared part completed", body)
	}
	m, _ = press(m, "down")
	m, _ = press(m, "tab")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "❯ "+base+"/alpha/ ") {
		t.Errorf("screen = %q, want the chosen directory taken", body)
	}
}

// A clone URL asks before it clones into the chosen folder under the
// repository's name, then writes the project with the URL and starts the
// clone.
func TestACloneURLWritesTheProjectAndClones(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("HOME", t.TempDir())
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "git@github.com:owner/widget.git")
	m, _ = press(m, "enter")
	if body := strings.Join(lines(m), "\n"); !strings.Contains(body, "/p/widget") || !strings.Contains(body, "~/widget") {
		t.Fatalf("screen = %q, want each folder with the clone's directory in it", body)
	}
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")
	if body := strings.Join(lines(m), "\n"); cmd != nil || !strings.Contains(body, "is not there") {
		t.Fatalf("screen = %q, want the clone asked about first", body)
	}
	m, cmd = press(m, "enter")

	file, err := os.ReadFile(filepath.Join(root, "projects", "widget.toml"))
	if err != nil {
		t.Fatalf("no project file written: %v\n%s", err, footer(m))
	}
	for _, want := range []string{`path = "~/widget"`, `git_url = "git@github.com:owner/widget.git"`} {
		if !strings.Contains(string(file), want) {
			t.Errorf("project file = %q, want %s", file, want)
		}
	}
	if cmd == nil {
		t.Error("no clone started")
	}
	if row := selectedRow(t, m); !strings.Contains(row, "widget") {
		t.Errorf("selected %q after adding, want the new project", row)
	}
}

// A clone URL whose folder is already there adds that folder, clones
// nothing, and says the URL, which is not the folder's origin, is ignored.
func TestACloneURLAddsAnExistingFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	noTools(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, c, projects := world(t, 1)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "https://github.com/owner/widget")
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	m, cmd := press(m, "enter")

	if f := footer(m); !strings.Contains(f, "the URL is ignored") {
		t.Errorf("footer = %q, want the URL said to be ignored", f)
	}
	if cmd != nil {
		t.Error("a clone started into an existing directory")
	}
	file, err := os.ReadFile(filepath.Join(root, "projects", "widget.toml"))
	if err != nil {
		t.Fatalf("no project file written: %v", err)
	}
	if strings.Contains(string(file), "git_url") {
		t.Errorf("project file = %q, want no git_url the folder does not have", file)
	}

	// The same folder again is a project already, and nothing is written.
	m, _ = press(m, "alt+n")
	m = typeInto(m, filepath.Join(home, "widget"))
	m, _ = press(m, "enter")
	if f := footer(m); !strings.Contains(f, "already") {
		t.Errorf("footer = %q, want the folder refused as a project already", f)
	}
}

// A folder that is a project already under another name is refused.
func TestAFolderThatIsAProjectIsRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	_, _, c, _ := world(t, 1)
	projects := core.Prepare([]revier.Project{{Name: "other", Path: "/p/widget"}})
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = press(m, "alt+n")
	m = typeInto(m, "/p/widget/")
	m, _ = press(m, "enter")
	if f := footer(m); !strings.Contains(f, `already project "other"`) {
		t.Errorf("footer = %q, want the folder refused", f)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "projects")); err == nil && len(entries) > 0 {
		t.Errorf("wrote %v for a folder that is a project already", entries)
	}
}

// alt+h lists every key: the surface's own, the target keys, the configured
// actions and the desktop key that opens revier. Esc goes back to the list.
func TestAltHListsEveryKeyAndEscLeaves(t *testing.T) {
	_, _, c, projects := world(t, 2)
	actions := []config.Action{{Key: "ctrl-y", Name: "sync", Run: []string{"true"}}}
	m := resize(refreshed(t, c, projects, stateWith(t, nil), actions), 120, 80)

	m, _ = press(m, "alt+h")
	if head := barLine(m); !strings.Contains(head, "Keyboard shortcuts") {
		t.Fatalf("top line = %q after alt+h, want the help screen", head)
	}
	screen := strings.Join(lines(m), "\n")
	for _, want := range []string{
		"alt+n", "alt+r", "alt+c", "alt+h", "alt+e", "alt+d", "ctrl+w", "ctrl+c",
		"ctrl+y", "sync",
		"ctrl+shift+u", "go to home",
		"alt+space", "open revier",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("help screen does not name %q:\n%s", want, screen)
		}
	}

	m, _ = press(m, "esc")
	if r := ruleLine(m); !strings.Contains(r, "2/2") {
		t.Errorf("rule = %q after esc, want the surface back", r)
	}
}

// The key that opens the help screen closes it, and a letter typed on it
// does not reach the filter behind it.
func TestAltHClosesTheHelpScreenAndFiltersNothing(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 40)

	m, _ = press(m, "alt+h")
	m, _ = press(m, "x")
	m, _ = press(m, "alt+h")
	if head := barLine(m); !strings.Contains(head, "help") {
		t.Fatalf("top line = %q after alt+h twice, want the bar back", head)
	}
	if q := query(m); strings.Contains(q, "x") {
		t.Errorf("query = %q, want the letter typed on the help screen dropped", q)
	}
}

// On a terminal shorter than the list of keys, the arrows scroll the screen.
func TestTheHelpScreenScrolls(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 16)

	m, _ = press(m, "alt+h")
	top := m.View()
	m, _ = press(m, "down")
	if m.View() == top {
		t.Error("the help screen is unchanged after down on a short terminal")
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

// The pointer lights a row of the list and a target in the pane, and the
// light is not the selection's: two rows can be marked at once, and which of
// them Enter means must stay readable.
func TestThePointerLightsARowAndATarget(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	_, mc := margins(m)

	plain := m.View()
	m = motion(m, mc+6, rowTop(m)+2) // the second row, not the selected one
	if m.View() == plain {
		t.Error("the list is unchanged with the pointer on a row")
	}
	if lit := selectedRow(t, m); !strings.Contains(lit, "project-02") {
		t.Errorf("selected %q, want the pointer to have moved nothing", lit)
	}

	x, y := paneCell(t, m, "editor")
	before := m.View()
	if m = motion(m, x, y); m.View() == before {
		t.Error("the pane is unchanged with the pointer on a target")
	}
}

// The first line below the last row is not a row: the pointer lights nothing
// there, and a click chooses nothing.
func TestThePointerBelowTheLastRowLightsNothing(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 30) // room under the rows
	_, mc := margins(m)
	below := rowTop(m) + 3*2 // three rows of two lines each

	plain := m.View()
	if m = motion(m, mc+6, below); m.View() != plain {
		t.Errorf("the list changed with the pointer below its last row:\n%s", m.View())
	}
	was := selectedRow(t, m)
	if m = clickAt(m, mc+6, below); selectedRow(t, m) != was {
		t.Errorf("selected %q after a click below the last row, want %q", selectedRow(t, m), was)
	}
}

// The light follows what is under the pointer, not the index it was on: after
// the keyboard scrolls the list, the surface is the one a fresh move of the
// pointer to the same cell draws.
func TestThePointerLightStaysOnWhatIsUnderIt(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	_, _, c, projects := world(t, 12)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 14)
	_, mc := margins(m)
	x, y := mc+6, rowTop(m)

	got := motion(m, x, y)
	for range 11 {
		got, _ = press(got, "down")
	}
	if want := motion(got, x, y); got.View() != want.View() {
		t.Errorf("after a scroll the light is not on the row under the pointer:\n%s\nwant:\n%s", got.View(), want.View())
	}
}

// Moving the pointer within one row redraws nothing that differs.
func TestThePointerMovingWithinARowChangesNothing(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	_, mc := margins(m)

	m = motion(m, mc+6, rowTop(m)+2)
	before := m.View()
	if m = motion(m, mc+9, rowTop(m)+2); m.View() != before {
		t.Error("the surface changed with the pointer still on the same row")
	}
}

// rowTop is the terminal row the first row of the list is on.
func rowTop(m tui.Model) int {
	mr, _ := margins(m)
	return mr + 4
}

// The new-project screen stands over the list: a double click where the rows
// were moves no selection behind it, and runs nothing.
func TestAClickOnTheNewProjectScreenReachesNoRow(t *testing.T) {
	_, _, c, projects := world(t, 3)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20)
	_, mc := margins(m)
	was := selectedRow(t, m)

	m, _ = press(m, "alt+n")
	m = clickAt(m, mc+6, rowTop(m)+2)
	m = clickAt(m, mc+6, rowTop(m)+2)
	if head := barLine(m); !strings.Contains(head, "Add a project") {
		t.Fatalf("top line = %q after a double click, want the new-project screen", head)
	}
	m, _ = press(m, "esc")
	if now := selectedRow(t, m); now != was {
		t.Errorf("selected %q after clicks on the new-project screen, want %q", now, was)
	}
}
