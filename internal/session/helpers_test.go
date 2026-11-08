package session_test

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/session"
)

func at(day int) time.Time { return time.Date(2026, 9, day, 14, 33, 5, 0, time.UTC) }

func sample(day int, name string) session.Session {
	return session.Session{
		Name: name, Reason: session.ReasonSaved, At: at(day), Current: "revier",
		Projects: []session.Project{{
			Name: "revier",
			Targets: []session.Target{
				{Name: "home", Agents: []session.Agent{
					{Harness: "claude", Session: "abc-123", Dir: "/home/user/dev/github/revier/.claude/worktrees/tui"},
					{Harness: "claude", Name: "review"},
				}},
				{Name: "editor"},
			},
		}},
	}
}

func mustSave(t *testing.T, root string, s session.Session) string {
	t.Helper()
	_, path, err := session.Save(root, s)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}
