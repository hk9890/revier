package proc_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// A named agent's panel carries its name as the mark an agent tab carries, and
// the default agent's carries none.
func TestANamedAgentIsMarkedWithItsName(t *testing.T) {
	root := t.TempDir()
	fake(t, root, 100, 1, []string{"claude"}, tagged("box.7", "session:demo")...)
	fake(t, root, 200, 1, []string{"dtop"}, append(tagged("box.9", "session:demo"), proc.AgentVar+"=dtop")...)

	panels := instances(t, root)[0].Panels
	if len(panels) != 2 || panels[0].Vars != nil || panels[1].Vars[revier.PanelAgentVar] != "dtop" {
		t.Fatalf("panels = %+v, want box.7 unmarked and box.9 marked dtop", panels)
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

// A process's directory is the first of the pids /proc shows one for: a
// foreground process of another user hides its own, and the pane's process
// stands in, as tmux's pane_current_path does.
func TestDirFallsBackToTheNextProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "20"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/home/user/work", filepath.Join(root, "20", "cwd")); err != nil {
		t.Fatal(err)
	}
	if got := proc.DirIn(root, 10, 20); got != "/home/user/work" {
		t.Errorf("DirIn(hidden, shown) = %q, want /home/user/work", got)
	}
	if got := proc.DirIn(root, 10, 0); got != "" {
		t.Errorf("DirIn(hidden, none) = %q, want none", got)
	}
}

// A variable is read whole, an '=' in its value included, and one that a
// longer name only starts with is not it.
func TestEnvInReadsOneVariable(t *testing.T) {
	root := t.TempDir()
	fake(t, root, 30, 1, []string{"server"}, "PATHS=no", "PATH=/a:/b=c")
	if got, ok := proc.EnvIn(root, 30, "PATH"); !ok || got != "/a:/b=c" {
		t.Errorf("EnvIn(PATH) = %q, %v; want /a:/b=c", got, ok)
	}
	if got, ok := proc.EnvIn(root, 30, "HOME"); ok {
		t.Errorf("EnvIn(HOME) = %q, want unset", got)
	}
	if got, ok := proc.EnvIn(root, 31, "PATH"); ok {
		t.Errorf("EnvIn of a process that is gone = %q, want unset", got)
	}
}

// A server is started without the agent session and without the
// GIT_EDITOR=true of a Claude Code session, and keeps the rest: Claude
// Code's configuration, the user's own GIT_EDITOR, and a variable whose name
// only starts like one of the list's.
func TestWithoutAgentSessionKeepsTheUsersConfiguration(t *testing.T) {
	kept := []string{
		"PATH=/bin", "CLAUDE_CONFIG_DIR=/c", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=m",
		"GIT_EDITOR=vim", "CLAUDE_PIDS=1", "TRACEPARENTS=x",
	}
	env := []string{"GIT_EDITOR=true"}
	for _, name := range proc.AgentSessionVars {
		env = append(env, name+"=x")
	}
	env = append(env, kept...)
	given := slices.Clone(env)
	if got := proc.WithoutAgentSession(env); !slices.Equal(got, kept) {
		t.Errorf("WithoutAgentSession = %q, want %q", got, kept)
	}
	if !slices.Equal(env, given) {
		t.Errorf("WithoutAgentSession changed its input to %q", env)
	}
}

// Without drops each variable named, and keeps one whose name only starts
// like one of them. The environment given is left as it was.
func TestWithoutDropsTheNamedVariablesAlone(t *testing.T) {
	env := []string{"TMUX=/t,1,0", "PATH=/bin", "REVIER_TAG=outer", "TMUXX=1"}
	given := slices.Clone(env)
	if got, want := proc.Without(env, []string{"TMUX", proc.TagVar}), []string{"PATH=/bin", "TMUXX=1"}; !slices.Equal(got, want) {
		t.Errorf("Without = %q, want %q", got, want)
	}
	if !slices.Equal(env, given) {
		t.Errorf("Without changed its input to %q", env)
	}
}

// Only a name the server's environment holds is set empty: one it does not
// hold is left out, a pane given it empty would take it as set. A name that
// a held one only starts with is not held. An environment that cannot be
// read sets the agent session's and revier's command names empty alone:
// Claude Code and revier read an empty one as unset, and a terminal's empty
// one reads as set.
func TestClearedSetsEmptyOnlyTheNamesTheServerHolds(t *testing.T) {
	root := t.TempDir()
	fake(t, root, 30, 1, []string{"server"}, "TMUX_PANE=%1", "AI_AGENT=claude", "CLAUDECODE=")
	names := []string{"TMUX", "TMUX_PANE", "AI_AGENT", "CLAUDECODE", "CLAUDE_PID", proc.TagVar}
	if got, want := proc.Cleared(root, 30, names), map[string]string{"TMUX_PANE": "", "AI_AGENT": "", "CLAUDECODE": ""}; !maps.Equal(got, want) {
		t.Errorf("Cleared = %v, want %v", got, want)
	}
	if got, want := proc.Cleared(root, 31, names), map[string]string{"AI_AGENT": "", "CLAUDECODE": "", "CLAUDE_PID": "", proc.TagVar: ""}; !maps.Equal(got, want) {
		t.Errorf("Cleared of a server whose environment cannot be read = %v, want %v", got, want)
	}
}

// A server's PATH is its own; one whose environment cannot be read, or holds
// no PATH, starts a program with this process's.
func TestServerPATHIsTheServersOwn(t *testing.T) {
	root := t.TempDir()
	fake(t, root, 30, 1, []string{"server"}, "PATH=/server/bin")
	fake(t, root, 32, 1, []string{"server"}, "HOME=/h")
	t.Setenv("PATH", "/mine")
	for pid, want := range map[int]string{30: "/server/bin", 31: "/mine", 32: "/mine"} {
		if got := proc.ServerPATH(root, pid); got != want {
			t.Errorf("ServerPATH(%d) = %q, want %q", pid, got, want)
		}
	}
}
