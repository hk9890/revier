package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// placed is a project at path whose editor window has the class code-<name>,
// so a focused window can name it.
func placed(name revier.ProjectName, path string) core.Project {
	return core.PrepareProject(revier.Project{Name: name, Path: path, Targets: []revier.Target{
		{Name: "editor", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code-" + string(name) + "$"}}},
	}})
}

// resolving is a core over two projects, demo and other, with the window of
// other focused and other remembered as the last project.
func resolving(t *testing.T) (*core.Core, *hosttest.Fake, []core.Project) {
	t.Helper()
	wm := hosttest.New("wm")
	wm.SetFocus(wm.Add("README.md", "code-other"))
	c := &core.Core{Window: wm, Ledger: &ledger{st: state.State{Current: "other"}}}
	return c, wm, []core.Project{placed("demo", t.TempDir()), placed("other", t.TempDir())}
}

// A named project is the only step: neither the directory, the focused
// window nor the last project stands in for a name that is no project's.
func TestProjectForTakesTheNamedProjectAlone(t *testing.T) {
	c, _, projects := resolving(t)
	ctx := t.Context()
	demo := projects[0]

	p, err := c.ProjectFor(ctx, projects, core.Ask{Name: "demo", Dir: projects[1].Path, Away: true})
	if err != nil || p.Name != "demo" {
		t.Errorf("ProjectFor(-p demo) = %q, %v; want demo", p.Name, err)
	}
	_, err = c.ProjectFor(ctx, projects, core.Ask{Name: "gone", Dir: demo.Path, Away: true})
	if err == nil || !strings.Contains(err.Error(), `"gone"`) {
		t.Errorf("ProjectFor(-p gone) err = %v; want it refused by name", err)
	}
}

// The directory a command runs in says what it means before the window in
// front of the user does.
func TestProjectForTakesTheDirectoryBeforeTheFocusedWindow(t *testing.T) {
	c, _, projects := resolving(t)
	p, err := c.ProjectFor(t.Context(), projects, core.Ask{Dir: filepath.Join(projects[0].Path, "sub"), Away: true})
	if err != nil || p.Name != "demo" {
		t.Errorf("ProjectFor = %q, %v; want demo, the project of the directory", p.Name, err)
	}
}

// Away from every project, the project the user is looking at beats the one
// revier last acted on.
func TestProjectForAwayTakesTheFocusedWindowBeforeTheLastProject(t *testing.T) {
	c, wm, projects := resolving(t)
	wm.SetFocus(wm.Add("README.md", "code-demo"))
	p, err := c.ProjectFor(t.Context(), projects, core.Ask{Dir: t.TempDir(), Away: true})
	if err != nil || p.Name != "demo" {
		t.Errorf("ProjectFor = %q, %v; want demo, the project of the focused window", p.Name, err)
	}
}

// A focused window that no project claims leaves the last project, and a
// last project that no longer names a configured one leaves nothing.
func TestProjectForAwayFallsBackToTheLastProject(t *testing.T) {
	c, wm, projects := resolving(t)
	ctx := t.Context()
	wm.SetFocus(wm.Add("Mozilla Firefox", "firefox"))

	p, err := c.ProjectFor(ctx, projects, core.Ask{Away: true})
	if err != nil || p.Name != "other" {
		t.Errorf("ProjectFor = %q, %v; want other, the last project", p.Name, err)
	}
	c.Ledger = &ledger{st: state.State{Current: "removed"}}
	if _, err := c.ProjectFor(ctx, projects, core.Ask{Away: true}); !errors.Is(err, core.ErrNoProject) {
		t.Errorf("ProjectFor with a last project that is gone: err = %v; want core.ErrNoProject", err)
	}
}

// A window host that cannot say what is focused costs that step alone.
func TestProjectForAwayGoesOnWhenFocusedFails(t *testing.T) {
	c, wm, projects := resolving(t)
	wm.FocusedErr = errors.New("no compositor")
	p, err := c.ProjectFor(t.Context(), projects, core.Ask{Away: true})
	if err != nil || p.Name != "other" {
		t.Errorf("ProjectFor = %q, %v; want other, the last project", p.Name, err)
	}
}

// Without Away, a directory in no project fails with the no-project status
// and says which directory and what to give instead; the focused window and
// the last project are not asked.
func TestProjectForADirectoryInNoProjectFailsWithoutAway(t *testing.T) {
	c, _, projects := resolving(t)
	nowhere := t.TempDir()
	_, err := c.ProjectFor(t.Context(), projects, core.Ask{Dir: nowhere})
	if !errors.Is(err, core.ErrNoProject) {
		t.Fatalf("ProjectFor(%s) err = %v; want core.ErrNoProject", nowhere, err)
	}
	if msg := err.Error(); !strings.Contains(msg, nowhere) || !strings.Contains(msg, "-p <project>") {
		t.Errorf("err = %q; want it to name %s and -p", msg, nowhere)
	}
}

// A directory is in the project whose path differs from it only by a
// symlink, on either side: a shell keeps the path through the link, a tmux
// window starts in the directory behind it.
func TestProjectForADirectoryIsInAProjectThroughASymlink(t *testing.T) {
	behind := t.TempDir()
	sub := filepath.Join(behind, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(behind, link); err != nil {
		t.Fatal(err)
	}
	c := &core.Core{}
	for _, d := range []struct{ path, dir string }{
		{path: link, dir: behind},
		{path: link, dir: sub},
		{path: behind, dir: link},
		{path: behind, dir: filepath.Join(link, "sub")},
	} {
		projects := []core.Project{placed("demo", d.path)}
		if p, err := c.ProjectFor(t.Context(), projects, core.Ask{Dir: d.dir}); err != nil || p.Name != "demo" {
			t.Errorf("project of %s with path %s = %q, %v; want demo", d.dir, d.path, p.Name, err)
		}
	}
}

// A project nested in another through a symlink still wins over the outer
// project that holds the directory as text: the longest match wins.
func TestProjectForANestedProjectReachedThroughASymlinkWins(t *testing.T) {
	mono := t.TempDir()
	nested := filepath.Join(mono, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(nested, link); err != nil {
		t.Fatal(err)
	}
	projects := []core.Project{placed("mono", mono), placed("nested", link)}
	if p, err := (&core.Core{}).ProjectFor(t.Context(), projects, core.Ask{Dir: nested}); err != nil || p.Name != "nested" {
		t.Errorf("project of %s = %q, %v; want nested", nested, p.Name, err)
	}
}

// A link's path is on its host: a directory here of the same name is some
// other checkout.
func TestProjectForSkipsALinksPath(t *testing.T) {
	dir := t.TempDir()
	linked := placed("linked", dir)
	linked.Remote = &revier.Link{Host: "box", Project: "linked"}
	_, err := (&core.Core{}).ProjectFor(t.Context(), []core.Project{linked}, core.Ask{Dir: dir})
	if !errors.Is(err, core.ErrNoProject) {
		t.Errorf("ProjectFor(%s) err = %v; want core.ErrNoProject, not the link", dir, err)
	}
}

// A directory that does not exist yet is still placed, by its path as text.
func TestProjectForPlacesADirectoryThatDoesNotExistYet(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-yet")
	projects := []core.Project{placed("demo", root)}
	p, err := (&core.Core{}).ProjectFor(t.Context(), projects, core.Ask{Dir: filepath.Join(root, "sub")})
	if err != nil || p.Name != "demo" {
		t.Errorf("ProjectFor = %q, %v; want demo", p.Name, err)
	}
}
