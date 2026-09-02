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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
// them. The tmux runtime uses the user's default server here, so every test
// kills the sessions it created rather than the server.
func scratch(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed; the CLI live layer needs it")
	}
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
	other := strings.ReplaceAll(projectTOML, "%PATH%", filepath.Join(workdir, "elsewhere"))
	other = strings.ReplaceAll(other, `name = "home"`, `name = "home"`)
	if err := os.WriteFile(filepath.Join(projects, "second.toml"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", filepath.Join(root, "state"))
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", "revier").Run() })
	return workdir
}

// capture runs the command and returns everything it printed.
func capture(t *testing.T, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()

	runErr := run(args)
	_ = w.Close()
	os.Stdout = old
	out := <-done

	if runErr != nil {
		t.Fatalf("revier %s: %v\n%s", strings.Join(args, " "), runErr, out)
	}
	return out
}

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
	if err := run([]string{"go", "absent", "-p", "demo"}); err == nil {
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

	if err := run([]string{"go", "home", "-p", "nosuchproject"}); err == nil {
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
	err := run([]string{"run", "fail", "-p", "demo"})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err = %v, want the action's exit status 3", err)
	}
}

func TestRunUnknownActionIsAnError(t *testing.T) {
	scratch(t)
	err := run([]string{"run", "nosuch", "-p", "demo"})
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("err = %v, want an error naming the action", err)
	}
}
