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
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("ASSIST_RECORD", record)
	// The stand-in files a conversation for the directory it ran in, as
	// Claude Code does once the user has said something.
	onPath(t, "claude", `{ pwd; printf '%s\n' "$@"; } > "$ASSIST_RECORD"
filed="$CLAUDE_CONFIG_DIR/projects/$(pwd | tr -c 'a-zA-Z0-9\n' '-')"
mkdir -p "$filed" && : > "$filed/abc.jsonl"`)

	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "--continue") {
		t.Errorf("the first start continues a conversation nobody held:\n%s", got)
	}
	// The next start is on the conversation the first one left.
	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(record); !strings.Contains(string(again), "\n--continue\n") {
		t.Errorf("the second start does not continue the conversation of the first:\n%s", again)
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

// A new installation has no configuration root, and the agent is allowed
// into a directory that is there: the command makes it before the agent
// starts.
func TestAssistMakesTheConfigurationRootOfANewInstallation(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", filepath.Join(t.TempDir(), "revier"))
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	onPath(t, "claude", `test -d "$REVIER_CONFIG_HOME"`)

	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Errorf("the agent found no configuration root: %v", err)
	}
}

// A file can be filed for the directory and hold nothing Claude Code will
// continue. It then ends at once, and the user must not be left with a key
// that fails until they delete a file: the agent starts once more, new.
func TestAssistStartsNewWhenTheAgentRefusesToContinue(t *testing.T) {
	configRoot(t, "", nil)
	st := t.TempDir()
	t.Setenv("REVIER_STATE_HOME", st)
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("ASSIST_RECORD", record)
	onPath(t, "claude", `filed="$CLAUDE_CONFIG_DIR/projects/$(pwd | tr -c 'a-zA-Z0-9\n' '-')"
case " $* " in *" --continue "*) echo refused >> "$ASSIST_RECORD"; exit 1;; esac
if [ ! -e "$filed/abc.jsonl" ]; then mkdir -p "$filed" && : > "$filed/abc.jsonl"; exit 0; fi
echo started >> "$ASSIST_RECORD"`)

	// The first start files the conversation, as the stand-in in the test
	// above does; the second finds it and is refused.
	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Fatal(err)
	}
	if err := run(&strings.Builder{}, []string{"assist"}); err != nil {
		t.Fatalf("assist after a refused continue: %v", err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "refused\nstarted\n" {
		t.Errorf("the agent's starts were %q, want one refused and then one new", got)
	}
}
