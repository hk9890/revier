package proc_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/pkg/revier"
)

// fake writes one process under root as /proc shows it: in the foreground of
// the panel's terminal, in a process group of its own.
func fake(t *testing.T, root string, pid, ppid int, cmdline []string, env ...string) {
	t.Helper()
	fakeOn(t, root, pid, ppid, pid, tty, pid, cmdline, env...)
}

// tty is the panel's terminal, the pty sshd gave the served process.
const tty = 5

// fakeOn writes one process with its process group, terminal and the
// terminal's foreground group as /proc/<pid>/stat has them.
func fakeOn(t *testing.T, root string, pid, ppid, pgrp, tty, tpgid int, cmdline []string, env ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"environ": strings.Join(env, "\x00") + "\x00",
		"cmdline": strings.Join(cmdline, "\x00") + "\x00",
		"stat":    fmt.Sprintf("%d (a (odd) name) S %d %d 1 %d %d", pid, ppid, pgrp, tty, tpgid),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func instances(t *testing.T, root string) []revier.Instance {
	t.Helper()
	got, err := (&proc.Host{Root: root}).Instances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func tagged(tag, workspace string) []string {
	return []string{"HOME=/home/u", proc.TagVar + "=" + tag, proc.WorkspaceVar + "=" + workspace}
}

// A workspace is one instance, titled as its realization is named, so the
// realization's Match finds it; each tag is a panel with the agent's pid.
func TestATagIsAPanelOfItsWorkspace(t *testing.T) {
	root := t.TempDir()
	fake(t, root, 100, 1, []string{"claude", "--resume", "abc"}, tagged("box.7", "session:demo")...)
	fake(t, root, 200, 1, []string{"claude"}, tagged("box.9", "session:demo")...)
	fake(t, root, 300, 1, []string{"claude"}, tagged("box.11", "session:other")...)
	fake(t, root, 400, 1, []string{"claude"}, "HOME=/home/u")

	got := instances(t, root)
	if len(got) != 2 || got[0].Title != "session:demo" || got[1].Title != "session:other" {
		t.Fatalf("instances = %+v, want session:demo and session:other", got)
	}
	m, err := revier.Match{Title: "^session:demo$"}.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Matches(got[0]) {
		t.Errorf("the match of the realization does not find %+v", got[0])
	}
	panels := got[0].Panels
	if len(panels) != 2 || panels[0].ID != "box.7" || panels[0].PID != 100 || panels[1].ID != "box.9" {
		t.Fatalf("panels = %+v, want box.7 on pid 100, then box.9", panels)
	}
	if !panels[0].Runs("claude") {
		t.Errorf("command = %v, want the agent's own", panels[0].Command)
	}
}

// Everything an agent starts inherits its tag: a shell tool, and a second
// Claude Code that the listing reports as a session of its own. The panel is
// the agent, whatever the pids say.
func TestWhatAnAgentStartsIsNotASecondPanel(t *testing.T) {
	root := t.TempDir()
	env := tagged("box.7", "session:demo")
	fake(t, root, 500, 1, []string{"claude"}, env...)
	fake(t, root, 90, 500, []string{"claude", "-p", "count"}, env...)
	fake(t, root, 91, 500, []string{"/bin/bash", "-c", "make"}, env...)

	got := instances(t, root)
	if len(got) != 1 || len(got[0].Panels) != 1 || got[0].Panels[0].PID != 500 {
		t.Fatalf("instances = %+v, want one panel on pid 500", got)
	}
}

// A shell tab is tagged too, so an agent typed into it is found; until then
// the panel is a shell, which no probe is asked about. The shell puts the
// agent in the terminal's foreground and stays behind it.
func TestAShellPanelBecomesTheAgentTypedIntoIt(t *testing.T) {
	root := t.TempDir()
	env := tagged("box.8", "session:demo")
	fake(t, root, 600, 1, []string{"-zsh"}, env...)
	if p := instances(t, root)[0].Panels[0]; p.Kind != revier.PanelShell {
		t.Fatalf("panel = %+v, want a shell", p)
	}
	fakeOn(t, root, 600, 1, 600, tty, 601, []string{"-zsh"}, env...)
	fakeOn(t, root, 601, 600, 601, tty, 601, []string{"claude"}, env...)
	if p := instances(t, root)[0].Panels[0]; p.Kind != revier.PanelTool || p.PID != 601 {
		t.Fatalf("panel = %+v, want the agent on pid 601", p)
	}
}

// What a shell's profile starts is not the panel: a prompt's helper on a pty
// of its own, a daemon with no terminal, and a job in the background all carry
// the tag and none is in the foreground of the panel's terminal, so the shell
// stays the panel until the agent typed into it takes the foreground.
func TestAHelperTheShellStartedIsNotThePanel(t *testing.T) {
	root := t.TempDir()
	env := tagged("box.8", "session:demo")
	fakeOn(t, root, 600, 1, 600, tty, 600, []string{"-zsh"}, env...)
	fakeOn(t, root, 590, 600, 590, 6, 590, []string{"gitstatusd"}, env...)
	fakeOn(t, root, 580, 1, 580, 0, -1, []string{"ssh-agent"}, env...)
	fakeOn(t, root, 595, 600, 595, tty, 600, []string{"sleep", "100"}, env...)
	if p := instances(t, root)[0].Panels[0]; p.Kind != revier.PanelShell || p.PID != 600 {
		t.Fatalf("panel = %+v, want the shell on pid 600", p)
	}
	fakeOn(t, root, 600, 1, 600, tty, 601, []string{"-zsh"}, env...)
	fakeOn(t, root, 601, 600, 601, tty, 601, []string{"claude"}, env...)
	if p := instances(t, root)[0].Panels[0]; p.Kind != revier.PanelTool || p.PID != 601 {
		t.Fatalf("panel = %+v, want the agent on pid 601", p)
	}
}

// A process that outlived its terminal - a server the agent left running when
// its ssh ended - keeps the tag and is in no foreground group, so it is no
// panel: listed, it would be an agent no terminal shows, and the next ssh to
// reuse the pid would be taken for it.
func TestAProcessThatOutlivedItsTerminalIsNoPanel(t *testing.T) {
	root := t.TempDir()
	fakeOn(t, root, 700, 1, 700, tty, -1, []string{"node", "server.js"}, tagged("box.8", "session:demo")...)
	if got := instances(t, root); len(got) != 0 {
		t.Fatalf("instances = %+v, want none", got)
	}
}

// A workspace name is free text: the environment separates its entries with a
// NUL and a name from its value at the first `=`, so every other byte is the
// name's own.
func TestAWorkspaceNameSurvivesTheEnvironment(t *testing.T) {
	root := t.TempDir()
	name := "session: a=b\tc\n"
	fake(t, root, 700, 1, []string{"claude"}, tagged("box.1", name)...)
	if got := instances(t, root); len(got) != 1 || got[0].Title != name || got[0].Ref.ID != name {
		t.Fatalf("instances = %+v, want one named %q", got, name)
	}
}

func TestOpenAndFocusAreRefused(t *testing.T) {
	h := &proc.Host{Root: t.TempDir()}
	if _, err := h.Open(context.Background(), revier.Realization{Name: "x"}); err == nil {
		t.Error("Open succeeded")
	}
	if err := h.Focus(context.Background(), revier.TargetRef{}); err == nil {
		t.Error("Focus succeeded")
	}
}
