//go:build live

// Layer L4 for `revier agent new` against a real tmux server. An agent tab is
// a tab (decisions.md D145), and on tmux a tab is a window of the workspace's
// session.
package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/pkg/revier"
)

// The agent opens in a new window of the workspace, on the conversation and in
// the directory the command names, with the declared shell split beside it.
func TestAgentNewOpensATabInTheWorkspace(t *testing.T) {
	args := work(t)
	agent := strings.TrimSuffix(args, ".args")
	dir := t.TempDir()

	fileConversation(t, "abc-123")
	if err := run(io.Discard, []string{"agent", "new", "-p", "work", "--resume", "abc-123", "--dir", dir}); err != nil {
		t.Fatalf("agent new: %v", err)
	}
	// The declared agent writes its own line whenever it gets to run, before
	// or after this one, so the new agent is its line among the lines.
	want := dir + " --resume abc-123"
	deadline := time.Now().Add(5 * time.Second)
	var started []string
	for !slices.Contains(started, want) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		read, _ := os.ReadFile(agent + ".started")
		started = strings.Split(strings.TrimSpace(string(read)), "\n")
	}
	if !slices.Contains(started, want) {
		t.Errorf("agents started as %q, want one started as %q", started, want)
	}
	windows := strings.Split(strings.TrimSpace(tmuxRun(t, "list-windows", "-t", "work", "-F", "#{window_panes} #{window_active}")), "\n")
	if len(windows) != 2 || windows[1] != "2 1" {
		t.Errorf("windows (panes, active) = %q, want the layout and a current second window of agent and shell", windows)
	}
}

// A tab that opened is an event of the project and the workspace it opened in,
// with the conversation and the directory it was started on.
func TestANewAgentAndANewShellAreEvents(t *testing.T) {
	work(t)
	root := os.Getenv("REVIER_STATE_HOME")
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	dir := t.TempDir()

	fileConversation(t, "abc-123")
	if err := run(io.Discard, []string{"agent", "new", "-p", "work", "--resume", "abc-123", "--dir", dir}); err != nil {
		t.Fatalf("agent new: %v", err)
	}
	if err := run(io.Discard, []string{"shell", "new", "-p", "work"}); err != nil {
		t.Fatalf("shell new: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i].Time = time.Time{}
	}
	want := []revier.Event{
		{Kind: revier.EventAgentNew, Project: "work", Target: "home", Session: "abc-123", Dir: dir},
		{Kind: revier.EventShellNew, Project: "work", Target: "home"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("events = %+v, want %+v", got, want)
	}
}

// Bringing an agent to the front from the command line is the event a press
// on it in the surface is.
func TestAnAgentFocusIsAGoAgentEvent(t *testing.T) {
	work(t)
	root := os.Getenv("REVIER_STATE_HOME")
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })

	if err := run(io.Discard, []string{"agent", "focus", "work"}); err != nil {
		t.Fatalf("agent focus: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventGoAgent || got[0].Project != "work" {
		t.Errorf("events = %+v, want one go agent of work", got)
	}
}

// A panel no workspace holds is named, whatever the runtime.
func TestAgentNewNamesAPanelNoWorkspaceHolds(t *testing.T) {
	work(t)
	err := run(io.Discard, []string{"agent", "new", "--panel", "%999"})
	if err == nil || !strings.Contains(err.Error(), "%999") {
		t.Errorf("err = %v, want the unknown panel named", err)
	}
}

// With outside "agent", in a tmux session that is no project's workspace, the
// agent key opens a window that runs config.toml's first named agent in the
// pane's directory, a named agent's key that agent, and the shell key a plain
// shell window.
func TestAgentNewOutsideAWorkspaceRunsTheConfiguredAgent(t *testing.T) {
	scratch(t)
	root := os.Getenv("REVIER_CONFIG_HOME")
	marks := t.TempDir()
	cfg, err := os.OpenFile(filepath.Join(root, "config.toml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("[terminal]\noutside = \"agent\"\n" +
		"[[agent]]\nname = \"first\"\ncommand = [\"sh\", \"-c\", \"pwd > " + marks + "/first; exec sleep 300\"]\n" +
		"[[agent]]\nname = \"second\"\ncommand = [\"sh\", \"-c\", \"pwd > " + marks + "/second; exec sleep 300\"]\n")
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}
	loose := t.TempDir()
	tmuxRun(t, "new-session", "-d", "-s", "loose", "-c", loose, "sh")
	pane := strings.TrimSpace(tmuxRun(t, "list-panes", "-t", "loose", "-F", "#{pane_id}"))

	for _, args := range [][]string{{"agent", "new", "--panel", pane}, {"agent", "new", "second", "--panel", pane}, {"shell", "new", "--panel", pane}} {
		if err := run(io.Discard, args); err != nil {
			t.Fatalf("%s: %v", strings.Join(args, " "), err)
		}
	}
	for _, name := range []string{"first", "second"} {
		var got []byte
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if got, _ = os.ReadFile(filepath.Join(marks, name)); len(got) > 0 {
				break
			}
		}
		if dir := strings.TrimSpace(string(got)); dir != loose {
			t.Errorf("agent %s started in %q, want %q", name, dir, loose)
		}
	}
	if windows := strings.Fields(tmuxRun(t, "list-windows", "-t", "loose", "-F", "#{window_panes}")); len(windows) != 4 {
		t.Errorf("windows of loose = %q, want its own, two agents and a shell", windows)
	}
}

// A project's agent new needs its workspace open.
func TestAgentNewNeedsTheWorkspaceOpen(t *testing.T) {
	work(t)
	reboot(t)
	err := run(io.Discard, []string{"agent", "new", "-p", "work"})
	if err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("err = %v, want the workspace named as not open", err)
	}
}
