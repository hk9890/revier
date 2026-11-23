//go:build live

// Layer L4 for `revier agent read`, `send-keys` and `new --no-focus`, against
// real hc terminals in a fake view, with no window host. The agents are the
// stand-ins of helpers_live_test.go.
package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

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

// `agent read` is a process of its own, with no survey before it: the lookup
// of the agent reads the listing that Detail then takes the session from.
func TestAgentReadPrintsWhatTheAgentSaid(t *testing.T) {
	agents(t)
	pid := agentPids(t, "agents", "claude")[0]
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-filed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","message":{"role":"assistant","content":"the tests pass"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s-read.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	session := `{"pid": ` + pid + `, "kind": "interactive", "sessionId": "s-read", "status": "idle"}`
	if err := os.WriteFile(filepath.Join(sessionsDir, pid+".json"), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := capture(t, "agent", "read", "agents:home"); !strings.Contains(out, "the tests pass") {
		t.Errorf("agent read printed %q, want what the agent said", out)
	}
}

// A script's agent tab opens behind the current tab, and what is printed is
// the address the new agent's terminal answers to.
func TestAgentNewWithNoFocusLeavesTheCurrentTabAndPrintsTheAddress(t *testing.T) {
	work(t)
	ref := instance(t, "work").Ref
	focused, _ := liveView.Focused(t.Context())
	before, _ := liveView.FocusedPanel(t.Context(), ref)

	address := strings.TrimSpace(capture(t, "agent", "new", "-p", "work", "--no-focus"))
	if !regexp.MustCompile(`^work:home:t\d+$`).MatchString(address) {
		t.Fatalf("printed %q, want work:home:<address>", address)
	}
	got := tabs(instance(t, "work"))
	if len(got) != 2 || len(got[1]) != 2 {
		t.Fatalf("tabs = %+v, want a second tab of agent and shell", got)
	}
	if after, _ := liveView.Focused(t.Context()); after != focused {
		t.Errorf("focused instance %+v, want %+v as before", after, focused)
	}
	if after, _ := liveView.FocusedPanel(t.Context(), ref); after != before {
		t.Errorf("current panel %s, want %s as before", after, before)
	}
	if panel := strings.TrimPrefix(address, "work:home:"); revier.PanelID(panel) != got[1][0].ID {
		t.Errorf("address names %s, want the new tab's agent %s", panel, got[1][0].ID)
	}
	eventually(t, "the printed address naming the new agent", func() bool {
		return run(io.Discard, []string{"agent", "read", address, "--screen"}) == nil
	})
}
