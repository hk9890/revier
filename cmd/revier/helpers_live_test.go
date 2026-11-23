//go:build live

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
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
// them, and gives the test an hc core of its own, shown in a view that opens
// no window (decisions.md D184). The runtime is hc and the view is named
// kitty, as a user who switched to hc writes them; viewOf hands every run
// the test's one FakeView in kitty's place, so what it shows outlives a
// run, as kitty's windows do. hc's config, state, runtime directory and
// socket are the test's, and no service manager is reached, so the core the
// first open starts runs detached: the user's own core is never seen, and
// never stopped.
func scratch(t *testing.T) string {
	t.Helper()
	// Short, and not t.TempDir: a socket path over 107 bytes cannot be bound.
	dir, err := os.MkdirTemp("", "rv-"+testenv.ShortName(t)+"-")
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(dir, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	liveSocket = filepath.Join(dir, "s.sock")
	testenv.NoServiceManager(t, run)
	for name, value := range map[string]string{
		"XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "HC_SOCKET": liveSocket, "SHELL": "/bin/sh",
		"HC_SESSION": "", "HC_TERMINAL_ID": "", "HC_TERMINAL_SOCKET": "", "HC_SCOPE": "",
	} {
		t.Setenv(name, value)
	}
	// Registered after the environment it needs, so it runs before that is
	// restored: the core it stops is this test's.
	t.Cleanup(func() {
		stopCore(t)
		_ = os.RemoveAll(dir)
	})
	if err := (&hc.Host{Socket: liveSocket}).Probe(t.Context()); err != nil {
		t.Skipf("hc is not usable; the CLI live layer needs it: %v", err)
	}
	liveView = hosttest.NewView("kitty")
	shownWith := viewOf
	viewOf = func(ctx context.Context, name string, rt revier.Runtime) (revier.View, error) {
		if name == "kitty" && rt.Name() == "hc" {
			return liveView, nil
		}
		return shownWith(ctx, name, rt)
	}
	t.Cleanup(func() { viewOf = shownWith })

	root := t.TempDir()
	projects := config.ProjectsDir(root)
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()
	body := strings.ReplaceAll(projectTOML, "%PATH%", workdir)
	if err := os.WriteFile(filepath.Join(projects, "demo.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pin the runtime to hc and disable the window host. This machine may
	// have a working kitty and a working GNOME adapter, and a test that
	// behaves differently depending on the ambient desktop is not a test. It
	// also keeps the suite from ever opening or touching a real window.
	cfg := "[hosts]\nruntime = [\"hc\"]\nview = [\"kitty\"]\nwindow = [\"none\"]\n" +
		"[[action]]\nkey = \"ctrl-y\"\nname = \"say\"\nrun = [\"sh\", \"-c\", \"echo action:$0:$1\", \"{{.Name}}\", \"{{.Path}}\"]\n" +
		"[[action]]\nkey = \"ctrl-x\"\nname = \"fail\"\nrun = [\"sh\", \"-c\", \"exit 3\"]\n"
	if err := os.WriteFile(config.File(root), []byte(cfg), 0o644); err != nil {
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

// noView leaves the test's hc core unshown, as over ssh.
func noView(t *testing.T) {
	t.Helper()
	path := config.File(os.Getenv("REVIER_CONFIG_HOME"))
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), `view = ["kitty"]`, `view = ["none"]`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// liveSocket is the socket of the test's hc core, and liveView the view
// every run of the test shows that core's terminals in.
var (
	liveSocket string
	liveView   *hosttest.FakeView
)

// stopCore stops the test's core, which ends every program it runs, and
// makes sure the core and its holders are gone: a pid still alive at the
// deadline is killed. It runs the binary the hc host runs, herdr-core where
// no hc links to it. hc's own CLI starts a core when none answers, so it is
// asked only of a core that does.
func stopCore(t *testing.T) {
	t.Helper()
	conn, err := net.Dial("unix", liveSocket)
	if err != nil {
		return
	}
	_ = conn.Close()
	bin, err := hc.Program()
	if err != nil {
		t.Errorf("stop the test's hc core: %v", err)
		return
	}
	var status struct {
		PID     int `json:"pid"`
		Holders []struct {
			PID int `json:"pid"`
		} `json:"holders"`
	}
	out, err := exec.Command(bin, "server", "status", "--json").Output()
	if err == nil {
		_ = json.Unmarshal(out, &status)
	}
	if out, err := exec.Command(bin, "server", "stop").CombinedOutput(); err != nil {
		t.Errorf("%s server stop: %v: %s", bin, err, out)
	}
	pids := []int{status.PID}
	for _, holder := range status.Holders {
		pids = append(pids, holder.PID)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		for pid > 0 && syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if pid > 0 && syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
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

// agents opens the agents project on the scratch hc core and returns the
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
	if err := os.WriteFile(filepath.Join(config.ProjectsDir(root), "agents.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(config.File(root), os.O_APPEND|os.O_WRONLY, 0)
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
	running(t, "agents", "claude")
	running(t, "agents-asker", "asker")
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
	if err := os.WriteFile(filepath.Join(config.ProjectsDir(root), "work.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, "open", "work")
	capture(t, "go", "notes", "-p", "work")
	running(t, "work", "claude")
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

// fileConversation files a transcript of the conversation id where the fake
// claude's config directory keeps them, as Claude Code does at the first
// prompt. revier resumes only a conversation that is filed.
func fileConversation(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-filed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// instances is what the test's core runs, as revier groups it into
// instances. A core that is not running runs none, which is an answer and
// not a failure: it is what the reboot leaves behind.
func instances(t *testing.T) []revier.Instance {
	t.Helper()
	got, err := (&hc.Host{Socket: liveSocket}).Instances(t.Context())
	if err != nil {
		t.Fatalf("hc instances: %v", err)
	}
	return got
}

// instance is the instance revier named name, with its panels in the order
// of its tabs.
func instance(t *testing.T, name string) revier.Instance {
	t.Helper()
	all := instances(t)
	for _, in := range all {
		if in.Title == name {
			return in
		}
	}
	t.Fatalf("no instance %q: %+v", name, all)
	return revier.Instance{}
}

// tabs is the instance's panels, one entry per tab, in the order of its tabs.
func tabs(in revier.Instance) [][]revier.Panel {
	var out [][]revier.Panel
	for i, p := range in.Panels {
		if i == 0 || p.Tab != in.Panels[i-1].Tab {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], p)
	}
	return out
}

// workspaces lists the names revier gave the instances on the test's core.
func workspaces(t *testing.T) string {
	t.Helper()
	var names strings.Builder
	for _, in := range instances(t) {
		names.WriteString(in.Title + "\n")
	}
	return names.String()
}

// agentPids lists the pids of the panels of the instance called name that
// run program, in the order of its panels: the order a save records agents
// in.
func agentPids(t *testing.T, name, program string) []string {
	t.Helper()
	var pids []string
	for _, in := range instances(t) {
		if in.Title != name {
			continue
		}
		for _, p := range in.Panels {
			if p.Runs(program) {
				pids = append(pids, strconv.Itoa(p.PID))
			}
		}
	}
	return pids
}

// agentPanes lists the pids of work's panels running the agent.
func agentPanes(t *testing.T) []string {
	t.Helper()
	return agentPids(t, "work", "claude")
}

// running waits until hc reports a panel of the instance called name as
// running program: hc reads a terminal's foreground process about twice a
// second, and until it has, the panel runs what it was started with.
func running(t *testing.T, name, program string) {
	t.Helper()
	eventually(t, name+" running "+program, func() bool { return len(agentPids(t, name, program)) > 0 })
}

// reboot loses every terminal without touching the project files or the
// saved session, which is the event `revier session` exists for. The
// bindings left in state now point at terminals that do not exist, exactly
// as they would after a real restart. The view's windows attached those
// terminals, so they go too.
func reboot(t *testing.T) {
	t.Helper()
	stopCore(t)
	liveView = hosttest.NewView("kitty")
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("unix", liveSocket)
		if err != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the hc core is still answering after it was stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The home workspace lists the tab that holds the agent beside a shell; notes
// is a plain pane; asker lists the tab of an agent that waits for the human,
// read by an external probe. The asker sets its own title: hc gives a
// terminal the title its program sets, and no other.
const agentsTOML = `
path = "%PATH%"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "agents"
  match = { title = "^agents$" }
  tabs = ["agent"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
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
  tabs = ["asking"]

[[target]]
name = "asking"
  [target.runtime]
  inside = "asker"
  [[target.runtime.panels]]
  kind = "agent"
  command = ["bash", "-c", "printf '\\033]2;Waiting for you\\007'; exec -a asker sleep 300"]
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
  [ -e "$0.hold" ] && continue
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
  tabs = ["agent"]

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
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

// listen is a Unix socket of the test's own that hands every connection it
// accepts to accepted, which keeps it or closes it.
func listen(t *testing.T, accepted func(net.Conn)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rvl")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = l.Close()
		_ = os.RemoveAll(dir)
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			accepted(conn)
		}
	}()
	return sock
}

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
