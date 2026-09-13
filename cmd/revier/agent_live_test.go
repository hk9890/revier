//go:build live

// Layer L4 for `revier agent`: wait and prompt against real tmux panes, with
// no window host. The agent is a stand-in that reports its state the way Claude
// Code does, by rewriting its file in the sessions directory the fake claude
// lists, and runs under the name claude so the Claude probe claims it.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAgent is at rest until a line arrives, writes the line beside itself,
// and works on it for a second. It starts a moment after the line arrives, as
// a real agent does, so a prompt that returned without waiting for the turn
// is caught still idle.
const fakeAgent = `status() {
  printf '{"pid": %s, "kind": "interactive", "status": "%s"}' $$ "$1" > "%SESSIONS%/$$.tmp"
  mv "%SESSIONS%/$$.tmp" "%SESSIONS%/$$.json"
}
status idle
while IFS= read -r line; do
  printf '%s' "$line" > "$0.got"
  sleep 0.3
  status busy
  sleep 1
  status idle
done
`

// The home workspace holds the agent beside a shell; notes is a plain pane;
// asker is an agent that waits for the human, read by an external probe.
const agentsTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "agents"
  match = { title = "^agents$" }
  [[target.runtime.panels]]
  kind = "agent"
  command = ["bash", "-c", "exec -a claude bash \"$0\"", "%AGENT%"]
  [[target.runtime.panels]]
  kind = "shell"
  command = ["sh", "-c", "sleep 300"]

[[target]]
name = "notes"
  [target.runtime]
  name = "agents-notes"
  launch = ["sh", "-c", "sleep 300"]
  match = { title = "^agents-notes$" }

[[target]]
name = "asker"
  [target.runtime]
  name = "agents-asker"
  match = { title = "^agents-asker$" }
  [[target.runtime.panels]]
  kind = "agent"
  title = "Waiting for you"
  command = ["bash", "-c", "exec -a asker sleep 300"]
`

// askerProbe reports attention for a panel whose title says it is waiting.
const askerProbe = `#!/bin/sh
case "$(cat)" in *Waiting*) echo '{"status":"attention"}' ;; *) echo '{"status":"idle"}' ;; esac
`

// agents opens the agents project on the scratch tmux server and returns the
// path the agent writes what it read to.
func agents(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed; the stand-in agent needs exec -a")
	}
	workdir := scratch(t)
	root := os.Getenv("REVIER_CONFIG_HOME")
	dir := t.TempDir()
	agent, probe := filepath.Join(dir, "agent.sh"), filepath.Join(dir, "asker-probe")
	if err := os.WriteFile(agent, []byte(strings.ReplaceAll(fakeAgent, "%SESSIONS%", sessionsDir)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(probe, []byte(askerProbe), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReplacer("%PATH%", workdir, "%AGENT%", agent).Replace(agentsTOML)
	if err := os.WriteFile(filepath.Join(root, "projects", "agents.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(filepath.Join(root, "config.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("[[probe]]\nname = \"asker\"\nexec = \"" + probe + "\"\n")
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}

	capture(t, "open", "agents")
	capture(t, "go", "notes", "-p", "agents")
	capture(t, "go", "asker", "-p", "agents")
	return agent + ".got"
}

func TestAgentWaitEndsOnTheStatusOrTimesOut(t *testing.T) {
	agents(t)
	if out := capture(t, "agent", "wait", "agents:home", "--until", "stopped"); strings.TrimSpace(out) != "idle" {
		t.Errorf("wait printed %q, want the status it ended on", out)
	}
	start := time.Now()
	err := run([]string{"agent", "wait", "agents:home", "--until", "running", "--timeout", "0.3"})
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
		err := run([]string{"agent", "prompt", addr, "hello"})
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
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexec sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(projectTOML, "%PATH%", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "projects", "demo.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("[hosts]\nruntime = [\"tmux\"]\nwindow = [\"none\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
}

// The timeout covers finding the agent, not only waiting on it: a host that
// never answers the lookup still ends the wait on time, with the timeout's
// status.
func TestAgentWaitTimesOutOnAHostThatNeverAnswers(t *testing.T) {
	wedgedTmux(t)
	start := time.Now()
	err := run([]string{"agent", "wait", "demo", "--until", "idle", "--timeout", "0.3"})
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
	old := promptTimeout
	promptTimeout = 300 * time.Millisecond
	t.Cleanup(func() { promptTimeout = old })
	start := time.Now()
	if err := run([]string{"agent", "prompt", "demo", "hello"}); err == nil {
		t.Fatal("prompt succeeded against a host that never answered")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the prompt outlived its bound")
	}
}
