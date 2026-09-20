//go:build integration

// Layer L3: what Instances costs, against a stub tmux on the PATH that records
// every call and answers with generated listings.
package tmux_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
)

// Two calls, whatever the project count: one for the sessions and one for
// every pane on the server.
func TestInstancesCostTwoCallsWhateverTheProjectCount(t *testing.T) {
	const projects = 90
	dir := t.TempDir()
	var sessions, panes strings.Builder
	for i := range projects {
		fmt.Fprintf(&sessions, "4242|$%d|1|project-%d\n", i, i)
		fmt.Fprintf(&panes, "$%d|@%d|%%%d|100|zsh||shell\n", i, i, 2*i)
		fmt.Fprintf(&panes, "$%d|@%d|%%%d|101|claude||agent\n", i, i, 2*i+1)
	}
	for name, body := range map[string]string{"sessions": sessions.String(), "panes": panes.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> " + log + "\n" +
		"case \"$*\" in\n" +
		"*list-sessions*) cat " + filepath.Join(dir, "sessions") + " ;;\n" +
		"*list-panes*) cat " + filepath.Join(dir, "panes") + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := (&tmux.Host{}).Instances(context.Background())
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(got) != projects {
		t.Fatalf("got %d instances, want %d", len(got), projects)
	}
	if p := got[1].Panels; len(p) != 2 || p[0].ID != "%2" || p[0].Tab != "@1" || p[1].Tab != "@1" {
		t.Errorf("panels = %+v, want %%2 and %%3, both in window @1", p)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(calls), "\n"); n != 2 {
		t.Errorf("tmux invoked %d times, want 2:\n%s", n, calls)
	}
}
