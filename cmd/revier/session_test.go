package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/session"
)

// AGENTS counts every agent the session records, not only the ones with a
// conversation to resume: an agent with none still restores, empty. REASON
// says why the session was saved.
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
	if len(lines) != 2 {
		t.Fatalf("list printed %q, want a header and one session", out.String())
	}
	if fields := strings.Fields(lines[1]); fields[len(fields)-1] != "3" {
		t.Errorf("row = %q, want AGENTS 3", lines[1])
	}
	if !strings.Contains(lines[0], "REASON") || !strings.Contains(lines[1], "shutdown") {
		t.Errorf("list printed %q, want a REASON column that says shutdown", out.String())
	}
}
