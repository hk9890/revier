//go:build live

// Layer L4 for `revier shutdown`: close what a real tmux server holds, and
// save the session first, with no window host.
package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/session"
)

// agentStatus makes the fake claude list every agent pane in the status.
func agentStatus(t *testing.T, status string) {
	t.Helper()
	for _, pid := range agentPanes(t) {
		body := `{"pid": ` + pid + `, "kind": "interactive", "sessionId": "abc-123", "status": "` + status + `"}`
		if err := os.WriteFile(filepath.Join(sessionsDir, pid+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func savedSessions(t *testing.T) []session.Session {
	t.Helper()
	all, err := session.List(filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return all
}

// A shutdown saves what is open, closes all of it, and a second shutdown of
// the same desktop saves nothing new.
func TestShutdownSavesThenClosesEverything(t *testing.T) {
	work(t)
	agentStatus(t, "idle")

	if plan := capture(t, "shutdown", "--dry-run"); !strings.Contains(plan, "home") || !strings.Contains(plan, "1 agent in it") {
		t.Errorf("dry run printed %q, want home and its agent", plan)
	}
	if got := workspaces(t); !strings.Contains(got, "work") {
		t.Fatalf("the dry run closed something: %q", got)
	}

	out := capture(t, "shutdown")
	if !strings.Contains(out, "saved: 1 project, 2 targets") || !strings.Contains(out, "closed 2, 0 still open") {
		t.Errorf("shutdown printed %q, want the save and both targets closed", out)
	}
	if got := workspaces(t); got != "" {
		t.Errorf("workspaces after = %q, want none", got)
	}
	if n := len(savedSessions(t)); n != 1 {
		t.Fatalf("sessions = %d, want 1", n)
	}

	capture(t, "session", "restore")
	agentStatus(t, "idle")
	if out := capture(t, "shutdown"); !strings.Contains(out, "already holds what is open") {
		t.Errorf("second shutdown printed %q, want the session found unchanged", out)
	}
	if n := len(savedSessions(t)); n != 1 {
		t.Errorf("sessions = %d, want still 1", n)
	}
}

// A busy agent refuses the shutdown before anything is saved or closed, and
// --force shuts down anyway.
func TestShutdownRefusesABusyAgentUnlessForced(t *testing.T) {
	work(t)
	agentStatus(t, "busy")

	err := run(io.Discard, []string{"shutdown"})
	if !errors.Is(err, errShutdownBusy) {
		t.Fatalf("err = %v, want the busy refusal", err)
	}
	if got := workspaces(t); !strings.Contains(got, "work") {
		t.Errorf("workspaces = %q, want everything still open", got)
	}
	if n := len(savedSessions(t)); n != 0 {
		t.Errorf("sessions = %d, want none saved by a refused shutdown", n)
	}

	capture(t, "shutdown", "--force", "--no-session-save")
	if got := workspaces(t); got != "" {
		t.Errorf("workspaces after --force = %q, want none", got)
	}
	if n := len(savedSessions(t)); n != 0 {
		t.Errorf("sessions = %d, want none with --no-session-save", n)
	}
}

// A project shutdown closes that project and leaves the other open.
func TestShutdownOfOneProjectLeavesTheOther(t *testing.T) {
	work(t)
	agentStatus(t, "idle")
	capture(t, "open", "demo")

	capture(t, "shutdown", "work")
	got := workspaces(t)
	if strings.Contains(got, "work") || !strings.Contains(got, "home") {
		t.Errorf("workspaces = %q, want demo's home alone", got)
	}
}

// Only the agents: the agent's pane closes and the workspace stays with its
// shell.
func TestShutdownAgentsKeepsTheWorkspace(t *testing.T) {
	work(t)
	agentStatus(t, "idle")

	capture(t, "shutdown", "work", "--agents")
	if got := workspaces(t); !strings.Contains(got, "work") {
		t.Fatalf("workspaces = %q, want the workspace kept", got)
	}
	if pids := agentPanes(t); len(pids) != 0 {
		t.Errorf("agent panes = %v, want none", pids)
	}
}
