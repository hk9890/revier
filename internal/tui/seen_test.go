package tui_test

import (
	"errors"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// forget removes what the surface wrote, so a test sees whether it writes
// again.
func forget(t *testing.T, root string) {
	t.Helper()
	if err := os.Remove(session.SeenPath(root)); err != nil {
		t.Fatal(err)
	}
}

// A check that cannot read what the end before left writes nothing over it:
// what that end left is the one copy of what it had open.
func TestAFailedCheckLeavesWhatTheEndBeforeLeft(t *testing.T) {
	root := t.TempDir()
	left := []byte("not a session")
	if err := os.WriteFile(session.SeenPath(root), left, 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := seenWorld(t, root)
	for range 3 {
		m = seenSurvey(m)
	}
	if got, err := os.ReadFile(session.SeenPath(root)); err != nil || string(got) != string(left) {
		t.Errorf("seen.toml = %q, %v, want it left as the end before wrote it", got, err)
	}
}

// A survey that finds the same as the last one written writes nothing; one
// that finds a target opened writes again.
func TestTheSurfaceWritesWhatItSawOnlyWhenItChanges(t *testing.T) {
	root := t.TempDir()
	m, rt := seenWorld(t, root)
	m = seenSurvey(seenSurvey(m))
	s, ok := seenOnDisk(t, root)
	if !ok || len(s.Projects) != 1 || s.Projects[0].Name != "project-01" {
		t.Fatalf("seen = %+v, %v, want project-01's home", s, ok)
	}

	forget(t, root)
	m = seenSurvey(m)
	if _, ok := seenOnDisk(t, root); ok {
		t.Error("the same survey wrote again, want nothing written")
	}

	rt.Add("session:project-00", "kitty")
	seenSurvey(m)
	if s, ok := seenOnDisk(t, root); !ok || len(s.Projects) != 2 {
		t.Errorf("seen = %+v, %v, want both projects once project-00 opened", s, ok)
	}
}

// Nothing open is never written, and neither is a survey in which a host
// here could not list.
func TestTheSurfaceWritesNothingEmptyOrWhileAHostCannotList(t *testing.T) {
	root := t.TempDir()
	m, rt := seenWorld(t, root)
	rt.Remove(revier.TargetRef{Host: "rt", ID: "2"})
	m = seenSurvey(seenSurvey(m))
	if s, ok := seenOnDisk(t, root); ok {
		t.Errorf("seen = %+v with nothing open, want nothing written", s)
	}

	rt.Add("session:project-01", "kitty")
	m = seenSurvey(m)
	forget(t, root)
	rt.Add("session:project-00", "kitty")
	rt.SetInstancesErr(errors.New("went away"))
	seenSurvey(m)
	if s, ok := seenOnDisk(t, root); ok {
		t.Errorf("seen = %+v while rt cannot list, want nothing written", s)
	}
}

// Nothing is written before the check for an end with no shutdown has run,
// which waits for a survey in which every host here listed, and nothing
// while a walk runs.
func TestTheSurfaceWritesNothingBeforeItsRecoveryCheckOrDuringAWalk(t *testing.T) {
	root := t.TempDir()
	m, rt := seenWorld(t, root)
	rt.SetInstancesErr(errors.New("went away"))
	m = seenSurvey(seenSurvey(m))
	rt.SetInstancesErr(nil)
	m = seenSurvey(m)
	if s, ok := seenOnDisk(t, root); ok {
		t.Errorf("seen = %+v on the survey the check ran on, want nothing written yet", s)
	}
	m = seenSurvey(m)
	if _, ok := seenOnDisk(t, root); !ok {
		t.Fatal("nothing written once the check ran, want what the survey saw")
	}

	forget(t, root)
	m, _ = press(m, "alt+q")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	m, _ = press(m, "enter")
	rt.Add("session:project-00", "kitty")
	seenSurvey(m)
	if s, ok := seenOnDisk(t, root); ok {
		t.Errorf("seen = %+v while the shutdown runs, want nothing written", s)
	}
}

// A survey that holds less than the last one written waits for the next
// survey to agree: a terminal killed before the surface lists as closed.
func TestAShrinkIsWrittenOnlyWhenTheNextSurveyAgrees(t *testing.T) {
	root := t.TempDir()
	m, rt := seenWorld(t, root)
	zero := rt.Add("session:project-00", "kitty")
	m = seenSurvey(seenSurvey(m))
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 2 {
		t.Fatalf("seen = %+v, want both projects", s)
	}

	rt.Remove(zero)
	m = seenSurvey(m)
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 2 {
		t.Errorf("seen = %+v after one survey without project-00, want both still", s)
	}
	seenSurvey(m)
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 1 {
		t.Errorf("seen = %+v after two, want project-01 alone", s)
	}
}

// A linked host's answer is laid over the listing of the last survey here,
// so it is no second look at a shrink: what the surface last saw keeps the
// target until the next survey of this machine agrees.
func TestALinkedHostsAnswerDoesNotConfirmAShrink(t *testing.T) {
	root := t.TempDir()
	rt, _, c, local := world(t, 2)
	rt.Remove(revier.TargetRef{Host: "rt", ID: "1"})
	rt.Add("session:project-01", "kitty")
	zero := rt.Add("session:project-00", "kitty")
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c.Remotes = map[string]revier.Remote{"buildbox": remote}
	projects := append(remoteOnDisk(t, "alpha"), local...)
	m, now := clocked(tui.New(c, projects, root, &config.Config{}, time.Nanosecond, theme.Default(), "").StaticCursors())
	m = resize(m, 150, 30)
	// One survey here and one round to buildbox, with every command they
	// answer with run, so what they keep is written.
	round := func() {
		t.Helper()
		var cmd tea.Cmd
		m, cmd = deliver(m, m.Survey())
		m, cmd = deliver(m, cmd)
		runAll(cmd)
	}

	round()
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 2 {
		t.Fatalf("seen = %+v, want both projects", s)
	}

	rt.Remove(zero)
	*now = now.Add(5 * time.Second)
	round()
	if len(remote.Asked) != 2 {
		t.Fatalf("buildbox was asked %d times, want a second round after the shrink", len(remote.Asked))
	}
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 2 {
		t.Errorf("seen = %+v after a survey without project-00 and buildbox's answer, want both still", s)
	}
	round()
	if s, _ := seenOnDisk(t, root); len(s.Projects) != 1 {
		t.Errorf("seen = %+v after a second survey here, want project-01 alone", s)
	}
}
