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
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// markConversation makes the fake claude list the agent's pane as holding a
// conversation, by the pid tmux reports for that pane - the pid Claude Code
// lists for its own process when it is the pane's command.
func markConversation(t *testing.T, id string) {
	t.Helper()
	out := tmuxRun(t, "list-panes", "-a", "-F", "#{pane_pid} #{pane_current_command}")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		pid, cmd, ok := strings.Cut(line, " ")
		if ok && cmd == "claude" {
			session := `{"pid": ` + pid + `, "kind": "interactive", "sessionId": "` + id + `", "status": "idle"}`
			if err := os.WriteFile(filepath.Join(sessionsDir, pid+".json"), []byte(session), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no pane is running the agent:\n%s", out)
}

// listConversation makes the fake claude list the process pid as holding the
// conversation id, worked on in dir.
func listConversation(t *testing.T, pid, id, dir string) {
	t.Helper()
	session := `{"pid": ` + pid + `, "kind": "interactive", "sessionId": "` + id + `", "cwd": "` + dir + `", "status": "idle"}`
	if err := os.WriteFile(filepath.Join(sessionsDir, pid+".json"), []byte(session), 0o644); err != nil {
		t.Fatal(err)
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
	names := workspaces(t)
	for _, want := range []string{"work", "work-notes"} {
		if !strings.Contains(names, want) {
			t.Errorf("workspace %q did not come back:\n%s", want, names)
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

// Every agent is recorded, with its conversation and directory, and every one
// comes back on its conversation in the directory it worked in: the declared
// agent in the workspace's layout, and each agent opened beside it in a tab of
// its own, a window of the workspace's session (decisions.md D65).
func TestSessionRestoreOnTmuxBringsBackEveryAgent(t *testing.T) {
	args := work(t)
	agent := strings.TrimSuffix(args, ".args")
	worktree, other := t.TempDir(), t.TempDir()

	// Two more agents, opened by hand in the workspace's window.
	for _, dir := range []string{other, other} {
		tmuxRun(t, "split-window", "-t", "work", "-c", dir, "bash", "-c", `exec -a claude bash "$0" "$@"`, agent)
	}
	deadline := time.Now().Add(5 * time.Second)
	pids := agentPanes(t)
	for len(pids) < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		pids = agentPanes(t)
	}
	if len(pids) != 3 {
		t.Fatalf("agent panes = %v, want 3", pids)
	}
	listConversation(t, pids[0], "declared", worktree)
	listConversation(t, pids[1], "by-hand-1", other)
	listConversation(t, pids[2], "by-hand-2", other)

	save := capture(t, "session", "save")
	if !strings.Contains(save, "3 agent conversations recorded") {
		t.Fatalf("save printed %q, want all three agents recorded", save)
	}

	reboot(t)
	_ = os.Remove(agent + ".started")

	restore := capture(t, "session", "restore")
	if !strings.Contains(restore, "opened, 3 agents resumed") {
		t.Errorf("restore printed %q, want all three agents resumed", restore)
	}
	deadline = time.Now().Add(5 * time.Second)
	var started []string
	for len(started) < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		read, _ := os.ReadFile(agent + ".started")
		started = strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(read)), " --resume ", "="))
	}
	slices.Sort(started)
	want := []string{other + "=by-hand-1", other + "=by-hand-2", worktree + "=declared"}
	slices.Sort(want)
	if !slices.Equal(started, want) {
		t.Errorf("agents started as %q, want %q", started, want)
	}
	if n := len(agentPanes(t)); n != 3 {
		t.Errorf("the workspace came back with %d agents, want 3", n)
	}
	if windows := strings.Count(tmuxRun(t, "list-windows", "-t", "work"), "\n"); windows != 3 {
		t.Errorf("the workspace has %d windows, want its layout and a tab for each of the two", windows)
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
	if err := os.WriteFile(filepath.Join(claudeBin, "claude"), []byte(broken), 0o755); err != nil {
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
	if n := strings.Count(workspaces(t), "work"); n != 2 {
		t.Errorf("there are now %d work workspaces, want the original 2", n)
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
	if names := workspaces(t); strings.Contains(names, "work") {
		t.Errorf("the dry run opened something:\n%s", names)
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
// machine that never saved, run from a login script that must not fail on it.
// A session asked for by a name nothing has is a typo, and fails.
func TestSessionRestoreWithNothingSaved(t *testing.T) {
	scratch(t)
	if out := capture(t, "session", "list"); !strings.Contains(out, "no saved sessions") {
		t.Errorf("list printed %q, want it to say the store is empty", out)
	}
	if out := capture(t, "session", "restore"); !strings.Contains(out, "no saved session") {
		t.Errorf("restore printed %q, want it to say there is nothing to restore", out)
	}
	err := run(io.Discard, []string{"session", "restore", "before-reboot"})
	if err == nil || !strings.Contains(err.Error(), "no stored session") {
		t.Errorf("err = %v, want the missing name refused", err)
	}
}

// A save with nothing open writes nothing: it would become the newest session,
// and a plain restore after the reboot would open it instead of the save made
// before.
func TestSessionSaveWithNothingOpenKeepsTheLastSession(t *testing.T) {
	work(t)
	capture(t, "session", "save", "--name", "before-reboot")
	reboot(t)

	if out := capture(t, "session", "save"); !strings.Contains(out, "nothing is open") {
		t.Errorf("save printed %q, want it to say nothing was saved", out)
	}
	if out := capture(t, "session", "list"); strings.Count(out, "\n") != 2 || !strings.Contains(out, "before-reboot") {
		t.Errorf("list printed %q, want the one session saved before the reboot", out)
	}
	if out := capture(t, "session", "restore"); !strings.Contains(out, "opened 2") {
		t.Errorf("restore printed %q, want the saved session opened", out)
	}
}
