//go:build live

// Layer L4 for `revier agent read`, `send-keys` and `new --no-focus`, against
// real tmux panes with no window host. The agents are the stand-ins of
// helpers_live_test.go.
package main

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// eventually polls until what holds, for what a pane does a moment after a
// key reached it.
func eventually(t *testing.T, what string, holds func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !holds() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 5s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Keys reach the agent's pane one at a time, the screen read back shows what
// they typed, and Enter submits the line they made.
func TestAgentSendKeysTypesIntoTheAgentAndTheScreenShowsIt(t *testing.T) {
	got := agents(t)
	capture(t, "agent", "send-keys", "agents:home", "h", "i", "space", "!")

	var screen string
	eventually(t, "the typed keys on the agent's screen", func() bool {
		screen = capture(t, "agent", "read", "agents:home", "--screen")
		return strings.Contains(screen, "hi !")
	})
	if strings.ContainsRune(screen, '\x1b') {
		t.Errorf("the screen carries an escape: %q", screen)
	}
	if last := capture(t, "agent", "read", "agents:home", "--screen", "--lines", "1"); strings.TrimSpace(last) != "hi !" {
		t.Errorf("the last line = %q, want the typed line alone", last)
	}

	capture(t, "agent", "send-keys", "agents:home", "enter")
	eventually(t, "the line the keys made, read by the agent", func() bool {
		read, _ := os.ReadFile(got)
		return string(read) == "hi !"
	})
}

// A key reaches the agent a prompt is refused for, and still no panel that is
// not an agent's.
func TestAgentSendKeysReachesAnAgentWaitingForAnAnswer(t *testing.T) {
	agents(t)
	capture(t, "agent", "send-keys", "agents:asker", "1")
	eventually(t, "the key on the asker's screen", func() bool {
		return strings.Contains(capture(t, "agent", "read", "agents:asker", "--screen"), "1")
	})

	err := run(io.Discard, []string{"agent", "send-keys", "agents:notes", "1"})
	if err == nil || !strings.Contains(err.Error(), "no agent panel") {
		t.Errorf("send-keys to a plain pane: err = %v, want a refusal", err)
	}
	err = run(io.Discard, []string{"agent", "send-keys", "agents:home", "escape"})
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("send-keys escape: err = %v, want the unknown key named", err)
	}
}

// An agent whose harness lists no conversation has no message to print, and
// the refusal names the screen as what can be read.
func TestAgentReadOfAnAgentWithNoConversationNamesTheScreen(t *testing.T) {
	agents(t)
	err := run(io.Discard, []string{"agent", "read", "agents:home"})
	if err == nil || !strings.Contains(err.Error(), "--screen") {
		t.Errorf("err = %v, want the refusal to name --screen", err)
	}
}

// A script's agent tab opens behind the current window, and what is printed
// is the address the new agent's pane answers to.
func TestAgentNewWithNoFocusLeavesTheCurrentWindowAndPrintsTheAddress(t *testing.T) {
	work(t)
	before := tmuxRun(t, "display-message", "-p", "-t", "work:", "#{window_id} #{pane_id}")

	address := strings.TrimSpace(capture(t, "agent", "new", "-p", "work", "--no-focus"))
	if !regexp.MustCompile(`^work:%\d+$`).MatchString(address) {
		t.Fatalf("printed %q, want work:<pane>", address)
	}
	windows := strings.Split(strings.TrimSpace(tmuxRun(t, "list-windows", "-t", "work", "-F", "#{window_panes} #{window_active}")), "\n")
	if len(windows) != 2 || windows[1] != "2 0" {
		t.Errorf("windows (panes, active) = %q, want a second window of agent and shell that is not current", windows)
	}
	if after := tmuxRun(t, "display-message", "-p", "-t", "work:", "#{window_id} #{pane_id}"); after != before {
		t.Errorf("current window and pane = %q, want %q as before", after, before)
	}
	pane := strings.TrimPrefix(address, "work:")
	if in := strings.TrimSpace(tmuxRun(t, "display-message", "-p", "-t", pane, "#{window_panes} #{window_active}")); in != "2 0" {
		t.Errorf("pane %s is in a window of (panes, active) %q, want the new one", pane, in)
	}
}
