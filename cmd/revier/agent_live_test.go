//go:build live

// Layer L4 for `revier agent`: wait and prompt against real tmux panes, with
// no window host. The agent is a stand-in that reports its state the way Claude
// Code does, by rewriting its file in the sessions directory the fake claude
// lists, and runs under the name claude so the Claude probe claims it.
package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentWaitEndsOnTheStatusOrTimesOut(t *testing.T) {
	agents(t)
	if out := capture(t, "agent", "wait", "agents:home", "--until", "stopped"); strings.TrimSpace(out) != "idle" {
		t.Errorf("wait printed %q, want the status it ended on", out)
	}
	start := time.Now()
	err := run(io.Discard, []string{"agent", "wait", "agents:home", "--until", "running", "--timeout", "0.3"})
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("err = %v, want the timeout, which exits %d", err, exitTimeout)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the wait outlived its timeout")
	}
}

// The prompt reaches the agent's pane, not the shell beside it, and the
// command returns once the agent is working on it, so the wait after it sees
// that turn end.
func TestAgentPromptReachesTheAgentAndReturnsOnceItWorks(t *testing.T) {
	got := agents(t)
	text := `-x "quoted" \back`
	capture(t, "agent", "prompt", "agents:home", "--", text)

	read, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("the agent read nothing: %v", err)
	}
	if string(read) != text {
		t.Errorf("agent read %q, want %q", read, text)
	}
	if out := capture(t, "agent", "wait", "agents:home", "--until", "running", "--timeout", "0.1"); strings.TrimSpace(out) != "running" {
		t.Errorf("after prompt the agent is %q, want running: prompt returns once it has left idle", out)
	}
	capture(t, "agent", "wait", "agents:home", "--until", "idle", "--timeout", "10")
}

func TestAgentPromptRefusals(t *testing.T) {
	agents(t)
	for addr, want := range map[string]string{
		"agents:asker": "waiting for an answer",
		"agents:notes": "no agent panel",
		"agents":       "names more than one agent",
	} {
		err := run(io.Discard, []string{"agent", "prompt", addr, "hello"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("prompt %s: err = %v, want %q", addr, err, want)
		}
	}
}

// wedgedTmux points revier at a tmux that never answers: it is on PATH, so it
// is selected, and every call to it hangs. It stands in for a server that
// stopped responding, which no host call times out on by itself.
func wedgedTmux(t *testing.T) {
	t.Helper()
	onPath(t, "tmux", "exec sleep 10")
	root := configRoot(t, "[hosts]\nruntime = [\"tmux\"]\nwindow = [\"none\"]\n", map[string]string{
		"demo.toml": strings.ReplaceAll(projectTOML, "%PATH%", t.TempDir()),
	})
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
}

// The timeout covers finding the agent, not only waiting on it: a host that
// never answers the lookup still ends the wait on time, with the timeout's
// status.
func TestAgentWaitTimesOutOnAHostThatNeverAnswers(t *testing.T) {
	wedgedTmux(t)
	start := time.Now()
	err := run(io.Discard, []string{"agent", "wait", "demo", "--until", "idle", "--timeout", "0.3"})
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("err = %v, want the timeout, which exits %d", err, exitTimeout)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the wait outlived its timeout")
	}
}

// A prompt has a bound of its own, so a host that never answers fails it
// rather than holding the script that called it for good.
func TestAgentPromptGivesUpOnAHostThatNeverAnswers(t *testing.T) {
	wedgedTmux(t)
	old := promptWait
	promptWait = 300 * time.Millisecond
	t.Cleanup(func() { promptWait = old })
	start := time.Now()
	if err := run(io.Discard, []string{"agent", "prompt", "demo", "hello"}); err == nil {
		t.Fatal("prompt succeeded against a host that never answered")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the prompt outlived its bound")
	}
}
