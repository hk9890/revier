//go:build live

// Layer L4 for `revier session`: save the desktop, lose it the way a reboot
// does, and restore it - against a real tmux server, with no window host.
//
// This is the layer that can prove the feature at all. L2 pins the policy
// against a fake, but "the workspace came back, with its agent on the
// conversation it held" is a claim about a real runtime relaunching a real
// argv, and only a real substrate answers it.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The agent writes the arguments it was started with beside itself, which is
// how the test reads back whether the restore resumed it. It runs under the
// name claude so the Claude probe claims it.
const sessionAgent = `printf '%s' "$*" > "$0.args"
printf '\033]2;%s\033\\' '✳ Ready'
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
	fakeClaude(t)
	capture(t, "open", "work")
	capture(t, "go", "notes", "-p", "work")
	return agent + ".args"
}

// agentsFile is where the fake claude reads what `claude agents --json`
// prints. It starts as an empty listing: no conversation is known.
var agentsFile string

// fakeClaude puts a claude first on PATH that answers `agents --json` from
// agentsFile, so the probe asks the real command line it asks in use while the
// test decides the answer. It also keeps the suite from reading the user's
// real sessions, and lets it run where Claude Code is not installed.
func fakeClaude(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	agentsFile = filepath.Join(bin, "agents.json")
	if err := os.WriteFile(agentsFile, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1 $2\" = 'agents --json' ] || exit 2\ncat '" + agentsFile + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
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

// windowNames lists the windows on the test's server. A server that is not
// running has no windows, which is an answer and not a failure: it is what the
// reboot leaves behind.
func windowNames(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("tmux", "list-windows", "-a", "-F", "#{window_name}").CombinedOutput()
	if err != nil && strings.Contains(string(out), "no server running") {
		return ""
	}
	if err != nil {
		t.Fatalf("tmux list-windows: %v: %s", err, out)
	}
	return string(out)
}

// markConversation makes the fake claude list the agent's pane as holding a
// conversation, by the pid tmux reports for that pane - the pid Claude Code
// lists for its own process when it is the pane's command.
func markConversation(t *testing.T, id string) {
	t.Helper()
	out := tmuxRun(t, "list-panes", "-a", "-F", "#{pane_pid} #{pane_current_command}")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		pid, cmd, ok := strings.Cut(line, " ")
		if ok && cmd == "claude" {
			listing := `[{"pid": ` + pid + `, "kind": "interactive", "sessionId": "` + id + `", "status": "idle"}]`
			if err := os.WriteFile(agentsFile, []byte(listing), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no pane is running the agent:\n%s", out)
}

// reboot loses every window without touching the project files or the saved
// session, which is the event the whole feature exists for. The bindings left
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

// The whole round trip, as a user runs it: save, reboot, restore.
func TestSessionSaveThenRestoreAfterAReboot(t *testing.T) {
	args := work(t)
	markConversation(t, "abc-123")

	save := capture(t, "session", "save", "--name", "before-reboot")
	if !strings.Contains(save, "1 project, 2 targets") {
		t.Errorf("save printed %q, want both targets recorded", save)
	}
	if !strings.Contains(save, "1 agent conversation recorded") {
		t.Errorf("save printed %q, want the conversation recorded", save)
	}

	list := capture(t, "session", "list")
	if !strings.Contains(list, "before-reboot") {
		t.Errorf("list printed %q, want the saved session", list)
	}

	reboot(t)
	if out := capture(t, "list"); strings.Contains(out, "running") {
		t.Fatalf("something survived the reboot:\n%s", out)
	}
	// The agent's own record goes with the windows, so what the test reads
	// afterwards is what the restore started.
	_ = os.Remove(args)

	restore := capture(t, "session", "restore")
	if !strings.Contains(restore, "opened 2") {
		t.Fatalf("restore printed %q, want both targets opened", restore)
	}

	// Both targets are back, by their own names.
	windows := windowNames(t)
	for _, want := range []string{"work", "work-notes"} {
		if !strings.Contains(windows, want) {
			t.Errorf("window %q did not come back:\n%s", want, windows)
		}
	}
	// And the agent came back on the conversation it held, rather than empty.
	read, err := os.ReadFile(args)
	if err != nil {
		t.Fatalf("the restored agent wrote no arguments: %v", err)
	}
	if got := strings.TrimSpace(string(read)); got != "--resume abc-123" {
		t.Errorf("the agent started with %q, want it resumed on abc-123", got)
	}
}

// An agent Claude Code does not list - claude typed into a shell, or a version
// that no longer answers - is said at save time, while it still runs, rather
// than found empty after the reboot.
func TestSessionSaveNamesAgentsItCannotResume(t *testing.T) {
	work(t)
	save := capture(t, "session", "save")
	if !strings.Contains(save, "1 agent without a conversation id") {
		t.Errorf("save printed %q, want the unnamed agent named", save)
	}
	if strings.Contains(save, "conversation recorded") {
		t.Errorf("save printed %q, want no conversation recorded", save)
	}
}

// A claude that cannot answer at all - not on PATH, or broken - is said with
// its reason, so those agents are not taken for ones no listing matched. The
// save still succeeds.
func TestSessionSaveSaysWhyItCouldNotAsk(t *testing.T) {
	work(t)
	broken := "#!/bin/sh\necho 'claude: not logged in' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(agentsFile), "claude"), []byte(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	save := capture(t, "session", "save")
	if !strings.Contains(save, "1 agent without a conversation id") {
		t.Errorf("save printed %q, want the unnamed agent counted", save)
	}
	if !strings.Contains(save, "could not ask claude: claude agents --json") {
		t.Errorf("save printed %q, want why the probe could not answer", save)
	}
}

// Restore is run-or-raise, so running it against a desktop that is already up
// opens nothing and says so. That is what makes a half-finished restore safe
// to simply run again.
func TestSessionRestoreIsIdempotent(t *testing.T) {
	work(t)
	capture(t, "session", "save")

	out := capture(t, "session", "restore")
	if !strings.Contains(out, "opened 0") {
		t.Errorf("restore printed %q, want nothing opened", out)
	}
	if strings.Count(out, "running") != 2 {
		t.Errorf("restore printed %q, want both targets reported as already up", out)
	}
	if n := strings.Count(windowNames(t), "work"); n != 2 {
		t.Errorf("there are now %d work windows, want the original 2", n)
	}
}

// The dry run reads and prints, and opens nothing. It is how the user decides
// whether a two-week-old session is still worth restoring.
func TestSessionRestoreDryRun(t *testing.T) {
	work(t)
	capture(t, "session", "save")
	reboot(t)

	out := capture(t, "session", "restore", "--dry-run")
	for _, want := range []string{"home", "notes"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run printed %q, want %s in it", out, want)
		}
	}
	// Future tense, because nothing was opened. "opened" on a dry run reads as
	// a report of work that did not happen.
	if !strings.Contains(out, "would open") {
		t.Errorf("dry run printed %q, want it to say what it would do", out)
	}
	if strings.Contains(out, "opened 2") {
		t.Errorf("dry run printed %q, want no summary of work it did not do", out)
	}
	if windows := windowNames(t); strings.Contains(windows, "work") {
		t.Errorf("the dry run opened something:\n%s", windows)
	}
}

// A target the project no longer declares is named and stepped over; the rest
// of the session still comes back. Losing nineteen workspaces over one stale
// line is the failure this avoids.
func TestSessionRestoreStepsOverWhatIsGone(t *testing.T) {
	work(t)
	capture(t, "session", "save")
	reboot(t)

	// Drop the notes target from the project, as an edit between the save and
	// the restore would.
	path := filepath.Join(os.Getenv("REVIER_CONFIG_HOME"), "projects", "work.toml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := body[:strings.Index(string(body), `[[target]]
name = "notes"`)]
	if err := os.WriteFile(path, trimmed, 0o644); err != nil {
		t.Fatal(err)
	}

	out := capture(t, "session", "restore")
	if !strings.Contains(out, "no such target") {
		t.Errorf("restore printed %q, want the dropped target named", out)
	}
	if !strings.Contains(out, "opened 1") {
		t.Errorf("restore printed %q, want the remaining target opened", out)
	}
}

// Nothing saved is a normal outcome, not a failure: the first restore on a
// machine that never saved.
func TestSessionRestoreWithNothingSaved(t *testing.T) {
	scratch(t)
	if out := capture(t, "session", "list"); !strings.Contains(out, "no saved sessions") {
		t.Errorf("list printed %q, want it to say the store is empty", out)
	}
	err := run([]string{"session", "restore"})
	if err == nil || !strings.Contains(err.Error(), "no stored session") {
		t.Errorf("err = %v, want it to say there is nothing to restore", err)
	}
}
