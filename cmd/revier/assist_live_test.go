//go:build live

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agent starts in a directory of its own under the state root, briefed,
// and allowed into the configuration: a stand-in claude records all three.
func TestAssistStartsTheAgentBriefedInItsOwnDirectory(t *testing.T) {
	cfg := configRoot(t, "", nil)
	st := t.TempDir()
	t.Setenv("REVIER_STATE_HOME", st)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("ASSIST_RECORD", record)
	onPath(t, "claude", `{ pwd; printf '%s\n' "$@"; } > "$ASSIST_RECORD"`)

	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(got), "\n")
	if want := filepath.Join(st, "assist"); lines[0] != want {
		t.Errorf("the agent started in %q, want %q", lines[0], want)
	}
	if lines[1] != "--append-system-prompt" || !strings.Contains(string(got), filepath.Join(cfg, "config.toml")) {
		t.Errorf("the agent was not briefed with the configuration's path:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(got)), "--add-dir\n"+cfg) {
		t.Errorf("the agent was not allowed into %s:\n%s", cfg, got)
	}
}
