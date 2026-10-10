//go:build live

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// onPath puts an executable named name first on PATH that runs body.
func onPath(t *testing.T, name, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const projectTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["sh", "-c", "sleep 300"]
  match = { title = "^home$" }

[[target]]
name = "notes"
key = "ctrl-n"
  [target.runtime]
  name = "notes"
  launch = ["sh", "-c", "sleep 300"]
  match = { title = "^notes$" }

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["true"]
  match = { class = "^definitely-not-running$" }
`

// scratch builds an isolated config and state root and points the process at
// them, and gives the test a tmux server of its own. The runtime here is the
// default server, as a real `revier` gets it (adapters.go), so the default is
// what moves: TMUX_TMPDIR puts its socket in the test's directory, and TMUX
// is cleared, so a run from inside tmux does not reach the server it runs in.
// The user's own sessions are never seen, and never killed.
func scratch(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; the CLI live layer needs it")
	}
	// Short, and not t.TempDir: a socket path over 108 bytes cannot be bound.
	sockets, err := os.MkdirTemp("", "rv")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", sockets)
	t.Setenv("TMUX", "")
	_ = os.Unsetenv("TMUX")
	// Registered after the environment it needs, so it runs before that is
	// restored: the server it kills is this test's.
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-server").Run()
		_ = os.RemoveAll(sockets)
	})
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()
	body := strings.ReplaceAll(projectTOML, "%PATH%", workdir)
	if err := os.WriteFile(filepath.Join(projects, "demo.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pin the runtime to tmux and disable the window host. This machine may
	// have a working kitty and a working GNOME adapter, and a test that
	// behaves differently depending on the ambient desktop is not a test. It
	// also keeps the suite from ever opening or touching a real window.
	cfg := "[hosts]\nruntime = [\"tmux\"]\nwindow = [\"none\"]\n" +
		"[[action]]\nkey = \"ctrl-y\"\nname = \"say\"\nrun = [\"sh\", \"-c\", \"echo action:$0:$1\", \"{{.Name}}\", \"{{.Path}}\"]\n" +
		"[[action]]\nkey = \"ctrl-x\"\nname = \"fail\"\nrun = [\"sh\", \"-c\", \"exit 3\"]\n"
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// A second project, so a command acting on the wrong one is detectable.
	// Its directory exists: `open` on a missing one clones or fails.
	elsewhere := filepath.Join(workdir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	other := strings.ReplaceAll(projectTOML, "%PATH%", elsewhere)
	if err := os.WriteFile(filepath.Join(projects, "second.toml"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
	fakeClaude(t)
	return workdir
}

// capture runs the command and returns everything it printed.
func capture(t *testing.T, args ...string) string {
	t.Helper()
	var b bytes.Buffer
	if err := run(&b, args); err != nil {
		t.Fatalf("revier %s: %v\n%s", strings.Join(args, " "), err, b.String())
	}
	return b.String()
}

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

// work builds a project whose workspace holds an agent, opens it and its notes
// target, and returns the file the agent writes its arguments to.
func work(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed; the stand-in agent needs exec -a")
	}
	workdir := scratch(t)
	root := os.Getenv("REVIER_CONFIG_HOME")
	agent := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(agent, []byte(sessionAgent), 0o644); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReplacer("%PATH%", workdir, "%AGENT%", agent).Replace(sessionTOML)
	if err := os.WriteFile(filepath.Join(root, "projects", "work.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, "open", "work")
	capture(t, "go", "notes", "-p", "work")
	return agent + ".args"
}

// claudeBin is the directory holding the fake claude, and sessionsDir the
// sessions directory it answers from.
var claudeBin, sessionsDir string

// fakeClaude puts a claude first on PATH whose `agents --json` lists the files
// in a scratch sessions directory, one session each, and points
// CLAUDE_CONFIG_DIR at it. A stand-in agent reports its state the way Claude
// Code does, by writing its file there, and the probe asks the real command
// line it asks in use. It also keeps the suite from reading the user's real
// sessions, and lets it run where Claude Code is not installed.
func fakeClaude(t *testing.T) {
	t.Helper()
	claudeBin = t.TempDir()
	home := t.TempDir()
	sessionsDir = filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1 $2\" = 'agents --json' ] || exit 2\n" +
		"sep=''; printf '['\n" +
		"for f in '" + sessionsDir + "'/*.json; do [ -e \"$f\" ] || continue; printf '%s' \"$sep\"; cat \"$f\"; sep=','; done\n" +
		"printf ']'\n"
	if err := os.WriteFile(filepath.Join(claudeBin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", claudeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CONFIG_DIR", home)
}

// tmuxRun runs a tmux command against the test's own server.
func tmuxRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// workspaces lists the names revier gave the sessions on the test's server. A
// server that is not running has none, which is an answer and not a failure:
// it is what the reboot leaves behind.
func workspaces(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{@revier-name}").CombinedOutput()
	if err != nil && strings.Contains(string(out), "no server running") {
		return ""
	}
	if err != nil {
		t.Fatalf("tmux list-sessions: %v: %s", err, out)
	}
	return string(out)
}

// agentPanes lists the pids of the panes running the agent, in the window's
// order: the order a save records agents in.
func agentPanes(t *testing.T) []string {
	t.Helper()
	var pids []string
	out := tmuxRun(t, "list-panes", "-s", "-t", "work", "-F", "#{pane_pid} #{pane_current_command}")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if pid, cmd, ok := strings.Cut(line, " "); ok && cmd == "claude" {
			pids = append(pids, pid)
		}
	}
	return pids
}

// reboot loses every window without touching the project files or the saved
// session, which is the event `revier session` exists for. The bindings left
// in state now point at windows that do not exist, exactly as they would after
// a real restart.
//
// kill-server returns before the server has exited. A command sent in that
// window reaches a server that is going away and fails with "server exited
// unexpectedly", which is not what a restore after a real reboot meets. So the
// reboot is over only when no server answers.
func reboot(t *testing.T) {
	t.Helper()
	_ = exec.Command("tmux", "kill-server").Run()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.Command("tmux", "list-sessions").CombinedOutput()
		if err != nil && (strings.Contains(string(out), "no server running") || strings.Contains(string(out), "error connecting")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tmux server is still answering after kill-server: %v: %s", err, out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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

// askerProbe reports attention for a panel whose title says it is waiting.
const askerProbe = `#!/bin/sh
case "$(cat)" in *Waiting*) echo '{"status":"attention"}' ;; *) echo '{"status":"idle"}' ;; esac
`

// The agent writes the arguments it was started with beside itself, which is
// how the test reads back whether the restore resumed it, and adds a line with
// its directory to a log every agent shares. It runs under the name claude so
// the Claude probe claims it.
const sessionAgent = `printf '%s' "$*" > "$0.args"
printf '%s %s\n' "$PWD" "$*" >> "$0.started"
sleep 300
`

const sessionTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "work"
  match = { title = "^work$" }
  [[target.runtime.panels]]
  kind = "shell"
  command = ["sh", "-c", "sleep 300"]
  [[target.runtime.panels]]
  kind = "agent"
  command = ["bash", "-c", "exec -a claude bash \"$0\" \"$@\"", "%AGENT%"]

[[target]]
name = "notes"
  [target.runtime]
  name = "work-notes"
  launch = ["sh", "-c", "sleep 300"]
  match = { title = "^work-notes$" }
`
