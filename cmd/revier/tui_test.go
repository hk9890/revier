package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
)

// The surface opens on the project of the directory it was started in, and
// then runs in the home directory, so what it starts later never inherits a
// directory that may be removed under it.
func TestTUIStartOpensOnItsDirectoryThenLeavesForHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	worktree := filepath.Join(project, "worktree")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktree)

	start, popup := tuiStart(tuiApp(project))
	if start != "demo" || popup {
		t.Errorf("start = %q, popup = %v; want demo, false", start, popup)
	}
	if got := cwd(t); got != resolved(t, home) {
		t.Errorf("cwd = %s, want the home directory %s", got, home)
	}
}

// The popup takes its project from the environment, and leaves its directory
// as the surface does.
func TestTUIStartInThePopupLeavesForHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(core.PopupEnv, "demo")
	t.Chdir(t.TempDir())

	start, popup := tuiStart(tuiApp(t.TempDir()))
	if start != "demo" || !popup {
		t.Errorf("start = %q, popup = %v; want demo, true", start, popup)
	}
	if _, set := os.LookupEnv(core.PopupEnv); set {
		t.Errorf("%s is still set, so what the popup launches takes itself for the popup", core.PopupEnv)
	}
	if got := cwd(t); got != resolved(t, home) {
		t.Errorf("cwd = %s, want the home directory %s", got, home)
	}
}

// A home directory that does not exist leaves the surface in the root, which
// cannot be removed under it.
func TestTUIStartWithoutAHomeLeavesForTheRoot(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "gone"))
	t.Chdir(t.TempDir())

	tuiStart(tuiApp(t.TempDir()))
	if got := cwd(t); got != "/" {
		t.Errorf("cwd = %s, want /", got)
	}
}

// program is a surface that runs first as soon as it is up, with no terminal.
type program struct{ first tea.Cmd }

func (p program) Init() tea.Cmd                       { return p.first }
func (p program) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (p program) View() string                        { return "" }

func surfaceOf(first tea.Cmd) *tea.Program {
	return tea.NewProgram(program{first: first}, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
}

// A terminal that closes hangs the surface up. The surface ends and says
// which signal ended it, with the status a shell gives that signal, so the
// log line of the process names it.
func TestASignalEndsTheSurfaceAndIsNamed(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		err := runSurface(surfaceOf(func() tea.Msg {
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Errorf("kill: %v", err)
			}
			return nil
		}))
		var ended signalEnd
		if !errors.As(err, &ended) || ended.sig != sig {
			t.Fatalf("%v: runSurface = %v, want the signal named", sig, err)
		}
		if status, say := outcome(err); status != 128+int(sig) || say {
			t.Errorf("%v: outcome = %d, %v; want %d and nothing printed", sig, status, say, 128+int(sig))
		}
	}
}

// A surface the user quits ended by no signal.
func TestAQuitSurfaceNamesNoSignal(t *testing.T) {
	if err := runSurface(surfaceOf(tea.Quit)); err != nil {
		t.Errorf("runSurface = %v, want a clean end", err)
	}
}
