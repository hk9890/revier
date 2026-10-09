// Layer L2: each use case against real files under a temporary root and a
// ledger in memory. No process is started and no surface is built. The origin
// a checkout records and the trust of its mise configuration need git and
// mise, and are layer L4's, in cmd/revier.
package app_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// ledger is a ledger in memory: the state, and every event recorded.
type ledger struct {
	st     state.State
	events []revier.Event
}

func (l *ledger) State() *state.State { st := l.st; return &st }

func (l *ledger) Update(apply func(*state.State) bool) *state.State {
	apply(&l.st)
	return l.State()
}

func (l *ledger) Record(e revier.Event) { l.events = append(l.events, e) }

// noTools empties PATH, so a project for a directory that is there starts
// neither git nor mise: this layer starts no process, and mise's trust list
// is left as it was.
func noTools(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// created is a configuration root with one project in it, for a directory
// that is there.
func created(t *testing.T, name revier.ProjectName) (root string, p core.Project) {
	t.Helper()
	noTools(t)
	root = t.TempDir()
	p, err := app.CreateProject(root, nil, name, t.TempDir(), "", io.Discard)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return root, p
}

func TestCreateProjectNamesTheProjectAfterItsDirectory(t *testing.T) {
	noTools(t)
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "my project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := app.CreateProject(root, nil, "", dir, "", io.Discard)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.Name != "my_project" || p.Path != dir || p.File != config.ProjectFile(root, "my_project") {
		t.Errorf("project %q at %s in %s; want my_project at %s in its own file", p.Name, p.Path, p.File, dir)
	}
}

// A directory that is not there is the caller's to clone: the project records
// the repository it was given. One that is there records its own origin, and
// a directory that is no checkout has none.
func TestCreateProjectRecordsTheRepositoryOnlyOfAMissingDirectory(t *testing.T) {
	const url = "git@github.com:hk9890/demo.git"
	noTools(t)
	root := t.TempDir()

	missing, err := app.CreateProject(root, nil, "", filepath.Join(t.TempDir(), "demo"), url, io.Discard)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if missing.GitURL != url {
		t.Errorf("git_url = %q, want the repository given for a directory that is not there", missing.GitURL)
	}
	there, err := app.CreateProject(root, []core.Project{missing}, "there", t.TempDir(), url, io.Discard)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if there.GitURL != "" {
		t.Errorf("git_url = %q, want none: the directory is there and has no origin", there.GitURL)
	}
}

func TestCreateProjectRefusesWhatCanCreateRefuses(t *testing.T) {
	root, p := created(t, "demo")

	_, err := app.CreateProject(root, []core.Project{p}, "other", p.Path, "", io.Discard)
	if err == nil || !strings.Contains(err.Error(), `already project "demo"`) {
		t.Errorf("err = %v, want the directory refused as demo's", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "other")); !os.IsNotExist(err) {
		t.Errorf("project file: %v, want none written", err)
	}
}

func TestLinkProjectTakesTheProjectsNameWhenNoneIsGiven(t *testing.T) {
	root := t.TempDir()
	on := revier.Project{Name: "far", Path: "/srv/far"}

	p, err := app.LinkProject(root, "", "buildbox", on)
	if err != nil {
		t.Fatalf("LinkProject: %v", err)
	}
	if p.Name != "far" || p.Remote == nil || p.Remote.Host != "buildbox" {
		t.Errorf("link = %+v, want far on buildbox", p.Project)
	}
	if _, err := app.LinkProject(root, "far", "buildbox", on); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want the taken name refused", err)
	}
	named, err := app.LinkProject(root, "build", "buildbox", on)
	if err != nil || named.Name != "build" {
		t.Errorf("LinkProject = %q, %v; want a second link under the name given", named.Name, err)
	}
}

// A rename moves the project in the file, in the state and in every saved
// session.
func TestRenameProjectMovesTheNameInAllThreeStores(t *testing.T) {
	root, _ := created(t, "revier")
	stateRoot := t.TempDir()
	ref := revier.TargetRef{Host: "rt", ID: "1"}
	l := &ledger{st: state.State{Current: "revier", Bound: map[revier.ProjectName]map[revier.TargetName]revier.TargetRef{"revier": {"home": ref}}}}
	saved := session.Session{Name: "before", At: time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC), Current: "revier", Projects: []session.Project{{Name: "revier"}}}
	if _, _, err := session.Save(stateRoot, saved); err != nil {
		t.Fatal(err)
	}

	p, err := app.RenameProject(root, stateRoot, l, "revier", "rv", nil)
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if p.Name != "rv" || p.File != config.ProjectFile(root, "rv") {
		t.Errorf("project %q in %s, want rv in its own file", p.Name, p.File)
	}
	if l.st.Current != "rv" || l.st.Bound["rv"]["home"] != ref || len(l.st.Bound["revier"]) != 0 {
		t.Errorf("state = %+v, want the current project and the binding under rv", l.st)
	}
	if got, err := session.Load(stateRoot, "before"); err != nil || got.Current != "rv" || got.Projects[0].Name != "rv" {
		t.Errorf("session = %+v, %v; want rv", got, err)
	}
}

// The saved sessions cannot fail a rename: the file and the state are moved,
// and the project comes back renamed.
func TestRenameProjectIsDoneWhenTheSessionStoreFails(t *testing.T) {
	root, _ := created(t, "revier")
	stateRoot := t.TempDir()
	// A file where the directory of the sessions belongs: it cannot be listed.
	if err := os.WriteFile(session.Dir(stateRoot), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.Rename(stateRoot, "revier", "rv"); err == nil {
		t.Fatal("the session store did not fail; the test proves nothing")
	}
	l := &ledger{st: state.State{Current: "revier"}}

	p, err := app.RenameProject(root, stateRoot, l, "revier", "rv", nil)
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if p.Name != "rv" {
		t.Errorf("project = %q, want rv", p.Name)
	}
	if _, err := os.Stat(config.ProjectFile(root, "revier")); !os.IsNotExist(err) {
		t.Errorf("old file: %v, want it gone", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "rv")); err != nil {
		t.Errorf("new file: %v", err)
	}
	if l.st.Current != "rv" {
		t.Errorf("current = %q, want rv", l.st.Current)
	}
}

// A file that cannot be moved changes no store.
func TestRenameProjectChangesNothingWhenTheFileIsRefused(t *testing.T) {
	root, _ := created(t, "revier")
	l := &ledger{st: state.State{Current: "revier"}}

	if _, err := app.RenameProject(root, t.TempDir(), l, "revier", "a b", nil); err == nil {
		t.Fatal("a name with a space was not refused")
	}
	if l.st.Current != "revier" {
		t.Errorf("current = %q, want the state left as it was", l.st.Current)
	}
}

// An action of a project on this machine is one launch, written before the
// command runs, and one event once it has succeeded, both stamped with the
// moment of the plan.
func TestAnActionIsOneLaunchAndOneEventStampedWithItsStart(t *testing.T) {
	l := &ledger{}
	c := &core.Core{Ledger: l}
	p := core.Project{Project: revier.Project{Name: "demo", Path: "/src/demo"}}
	before := time.Now()

	act, err := app.PlanAction(c, p, "sync", []string{"git", "-C", "{{.Path}}", "pull"})
	if err != nil {
		t.Fatalf("PlanAction: %v", err)
	}
	if strings.Join(act.Argv, " ") != "git -C /src/demo pull" || act.Dir != "/src/demo" {
		t.Errorf("command = %q in %q, want the action rendered in the checkout", act.Argv, act.Dir)
	}
	launch := l.st.Launch
	if launch == nil || launch.Project != "demo" || launch.Target != "" || launch.At.Before(before) {
		t.Fatalf("launch = %+v, want the action's launch written by the plan", launch)
	}
	if l.st.Current != "demo" {
		t.Errorf("current = %q, want the action's project", l.st.Current)
	}
	if len(l.events) != 0 {
		t.Fatalf("events = %+v, want none before the command has ended", l.events)
	}

	act.Done(nil)

	want := revier.Event{Time: launch.At, Kind: revier.EventAction, Project: "demo", Action: "sync"}
	if len(l.events) != 1 || l.events[0] != want {
		t.Errorf("events = %+v, want %+v", l.events, want)
	}
	if *l.st.Launch != *launch {
		t.Errorf("launch = %+v, want the one launch %+v", l.st.Launch, launch)
	}
}

func TestAnActionThatFailedIsNoEvent(t *testing.T) {
	l := &ledger{}
	p := core.Project{Project: revier.Project{Name: "demo", Path: "/src/demo"}}
	act, err := app.PlanAction(&core.Core{Ledger: l}, p, "sync", []string{"false"})
	if err != nil {
		t.Fatalf("PlanAction: %v", err)
	}

	act.Done(errors.New("exit status 1"))

	if len(l.events) != 0 {
		t.Errorf("events = %+v, want none", l.events)
	}
}

// A link's action runs on its host: it opens no window here, so it writes no
// launch, and it is the event of the revier there, so it is none here.
func TestALinksActionIsNoLaunchAndNoEventHere(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"ssh", "buildbox", "revier", "run", "sync", "-p", "far"}
	l := &ledger{}
	c := &core.Core{Ledger: l, Remotes: map[string]revier.Remote{"buildbox": remote}}
	p := core.PrepareProject(revier.Project{Name: "far", Remote: &revier.Link{Host: "buildbox", Project: "far"}})

	act, err := app.PlanAction(c, p, "sync", nil)
	if err != nil {
		t.Fatalf("PlanAction: %v", err)
	}
	act.Done(nil)

	if strings.Join(act.Argv, " ") != strings.Join(remote.RunArgv, " ") || act.Dir != "" {
		t.Errorf("command = %q in %q, want the host's in no directory", act.Argv, act.Dir)
	}
	if l.st.Launch != nil || len(l.events) != 0 {
		t.Errorf("launch = %+v, events = %+v; want neither", l.st.Launch, l.events)
	}
}

// An action that cannot be planned writes nothing.
func TestAnActionThatRunsNothingWritesNoLaunch(t *testing.T) {
	l := &ledger{}
	p := core.Project{Project: revier.Project{Name: "demo", Path: "/src/demo"}}

	if _, err := app.PlanAction(&core.Core{Ledger: l}, p, "sync", nil); err == nil {
		t.Fatal("an action that runs nothing was planned")
	}
	if l.st.Launch != nil {
		t.Errorf("launch = %+v, want none", l.st.Launch)
	}
}
