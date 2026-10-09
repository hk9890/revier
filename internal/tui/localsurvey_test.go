package tui_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// surveyedHere applies the survey of this machine alone, as it answers while
// a linked host has yet to.
func surveyedHere(m tui.Model) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(m.Survey()())
	return next.(tui.Model), cmd
}

// besideALink is the world of three projects, the last one open, beside a
// link to buildbox.
func besideALink(t *testing.T) (tui.Model, *hosttest.FakeRuntime, *hosttest.FakeRemote) {
	t.Helper()
	rt, _, c, local := world(t, 3)
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c.Remotes = map[string]revier.Remote{"buildbox": remote}
	projects := append(remoteOnDisk(t, "alpha"), local...)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Millisecond, theme.Default(), "").StaticCursors()
	return resize(m, 150, 30), rt, remote
}

// A project closed here is off its row at the next survey of this machine,
// with no linked host asked: a host that is slow or gone holds no row here
// up (decisions.md D114).
func TestARowHereChangesBeforeALinkedHostAnswers(t *testing.T) {
	m, rt, remote := besideALink(t)
	th := theme.Default()

	m, _ = surveyedHere(m)
	if row := rows(m)[0]; !strings.Contains(row, "project-02") || !strings.Contains(row, th.Glyphs.Running) {
		t.Fatalf("row = %q, want project-02 open", row)
	}

	open, err := rt.Instances(context.Background())
	if err != nil || len(open) != 1 {
		t.Fatalf("instances = %v, %v; want the one workspace", open, err)
	}
	rt.Remove(open[0].Ref)
	m, _ = surveyedHere(m)

	for _, row := range rows(m) {
		if strings.Contains(row, "project-02") && strings.Contains(row, th.Glyphs.Running) {
			t.Errorf("row = %q, want project-02 closed", row)
		}
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %d times by the surveys of this machine, want none", len(remote.Asked))
	}
}

// A link's pane says the host is still to answer, and not that the checkout
// is missing: the survey of this machine says nothing about it.
func TestALinkSaysSurveyingUntilItsHostAnswers(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := tui.New(c, remoteOnDisk(t, "alpha"), stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").StaticCursors()
	m, _ = surveyedHere(resize(m, 140, 30))

	if body := pane(m); !strings.Contains(body, "surveying") || strings.Contains(body, "not available") || strings.Contains(body, "clone") {
		t.Errorf("pane = %q, want surveying and nothing about the checkout", body)
	}

	next, _ := m.Update(m.AskRemotes()())
	if body := pane(next.(tui.Model)); strings.Contains(body, "surveying") {
		t.Errorf("pane = %q, want the host's answer", body)
	}
}

// A project written on the project screen stays as written when a linked host
// answers: the answer is laid over the last survey of this machine, which
// takes the change as the rows do.
func TestAHostsAnswerKeepsAProjectAsItWasJustWritten(t *testing.T) {
	root := configRoot(t, sharedTargets)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"demo.toml": demoProject, "far.toml": remoteFile} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	remote := hosttest.NewRemote("buildbox", hostSays("far", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := tui.New(c, projects, stateWith(t, nil), cfg, time.Second, theme.Default(), "").StaticCursors()
	m = resize(survey(m), 140, 60)
	asked := m.AskRemotes() // a round that answers after the file is written

	m, _ = press(m, "alt+e")
	m = downs(m, 2) // git url
	m, _ = press(m, "enter")
	m = typeInto(m, "git@github.com:me/demo.git")
	m, _ = press(m, "enter")
	m, _ = press(m, "esc")
	m = run(m, asked)

	if body := pane(m); !strings.Contains(body, "git@github.com:me/demo.git") {
		t.Errorf("pane = %q, want the git url the screen just wrote", body)
	}
}

// A link renamed on the project screen keeps what its host said last: the
// next survey of this machine lays it over the row under the new name, and
// the pane does not go back to waiting for the host.
func TestARenamedLinkKeepsWhatItsHostSaid(t *testing.T) {
	root := configRoot(t, sharedTargets)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "far.toml"), []byte(remoteFile), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	remote := hosttest.NewRemote("buildbox", hostSays("far", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := tui.New(c, projects, stateWith(t, nil), cfg, time.Second, theme.Default(), "").StaticCursors()
	m = resize(survey(m), 140, 60)

	m, _ = press(m, "alt+e")
	m, _ = press(m, "enter")
	m = typeInto(clearField(m), "farther")
	m, _ = press(m, "enter")
	m, _ = press(m, "esc")
	m, _ = surveyedHere(m)

	if body := pane(m); !strings.Contains(body, "farther") || strings.Contains(body, "surveying") {
		t.Errorf("pane = %q, want farther with its host's last answer", body)
	}
}

// The linked hosts are asked one round at a time: a survey that answers
// while a round runs starts no second one, and the first survey after it
// starts the next.
func TestTheLinkedHostsAreAskedOneRoundAtATime(t *testing.T) {
	m, _, remote := besideALink(t)

	m, first := surveyedHere(m)
	m, second := surveyedHere(m)
	m, _ = deliver(m, first)
	m, _ = deliver(m, second)
	if len(remote.Asked) != 1 {
		t.Fatalf("the host was asked %d times over two surveys, want one round", len(remote.Asked))
	}

	m, third := surveyedHere(m)
	deliver(m, third)
	if len(remote.Asked) != 2 {
		t.Errorf("the host was asked %d times, want a second round once the first answered", len(remote.Asked))
	}
}

// Each linked host is asked by itself: one that has not answered holds
// neither the rows of a link on another host nor that host's next round.
func TestASlowHostHoldsNoOtherHostsLinkUp(t *testing.T) {
	dir := t.TempDir()
	for name, host := range map[string]string{"alpha": "buildbox", "beta": "slowbox"} {
		body := strings.ReplaceAll(remoteFile, "buildbox", host)
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := config.LoadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	fast := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	slow := hosttest.NewRemote("slowbox", hostSays("beta", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": fast, "slowbox": slow}}
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Millisecond, theme.Default(), "").StaticCursors()

	// The survey starts a round of each host; only buildbox answers.
	m, rounds := surveyedHere(resize(m, 140, 30))
	m = run(m, m.AskRemotes("buildbox"))
	if body := pane(m); strings.Contains(body, "surveying") {
		t.Errorf("pane of alpha = %q, want what buildbox said", body)
	}
	if body := pane(downs(m, 1)); !strings.Contains(body, "surveying") {
		t.Errorf("pane of beta = %q, want surveying: slowbox has not answered", body)
	}

	m, next := surveyedHere(m)
	deliver(m, next)
	if len(fast.Asked) != 2 || len(slow.Asked) != 0 {
		t.Errorf("asked buildbox %d times and slowbox %d, want a second round of buildbox alone", len(fast.Asked), len(slow.Asked))
	}
	deliver(m, rounds)
	if len(slow.Asked) != 1 {
		t.Errorf("asked slowbox %d times, want its one round", len(slow.Asked))
	}
}

// linkOpenHere is the world of one open project above a link to buildbox
// whose workspace is open here, and whose agent there needs the user. The
// survey of this machine alone leaves the local project first.
func linkOpenHere(t *testing.T) tui.Model {
	t.Helper()
	rt := openHere("alpha")
	rt.Add("session:local", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude"})
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusAttention))
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote},
		Probes: []revier.AgentProbe{&hosttest.FakeProbe{
			Harness: "claude", Marker: "claude",
			State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning},
		}}}
	local := core.Prepare([]revier.Project{{Name: "local", Path: "/p/local", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:local", Launch: []string{"x"}, Match: revier.Match{Title: "^session:local$"}}},
	}}})
	projects := append(local, remoteOnDisk(t, "alpha")...)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").StaticCursors()
	return resize(m, 150, 30)
}

// The top row is the project that needs the user most only once the linked
// hosts have answered: a cursor nobody moved goes to the link an answer put
// first, as it did when one survey asked everybody. After that the user's own
// selection wins.
func TestACursorNobodyMovedGoesToTheLinkItsHostPutsFirst(t *testing.T) {
	m, _ := surveyedHere(linkOpenHere(t))
	wantSelected(t, m, "local", "the first row before the host answers")

	m = run(m, m.AskRemotes())
	wantSelected(t, m, "alpha", "its agent needs the user, and nobody chose the row the cursor was on")

	m, _ = press(m, "down")
	m = survey(m)
	wantSelected(t, m, "local", "the user moved to it")
}

// A cursor the user moved before a host answered stays on its project.
func TestACursorTheUserMovedStaysWhenAHostAnswers(t *testing.T) {
	m, _ := surveyedHere(linkOpenHere(t))
	m, _ = press(m, "down")
	wantSelected(t, m, "alpha", "the second row, before its host answers")

	m = run(m, m.AskRemotes())
	wantSelected(t, m, "alpha", "the user moved to it")
	if row := rows(m)[0]; !strings.Contains(row, "alpha") {
		t.Errorf("first row = %q, want alpha once its agent needs the user", row)
	}
}
