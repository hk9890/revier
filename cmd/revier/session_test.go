package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// AGENTS counts every agent the session records, not only the ones with a
// conversation to resume: an agent with none still restores, empty. REASON
// says why the session was saved. The last line names the save a plain
// restore opens.
func TestSessionListCountsEveryAgent(t *testing.T) {
	root := t.TempDir()
	_, _, err := session.Save(root, session.Session{
		Name: "t1", Reason: session.ReasonShutdown, At: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
		Projects: []session.Project{{Name: "demo", Targets: []session.Target{{Name: "home", Agents: []session.Agent{
			{Harness: "claude"}, {Harness: "claude"}, {Harness: "claude", Session: "abc"},
		}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdSessionList(&app{stateRoot: root, out: &out}, nil); err != nil {
		t.Fatalf("session list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("list printed %q, want a header, one session and what a restore opens", out.String())
	}
	if fields := strings.Fields(lines[1]); fields[len(fields)-1] != "3" {
		t.Errorf("row = %q, want AGENTS 3", lines[1])
	}
	if !strings.Contains(lines[0], "REASON") || !strings.Contains(lines[1], "shutdown") {
		t.Errorf("list printed %q, want a REASON column that says shutdown", out.String())
	}
}

// saveAt writes a session of demo's targets, saved for reason at the given
// hour of 2026-10-16, and returns its id.
func saveAt(t *testing.T, root string, hour int, reason session.Reason, targets ...revier.TargetName) string {
	t.Helper()
	var ts []session.Target
	for _, name := range targets {
		ts = append(ts, session.Target{Name: name})
	}
	s, _, err := session.Save(root, session.Session{Reason: reason, At: time.Date(2026, 10, 16, hour, 0, 0, 0, time.UTC), Projects: []session.Project{{Name: "demo", Targets: ts}}})
	if err != nil {
		t.Fatal(err)
	}
	return s.ID
}

// restoreApp is an app over demo with nothing open, whose sessions are under
// root.
func restoreApp(t *testing.T, root string) *app {
	t.Helper()
	return withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: root, core: &core.Core{Runtime: hosttest.NewRuntime("rt")}}, &state.State{})
}

// A plain restore opens the full shutdown's save, not the close saved after
// it, which holds less (decisions.md D149).
func TestSessionRestoreWithoutRefSkipsAProjectClose(t *testing.T) {
	root := t.TempDir()
	saveAt(t, root, 22, session.ReasonShutdown, "home", "diff")
	saveAt(t, root, 23, session.ReasonProjectClosed, "home")
	a := restoreApp(t, root)

	out := output(t, a, func() error { return cmdSessionRestore(context.Background(), a, []string{"--dry-run"}) })
	if !strings.Contains(out, "diff") {
		t.Errorf("dry run printed %q, want the targets of the shutdown's save", out)
	}
}

// With only closes saved, a plain restore opens none of them and says where
// they are, as an empty store does: no failure.
func TestSessionRestoreWhenOnlyClosesWereSaved(t *testing.T) {
	root := t.TempDir()
	saveAt(t, root, 23, session.ReasonProjectClosed, "home")
	a := restoreApp(t, root)

	out := output(t, a, func() error { return cmdSessionRestore(context.Background(), a, nil) })
	if !strings.Contains(out, "no saved session holds every open project") {
		t.Errorf("restore printed %q, want it to say no save holds every open project", out)
	}
}

// The newest row is a close; the list's last line names the save a plain
// restore opens instead.
func TestSessionListNamesWhatARestoreOpens(t *testing.T) {
	root := t.TempDir()
	shut := saveAt(t, root, 22, session.ReasonShutdown, "home", "diff")
	saveAt(t, root, 23, session.ReasonProjectClosed, "home")
	a := &app{stateRoot: root}

	out := output(t, a, func() error { return cmdSessionList(a, nil) })
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; last != "revier session restore opens "+shut {
		t.Errorf("last line = %q, want it to name %s", last, shut)
	}
}
