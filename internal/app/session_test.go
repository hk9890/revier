package app_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// desktop is a core whose runtime holds the workspace, with an agent in it,
// and the notes of demo, the project the ledger focused last.
func desktop(t *testing.T) (*core.Core, *hosttest.FakeRuntime, core.Report, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:demo", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "claude", Vars: map[string]string{"session": "abc-123"}})
	rt.Add("notes:demo", "kitty")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.NewResumableProbe("claude", "claude")}, Ledger: &ledger{st: state.State{Current: "demo"}}}
	projects := []core.Project{core.PrepareProject(revier.Project{Name: "demo", Path: "/src/demo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "session:demo", Match: revier.Match{Title: "^session:demo$"}}},
		{Name: "notes", Runtime: &revier.Realization{Name: "notes:demo", Launch: []string{"less"}, Match: revier.Match{Title: "^notes:demo$"}}},
	}})}
	return c, rt, surveyed(t, c, projects), projects
}

func surveyed(t *testing.T, c *core.Core, projects []core.Project) core.Report {
	t.Helper()
	r, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return r
}

// A save with nothing open is refused with a normal outcome, not written: an
// empty session would become the newest, the one a plain restore opens. With
// something open, the save is written, stamped as asked, and its current
// project is the ledger's.
func TestSaveSessionRefusesAnEmptySave(t *testing.T) {
	root := t.TempDir()
	empty := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	_, _, _, projects := desktop(t)
	if _, _, _, err := app.SaveSession(context.Background(), empty, root, surveyed(t, empty, projects), "before", time.Now()); !errors.Is(err, app.ErrNothingOpen) {
		t.Errorf("save = %v, want ErrNothingOpen", err)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want none written", all)
	}

	c, _, report, _ := desktop(t)
	at := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)
	stored, path, _, err := app.SaveSession(context.Background(), c, root, report, "before", at)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if stored.Name != "before" || !stored.At.Equal(at) || stored.Current != "demo" || len(stored.Projects) == 0 {
		t.Errorf("stored = %+v, want the name, the moment and the ledger's current project", stored)
	}
	if stored.Reason != session.ReasonSaved {
		t.Errorf("reason = %q, want a save by hand", stored.Reason)
	}
	if stored.Conversations() != 1 {
		t.Errorf("conversations = %d, want the agent's recorded", stored.Conversations())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path %s: %v, want the file written", path, err)
	}
}

// A shutdown saves what is open, once: a second close of the same desktop
// finds the newest session already holds it, whatever its reason. Each
// session written carries the reason its caller gave.
func TestSaveChangedSavesOnlyWhatChanged(t *testing.T) {
	c, rt, report, projects := desktop(t)
	root := t.TempDir()
	at := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

	first, saved, _, err := app.SaveChanged(context.Background(), c, root, report, session.ReasonShutdown, at)
	if err != nil || !saved {
		t.Fatalf("first save = %v, %v; want a save", saved, err)
	}
	if first.Reason != session.ReasonShutdown {
		t.Errorf("first reason = %q, want shutdown", first.Reason)
	}
	if _, saved, _, _ := app.SaveChanged(context.Background(), c, root, surveyed(t, c, projects), session.ReasonProjectClosed, at.Add(time.Minute)); saved {
		t.Error("second save wrote a session, want the same desktop left unsaved")
	}
	rt.Remove(report.Views[0].Targets[1].Ref)
	second, saved, _, err := app.SaveChanged(context.Background(), c, root, surveyed(t, c, projects), session.ReasonProjectClosed, at.Add(2*time.Minute))
	if err != nil || !saved || second.ID == first.ID {
		t.Errorf("save after a change = %v, %v, id %q; want a new session", saved, err, second.ID)
	}
	if second.Reason != session.ReasonProjectClosed {
		t.Errorf("second reason = %q, want project closed", second.Reason)
	}
	if all, _ := session.List(root); len(all) != 2 {
		t.Errorf("sessions = %d, want 2", len(all))
	}
}

// Nothing open is nothing to save, and no failure.
func TestSaveChangedSavesNothingWhenNothingIsOpen(t *testing.T) {
	c := &core.Core{Runtime: hosttest.NewRuntime("rt")}
	_, _, _, projects := desktop(t)
	if _, saved, _, err := app.SaveChanged(context.Background(), c, t.TempDir(), surveyed(t, c, projects), session.ReasonShutdown, time.Now()); saved || err != nil {
		t.Errorf("save = %v, %v; want none", saved, err)
	}
}

// A save its caller cancelled writes nothing and fails with the cancel: what
// the cancel cut short would be the newest session, the one a plain restore
// opens. A shutdown takes the failure as its own and closes nothing.
func TestSaveChangedSavesNothingWhenTheCallerCancels(t *testing.T) {
	c, _, report, _ := desktop(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, saved, _, err := app.SaveChanged(ctx, c, root, report, session.ReasonShutdown, time.Now())
	if !errors.Is(err, context.Canceled) || saved {
		t.Errorf("save = %v, %v; want the cancel and nothing saved", saved, err)
	}
	if all, _ := session.List(root); len(all) != 0 {
		t.Errorf("sessions = %+v, want none written", all)
	}
}

// A store that cannot be read is the save's failure, which a shutdown takes
// as its own and closes nothing; it is not taken as no session at all.
func TestSaveChangedFailsWhenTheStoreCannotBeRead(t *testing.T) {
	c, _, report, _ := desktop(t)
	root := t.TempDir()
	if err := os.WriteFile(session.Dir(root), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, saved, _, err := app.SaveChanged(context.Background(), c, root, report, session.ReasonShutdown, time.Now())
	if err == nil || errors.Is(err, session.ErrNoSession) || saved {
		t.Errorf("save = %v, %v; want the read error and nothing saved", saved, err)
	}
}
