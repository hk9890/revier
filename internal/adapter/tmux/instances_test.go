//go:build integration

// Layer L3: what Instances costs, against a recorder in place of tmux that
// answers with generated listings.
package tmux_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
)

// Two calls, whatever the project count: one for the sessions and one for
// every pane on the server.
func TestInstancesCostTwoCallsWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	var sessions, panes strings.Builder
	for i := range projects {
		fmt.Fprintf(&sessions, "4242|$%d|1|project-%d\n", i, i)
		fmt.Fprintf(&panes, "$%d|@%d|%%%d|100|zsh||shell\n", i, i, 2*i)
		fmt.Fprintf(&panes, "$%d|@%d|%%%d|101|claude||agent\n", i, i, 2*i+1)
	}
	var calls []string
	h := &tmux.Host{}
	h.SetRunner(func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "list-sessions"):
			return sessions.String(), nil
		case strings.Contains(joined, "list-panes"):
			return panes.String(), nil
		}
		return "", nil
	})

	got, err := h.Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	if p := got[1].Panels; len(p) != 2 || p[0].ID != "%2" || p[0].Tab != "@1" || p[1].Tab != "@1" {
		t.Errorf("panels = %+v, want %%2 and %%3, both in window @1", p)
	}
	if len(calls) != 2 {
		t.Errorf("tmux invoked %d times, want 2: %q", len(calls), calls)
	}
}
