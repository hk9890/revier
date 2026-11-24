//go:build live

// Layer L4 for `revier agent new` against a real hc core, shown in a fake
// view. An agent tab is a tab, and on hc a tab is the
// terminals of the workspace that share a tab's token.
package main

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/pkg/revier"
)

// The agent opens in a new tab of the workspace, on the conversation and in
// the directory the command names, with the declared shell split beside it,
// and the view shows it current.
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
	eventually(t, "an agent started as "+want, func() bool {
		read, _ := os.ReadFile(agent + ".started")
		return slices.Contains(strings.Split(strings.TrimSpace(string(read)), "\n"), want)
	})
	work := instance(t, "work")
	got := tabs(work)
	if len(got) != 2 || len(got[1]) != 2 {
		t.Fatalf("tabs = %+v, want the layout and a second tab of agent and shell", got)
	}
	if cur, _ := liveView.FocusedPanel(t.Context(), work.Ref); cur != got[1][0].ID {
		t.Errorf("current panel %s, want the new agent %s", cur, got[1][0].ID)
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

// A panel no workspace holds is named where no view opens a tab beside it.
func TestAgentNewNamesAPanelNoWorkspaceHolds(t *testing.T) {
	work(t)
	noView(t)
	err := run(io.Discard, []string{"agent", "new", "--panel", "t999"})
	if err == nil || !strings.Contains(err.Error(), "t999") {
		t.Errorf("err = %v, want the unknown panel named", err)
	}
}

// With outside "agent", in a window of the view's that shows no workspace,
// the agent key opens a tab of the view's own beside it that runs
// config.toml's first named agent, a named agent's key that agent, and the
// shell key a plain shell tab; hc starts none of them.
func TestAgentNewOutsideAWorkspaceRunsTheConfiguredAgent(t *testing.T) {
	scratch(t)
	root := os.Getenv("REVIER_CONFIG_HOME")
	cfg, err := os.OpenFile(config.File(root), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("[terminal]\noutside = \"agent\"\n" +
		"[[agent]]\nname = \"first\"\ncommand = [\"first-agent\"]\n" +
		"[[agent]]\nname = \"second\"\ncommand = [\"second-agent\"]\n")
	_ = cfg.Close()
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"agent", "new", "--panel", "7"}, {"agent", "new", "second", "--panel", "7"}, {"shell", "new", "--panel", "7"}} {
		if err := run(io.Discard, args); err != nil {
			t.Fatalf("%s: %v", strings.Join(args, " "), err)
		}
	}
	var got []string
	for _, tab := range liveView.Beside {
		got = append(got, string(tab.Panel)+" "+string(tab.Spec.Kind)+" "+strings.Join(tab.Spec.Command, " "))
	}
	if want := []string{"7 agent first-agent", "7 agent second-agent", "7 shell "}; !slices.Equal(got, want) {
		t.Errorf("tabs beside the window = %q, want %q", got, want)
	}
	if n := len(instances(t)); n != 0 {
		t.Errorf("hc runs %d instances, want none: the tabs are the view's own", n)
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
