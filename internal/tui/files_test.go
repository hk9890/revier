package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

const fileProject = `
path = "%PATH%"
%GIT%
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  launch = ["sh"]
  match = { title = "^session:{{.Name}}$" }
`

// onDisk writes one project file per name, loads them the way the CLI does,
// and returns the loaded projects and the directory the files are in. path
// maps a name to its directory; a name not in it gets a directory that
// exists.
func onDisk(t *testing.T, names []string, path map[string]string, gitURL map[string]string) ([]core.Project, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p, ok := path[n]
		if !ok {
			p = t.TempDir()
		}
		git := ""
		if u := gitURL[n]; u != "" {
			git = `git_url = "` + u + `"`
		}
		body := strings.NewReplacer("%PATH%", p, "%GIT%", git).Replace(fileProject)
		if err := os.WriteFile(filepath.Join(dir, n+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := config.LoadProjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	return projects, dir
}

func alt(m tui.Model, r rune) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true})
	return next.(tui.Model), cmd
}

func fileWorld(t *testing.T, names ...string) (*hosttest.FakeRuntime, tui.Model, string) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}
	projects, dir := onDisk(t, names, nil, nil)
	return rt, resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 20), dir
}

// Delete asks first and names the file it will remove; "y" removes the file
// and the row.
func TestDeleteAsksAndThenRemovesTheFile(t *testing.T) {
	_, m, dir := fileWorld(t, "alpha", "beta")

	m, _ = alt(m, 'd')
	if f := footer(m); !strings.Contains(f, "delete alpha?") || !strings.Contains(f, "alpha.toml") {
		t.Fatalf("footer = %q, want the question naming the file", f)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.toml")); err != nil {
		t.Fatal("the file went before the answer")
	}

	// A survey already under way when the file goes answers afterwards, with
	// the project list it started from.
	inFlight := m.Survey()

	m, _ = press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "alpha.toml")); !os.IsNotExist(err) {
		t.Errorf("alpha.toml is still there: %v", err)
	}
	if got := strings.Join(rows(m), "\n"); strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("rows = %q, want alpha gone and beta kept", got)
	}
	next, _ := m.Update(inFlight())
	if got := strings.Join(rows(next.(tui.Model)), "\n"); strings.Contains(got, "alpha") {
		t.Errorf("rows after the late survey = %q, want alpha to stay gone", got)
	}
}

// Any answer but "y" keeps the project, and the key is not acted on: an
// Enter meant as "no" must not open the project.
func TestDeleteDeclinedKeepsTheFileAndDoesNothingElse(t *testing.T) {
	rt, m, dir := fileWorld(t, "alpha")

	m, _ = alt(m, 'd')
	m, cmd := press(m, "enter")
	if cmd != nil {
		cmd()
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha.toml")); err != nil {
		t.Errorf("alpha.toml went on a declined delete: %v", err)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("the declining key opened %v", rt.Opened)
	}
	if strings.Contains(footer(m), "delete alpha?") {
		t.Errorf("footer = %q, want the question gone", footer(m))
	}
}

// A project with anything running is refused, as the picker refuses a
// running session.
func TestDeleteRefusesARunningProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:alpha", "sh")
	projects, dir := onDisk(t, []string{"alpha"}, nil, nil)
	m := resize(refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil), 120, 20)

	m, _ = alt(m, 'd')
	if f := footer(m); !strings.Contains(f, "running") {
		t.Fatalf("footer = %q, want the refusal", f)
	}
	// The refresh comes every second; the refusal has to outlast it.
	if f := footer(survey(m)); !strings.Contains(f, "running") {
		t.Errorf("footer after a refresh = %q, want the refusal still there", f)
	}
	_, _ = press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "alpha.toml")); err != nil {
		t.Errorf("a running project's file was removed: %v", err)
	}
}

// A window attached to the project is refused as a running target is: it
// would outlive the only entry that can reach it.
func TestDeleteRefusesAProjectWithAnAttachedWindow(t *testing.T) {
	wm := hosttest.New("wm")
	ref := wm.Add("Pull requests", "chromium")
	projects, dir := onDisk(t, []string{"alpha"}, nil, nil)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	root := stateWith(t, map[revier.ProjectName][]revier.TargetRef{"alpha": {ref}})
	m := resize(refreshed(t, c, projects, root, nil), 120, 20)

	m, _ = alt(m, 'd')
	if f := footer(m); !strings.Contains(f, "attached window") {
		t.Fatalf("footer = %q, want the refusal", f)
	}
	_, _ = press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "alpha.toml")); err != nil {
		t.Errorf("the file of a project with an attached window was removed: %v", err)
	}
}

// With no $EDITOR there is nothing to run, and the surface says so instead
// of doing nothing.
func TestEditWithoutEditorSaysSo(t *testing.T) {
	t.Setenv("EDITOR", "")
	_, m, _ := fileWorld(t, "alpha")
	m, cmd := alt(m, 'e')
	if cmd != nil {
		t.Error("want no command without an editor")
	}
	if f := footer(m); !strings.Contains(f, "$EDITOR") {
		t.Errorf("footer = %q, want it to name $EDITOR", f)
	}
}

// alt+e hands the terminal to the editor, and never reaches the filter.
func TestEditRunsTheEditorAndLeavesTheFilterAlone(t *testing.T) {
	t.Setenv("EDITOR", "true")
	_, m, _ := fileWorld(t, "alpha")
	m, cmd := alt(m, 'e')
	if cmd == nil {
		t.Fatal("want the editor command")
	}
	// The placeholder shows only while the query is empty.
	if q := lines(m)[1]; !strings.Contains(q, "filter") {
		t.Errorf("query line = %q, the key reached the filter", q)
	}
}

// The pane shows where a project was cloned from.
func TestDetailPaneShowsTheGitURL(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	projects, _ := onDisk(t, []string{"alpha"}, nil, map[string]string{"alpha": "git@github.com:hk9890/alpha.git"})
	m := resize(refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil), 140, 30)
	if body := pane(m); !strings.Contains(body, "git@github.com:hk9890/alpha.git") {
		t.Errorf("pane = %q, want the git URL", body)
	}
}

// A missing directory says what Enter will do about it: clone, when the file
// says from where, and otherwise which field is missing.
func TestDetailPaneSaysWhatEnterDoesForAMissingDirectory(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	gone := map[string]string{"cloneable": "/nowhere/a", "stuck": "/nowhere/b"}
	projects, _ := onDisk(t, []string{"cloneable", "stuck"}, gone, map[string]string{"cloneable": "/srv/git/a.git"})
	m := resize(refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil), 140, 30)

	if body := pane(m); !strings.Contains(body, "Enter: clone and open") {
		t.Errorf("pane = %q, want the clone offered", body)
	}
	m, _ = press(m, "down")
	if body := pane(m); !strings.Contains(body, "No git_url") {
		t.Errorf("pane = %q, want the missing field named", body)
	}
}

// Enter on a project whose directory is missing clones before anything
// launches: nothing may open into a directory that is not there yet.
func TestEnterOnAMissingDirectoryClonesBeforeLaunching(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	projects, _ := onDisk(t, []string{"cloneable"}, map[string]string{"cloneable": "/nowhere/a"},
		map[string]string{"cloneable": "/srv/git/a.git"})
	m := refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil)

	_, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("want the clone command")
	}
	cmd() // hands back the request for the terminal; the clone runs when the program grants it
	if len(rt.Opened) != 0 {
		t.Errorf("runtime Opened = %v before the clone", rt.Opened)
	}
}

// With no git_url there is nothing to clone, and Enter refuses as `revier
// open` does, naming the field: opened anyway, the workspace and its agent
// would start in whatever directory the runtime falls back to.
func TestEnterOnAMissingDirectoryWithoutGitURLOpensNothing(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	projects, _ := onDisk(t, []string{"stuck"}, map[string]string{"stuck": "/nowhere/b"}, nil)
	m := resize(refreshed(t, &core.Core{Runtime: rt}, projects, stateWith(t, nil), nil), 160, 20)

	m, cmd := press(m, "enter")
	if cmd != nil {
		cmd()
	}
	if len(rt.Opened) != 0 {
		t.Errorf("runtime Opened = %v for a directory that is not there", rt.Opened)
	}
	if f := footer(m); !strings.Contains(f, "git_url") {
		t.Errorf("footer = %q, want the missing field named", f)
	}
}

// While a delete waits for its answer, the wheel does not move the highlight:
// the question names one project, and the row under it has to stay that one.
func TestTheWheelHoldsStillWhileADeleteIsAsked(t *testing.T) {
	_, m, _ := fileWorld(t, "alpha", "beta")
	before := selectedRow(t, m)

	m, _ = alt(m, 'd')
	m = wheel(m, 5, tea.MouseButtonWheelDown)
	if row := selectedRow(t, m); row != before {
		t.Errorf("selected %q while the delete was asked, want %q", row, before)
	}
	if f := footer(m); !strings.Contains(f, "delete alpha?") {
		t.Errorf("footer = %q, want the question still open", f)
	}
}
