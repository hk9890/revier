package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// A tool a host drives failed. That is revier's own failure whatever status
// the tool exited with: the message is printed, and the status is revier's,
// so a tool's 3 cannot pass for the no-project status that the desktop
// binding turns into the picker.
func TestAToolFailureIsPrintedWithRevierStatus(t *testing.T) {
	err := fmt.Errorf("hc: open home: terminal.run: %w: no core running", &exec.ExitError{})
	if status, say := outcome(err); status != 1 || !say {
		t.Errorf("outcome = %d, %v; want 1, with the message printed", status, say)
	}
}

// `revier setup install` that left keys behind ends with its own status, 4,
// and says nothing more: the plan it printed has already named every key.
func TestAnIncompleteKeysInstallEndsWithStatusFourAndNoMoreWords(t *testing.T) {
	if status, say := outcome(fmt.Errorf("install: %w", errSetupIncomplete)); status != exitSetupIncomplete || say {
		t.Errorf("outcome = %d, %v; want %d, silent", status, say, exitSetupIncomplete)
	}
}

// An argument a command takes no place for is refused, not dropped: `revier
// new my project` would otherwise write a project called my.
func TestAnExtraArgumentIsRefused(t *testing.T) {
	root := t.TempDir()
	// No host is probed: the scratch config names none, so nothing here can
	// reach the desktop this runs on.
	if err := os.WriteFile(config.File(root), []byte("[hosts]\nruntime = [\"none\"]\nwindow = [\"none\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
	t.Chdir(t.TempDir())

	for _, args := range [][]string{
		{"new", "my", "project"},
		{"open", "my", "project"},
		{"go", "editor", "extra"},
		{"run", "say", "extra"},
		{"attach", "extra"},
		{"status", "extra"},
	} {
		err := run(io.Discard, args)
		if err == nil || !strings.Contains(err.Error(), "usage: revier "+args[0]) {
			t.Errorf("revier %s: err = %v, want the usage", strings.Join(args, " "), err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "projects")); len(entries) > 0 {
		t.Errorf("a refused command wrote %d project files", len(entries))
	}
}

// A subcommand refuses an argument it takes no place for, as a command does:
// `revier session save before-reboot` would otherwise save an unnamed session.
// The app has no key binder, no window rules and a scratch kitty directory, so nothing here
// reaches the desktop or the user's terminal.
func TestASubcommandRefusesAnExtraArgument(t *testing.T) {
	a, _ := termApp(t, "", "")
	a.stateRoot = t.TempDir()
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"session save":    func() error { return cmdSessionSave(ctx, a, []string{"before-reboot"}) },
		"session list":    func() error { return cmdSessionList(a, []string{"extra"}) },
		"setup status":    func() error { return cmdSetup(ctx, a, []string{"status", "extra"}) },
		"setup install":   func() error { return cmdSetup(ctx, a, []string{"install", "extra"}) },
		"setup uninstall": func() error { return cmdSetup(ctx, a, []string{"uninstall", "extra"}) },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "usage: revier "+name) {
			t.Errorf("revier %s extra: err = %v, want the usage", name, err)
		}
	}
}

// An action's own failure passes its status on and adds nothing: the action
// has already said why, on the terminal it was given.
func TestAnActionsFailurePassesItsStatusOn(t *testing.T) {
	exit := &exec.ExitError{}
	status, say := outcome(fmt.Errorf("%w: %w", errActionFailed, exit))
	if say || status != exit.ExitCode() {
		t.Errorf("outcome = %d, %v; want %d with nothing printed", status, say, exit.ExitCode())
	}
}

// `revier list` names the agent the TUI row names: the first in the worst
// state, whatever that state is. An agent whose probe could not read it is
// still an agent, and what it was doing is still worth the column.
func TestListSumsAProjectUpByTheSameAgentAsTheTUI(t *testing.T) {
	agent := func(s revier.Status, activity string) revier.AgentView {
		return revier.AgentView{State: revier.AgentState{Status: s, Activity: activity}}
	}
	for _, tc := range []struct {
		agents []revier.AgentView
		want   string
	}{
		{[]revier.AgentView{agent(revier.StatusAttention, "first"), agent(revier.StatusAttention, "second")}, "attention: first"},
		{[]revier.AgentView{agent(revier.StatusUnknown, "reading")}, "unknown: reading"},
	} {
		if got := agentSummary(revier.ProjectView{Agents: tc.agents}); got != tc.want {
			t.Errorf("agentSummary = %q, want %q", got, tc.want)
		}
	}
}

// A -p that names nothing fails at the parse, naming the flag: read as no
// flag, a script's empty variable would act on another project.
func TestAProjectFlagThatNamesNothingFailsAtTheParse(t *testing.T) {
	for _, c := range []struct {
		args []string
		flag string
	}{
		{[]string{"-p", ""}, "flag -p"},
		{[]string{"--project="}, "flag -project"},
		{[]string{"--project", ""}, "flag -project"},
		{[]string{"-p", ":"}, "flag -p"},
		{[]string{"-p=:"}, "flag -p"},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		projectFlag(fs)
		if _, err := parseArgs(fs, c.args); err == nil || !strings.Contains(err.Error(), c.flag) {
			t.Errorf("%q: err = %v, want a refusal naming %q", c.args, err, c.flag)
		}
	}
}

// A command refuses the empty -p before it reaches a host.
func TestACommandWithAnEmptyProjectFlagActsOnNothing(t *testing.T) {
	testenv.NoTools(t)
	configRoot(t, "", nil)
	err := run(io.Discard, []string{"agent", "new", "-p", ""})
	if err == nil || !strings.Contains(err.Error(), "flag -p") {
		t.Fatalf("err = %v, want a refusal naming the flag", err)
	}
	if status, say := outcome(err); status != 1 || !say {
		t.Errorf("outcome = %d, %v, want 1 and the message", status, say)
	}
}

// ":target" still names a target of the directory's project, and no -p
// still leaves the project to the resolution.
func TestAProjectFlagWithATargetAloneAndNoFlagStillParse(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-p", ":web"}, ":web"},
		{[]string{"-p", "demo:web"}, "demo:web"},
		{nil, ""},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		project := projectFlag(fs)
		if _, err := parseArgs(fs, c.args); err != nil || *project != c.want {
			t.Errorf("%q: project = %q, err = %v, want %q", c.args, *project, err, c.want)
		}
	}
	req, err := tabRequest(revier.PanelAgent, ":web", "", "")
	if err != nil || req.Project != "" || req.Target != "web" || req.Cwd == "" {
		t.Errorf("tabRequest(:web) = %+v, %v, want the target web of the working directory's project", req, err)
	}
}

// An empty name from a script is no name it meant to leave out, so no file
// is written under the directory's name.
func TestNewRefusesAnEmptyName(t *testing.T) {
	testenv.NoTools(t)
	root := configRoot(t, "", nil)
	t.Chdir(t.TempDir())

	err := cmdNew(io.Discard, []string{""})
	if err == nil || !strings.Contains(err.Error(), "name is empty") {
		t.Fatalf("err = %v, want the empty name refused", err)
	}
	if files, _ := os.ReadDir(config.ProjectsDir(root)); len(files) != 0 {
		t.Errorf("wrote %v, want no file", files)
	}
}

// A name that starts with a dash is valid, and reads as a flag unless `--`
// comes first, as the configuration reference says.
func TestANameStartingWithADashNeedsTheSeparator(t *testing.T) {
	testenv.NoTools(t)
	root := configRoot(t, "", nil)
	t.Chdir(t.TempDir())

	if err := cmdNew(io.Discard, []string{"-x"}); err == nil || !strings.Contains(err.Error(), "-x") {
		t.Fatalf("err = %v, want -x read as an undefined flag", err)
	}
	if err := cmdNew(io.Discard, []string{"--", "-x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "-x")); err != nil {
		t.Errorf("no file for -x: %v", err)
	}
}
