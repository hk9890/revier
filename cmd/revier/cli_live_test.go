//go:build live

// Layer L4 for the CLI: the real command paths against a real tmux server.
//
// It runs in-process against a scratch config and state root, so it never sees
// the user's projects and never writes their state. tmux is the runtime; no
// window host probes in this environment, which is also the degradation case
// worth pinning - window-only targets must report unavailable rather than
// failing the command.
package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestListWithNoProjectsRunning(t *testing.T) {
	scratch(t)
	out := capture(t, "list")
	if !strings.Contains(out, "demo") {
		t.Fatalf("list did not name the project:\n%s", out)
	}
	// A window-only target with no window host is marked unavailable, not
	// missing: the user needs to see that it exists and cannot be reached.
	if !strings.Contains(out, "xeditor") {
		t.Errorf("editor should be marked unavailable (x):\n%s", out)
	}
}

func TestOpenThenListShowsRunning(t *testing.T) {
	scratch(t)
	capture(t, "open", "demo")

	out := capture(t, "list")
	if !strings.Contains(out, "running") {
		t.Fatalf("project should be running after open:\n%s", out)
	}
	if !strings.Contains(out, "*home") {
		t.Errorf("home should be marked running (*):\n%s", out)
	}
}

// The full round trip through the CLI: go opens and focuses, pressing the same
// target again returns home.
func TestGoTogglesBackThroughTheCLI(t *testing.T) {
	scratch(t)
	capture(t, "open", "demo")

	first := capture(t, "go", "notes", "-p", "demo")
	if !strings.Contains(first, "notes") {
		t.Fatalf("first go should land on notes:\n%s", first)
	}
	second := capture(t, "go", "notes", "-p", "demo")
	if !strings.Contains(second, "home") {
		t.Fatalf("second go should return home:\n%s", second)
	}
}

// The working directory decides the project, which is what makes a command
// typed in a terminal mean the obvious thing.
func TestProjectResolvesFromWorkingDirectory(t *testing.T) {
	workdir := scratch(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	out := capture(t, "status")
	if !strings.Contains(out, "demo") {
		t.Fatalf("status did not resolve the project from the cwd:\n%s", out)
	}
	if !strings.Contains(out, "tmux") {
		t.Errorf("status should report the selected runtime:\n%s", out)
	}
}

// State carries the project across a command run from nowhere in particular,
// which is how a keybinding pressed away from a terminal still works.
func TestStateRemembersTheProject(t *testing.T) {
	scratch(t)
	capture(t, "open", "demo")
	// No -p and a cwd outside any project: only remembered state can answer.
	out := capture(t, "status")
	if !strings.Contains(out, "demo") {
		t.Fatalf("status should fall back to remembered state:\n%s", out)
	}
}

func TestJSONOutput(t *testing.T) {
	scratch(t)
	out := capture(t, "list", "--json")
	for _, want := range []string{`"project"`, `"targets"`, `"available"`, `"demo"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s:\n%s", want, out)
		}
	}
}

func TestUnknownTargetIsAnError(t *testing.T) {
	scratch(t)
	if err := run(io.Discard, []string{"go", "absent", "-p", "demo"}); err == nil {
		t.Fatal("want an error for an unknown target")
	}
}

// flag.Parse stops at the first positional, so `go <target> -p <project>` used
// to leave -p unparsed and silently act on the resolved project instead. Two
// projects exist here, so acting on the wrong one is visible.
func TestProjectFlagAfterPositionalIsHonoured(t *testing.T) {
	scratch(t)
	capture(t, "open", "demo")

	out := capture(t, "go", "home", "-p", "second")
	if !strings.HasPrefix(out, "second:") {
		t.Fatalf("go acted on the wrong project:\n%s", out)
	}
}

func TestUnknownProjectAfterPositionalIsAnError(t *testing.T) {
	scratch(t)
	capture(t, "open", "demo")

	if err := run(io.Discard, []string{"go", "home", "-p", "nosuchproject"}); err == nil {
		t.Fatal("want an error for an unknown project named after the target")
	}
}

func TestOpenNamedProjectWins(t *testing.T) {
	scratch(t)
	out := capture(t, "open", "second")
	if !strings.HasPrefix(out, "second:") {
		t.Fatalf("open acted on the wrong project:\n%s", out)
	}
}

// An action runs its rendered argv in the project and needs no host at all:
// the scratch config disables the window host, and the argv sees the project.
func TestRunExecutesAnAction(t *testing.T) {
	workdir := scratch(t)
	out := capture(t, "run", "say", "-p", "demo")
	if want := "action:demo:" + workdir; !strings.Contains(out, want) {
		t.Fatalf("run printed %q, want %q: the argv must be rendered against the project", out, want)
	}
}

// The action's own exit status is the command's, so a binding sees the failure
// the action reported.
func TestRunPropagatesTheExitStatus(t *testing.T) {
	scratch(t)
	err := run(io.Discard, []string{"run", "fail", "-p", "demo"})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err = %v, want the action's exit status 3", err)
	}
}

func TestRunUnknownActionIsAnError(t *testing.T) {
	scratch(t)
	err := run(io.Discard, []string{"run", "nosuch", "-p", "demo"})
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("err = %v, want an error naming the action", err)
	}
}

// A window that belongs to no project is a normal outcome, not a failure: it
// carries its own exit status so a desktop binding can offer the picker
// instead (`go --picker`). No window host probes here, so nothing can be
// resolved from focus, which is the case under test.
func TestNoProjectAnywhereHasItsOwnOutcome(t *testing.T) {
	// The working directory is this package, which no project claims; state
	// is fresh, and no window host probes here.
	scratch(t)
	err := run(io.Discard, []string{"go", "home"})
	if err == nil {
		t.Fatal("go with no project resolved: want an error")
	}
	if !errors.Is(err, errNoProject) {
		t.Fatalf("error = %v, want the no-project outcome so the binding can fall back", err)
	}
	if exitNoProject == 1 {
		t.Error("the no-project status must differ from a generic failure")
	}
}

// With --picker, no project is the popup's to answer, not an exit status. The
// window host is disabled here, so the popup refuses, names what it needs, and
// launches nothing. A kitty stub is on PATH, so the refusal comes from the
// window host and not from a machine without kitty.
func TestNoProjectWithPickerOpensThePopup(t *testing.T) {
	scratch(t)
	onPath(t, "kitty", "exit 1")

	err := run(io.Discard, []string{"go", "home", "--picker"})
	if err == nil || errors.Is(err, errNoProject) {
		t.Fatalf("err = %v, want the popup's refusal and not the no-project outcome", err)
	}
	if !strings.Contains(err.Error(), "popup:") || strings.Contains(err.Error(), "kitty is not on PATH") {
		t.Errorf("err = %v, want the popup's refusal for want of a window host", err)
	}
}

// An argument is refused before anything is launched, so `revier popup --help`
// does not open a window.
func TestPopupRefusesArguments(t *testing.T) {
	scratch(t)
	if err := run(io.Discard, []string{"popup", "--help"}); err == nil || !strings.Contains(err.Error(), "usage: revier popup") {
		t.Errorf("err = %v, want the usage", err)
	}
}
