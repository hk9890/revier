package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
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

// program is a surface that runs first as soon as it is up.
type program struct{ first tea.Cmd }

func (p program) Init() tea.Cmd                       { return p.first }
func (p program) Update(tea.Msg) (tea.Model, tea.Cmd) { return p, nil }
func (p program) View() tea.View                      { return tea.NewView("") }

// surfaceOf runs a surface that runs first, with no terminal.
func surfaceOf(first tea.Cmd) error {
	return runSurface(program{first: first}, tea.WithInput(nil), tea.WithOutput(io.Discard))
}

// signalled sends the test process a signal, as a terminal that closes does.
func signalled(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Errorf("kill: %v", err)
	}
}

// wantSignal asserts a surface that ended by sig, with the status a shell
// gives that signal and nothing left to print.
func wantSignal(t *testing.T, err error, sig syscall.Signal) {
	t.Helper()
	var ended signalEnd
	if !errors.As(err, &ended) || ended.sig != sig {
		t.Fatalf("%v: runSurface = %v, want the signal named", sig, err)
	}
	if status, say := outcome(err); status != 128+int(sig) || say {
		t.Errorf("%v: outcome = %d, %v; want %d and nothing printed", sig, status, say, 128+int(sig))
	}
}

// A terminal that closes hangs the surface up. The surface ends and says
// which signal ended it, so the log line of the process names it.
func TestASignalEndsTheSurfaceAndIsNamed(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		err := surfaceOf(func() tea.Msg {
			signalled(t, sig)
			return nil
		})
		wantSignal(t, err, sig)
	}
}

// styled is a surface that draws one bold, coloured word, then quits.
type styled struct{}

func (styled) Init() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return tea.QuitMsg{} })
}
func (styled) Update(tea.Msg) (tea.Model, tea.Cmd) { return styled{}, nil }
func (styled) View() tea.View {
	return tea.NewView(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff0000")).Render("word"))
}

// NO_COLOR with any value that is not empty draws no colour, and keeps bold.
func TestNoColorWithAnyValueDrawsNoColour(t *testing.T) {
	for _, v := range []string{"1", "yes"} {
		var out bytes.Buffer
		opts := append(colourOf(func(string) string { return v }), tea.WithInput(nil), tea.WithOutput(&out), tea.WithWindowSize(20, 2))
		if err := runSurface(styled{}, opts...); err != nil {
			t.Fatalf("NO_COLOR=%s: runSurface = %v", v, err)
		}
		got := out.String()
		if !strings.Contains(got, "word") || !strings.Contains(got, "\x1b[1m") {
			t.Errorf("NO_COLOR=%s: output %q, want the word in bold", v, got)
		}
		if strings.Contains(got, "38;") {
			t.Errorf("NO_COLOR=%s: output %q has a colour", v, got)
		}
	}
	if opts := colourOf(func(string) string { return "" }); opts != nil {
		t.Errorf("colourOf(no NO_COLOR) = %v, want no option", opts)
	}
}

// A surface the user quits ended by no signal.
func TestAQuitSurfaceNamesNoSignal(t *testing.T) {
	if err := surfaceOf(tea.Quit); err != nil {
		t.Errorf("runSurface = %v, want a clean end", err)
	}
}

// ctrl+c in an action, a clone or the assistant interrupts that command, and
// the terminal sends the surface the same interrupt. The surface goes on: it
// ends when it is quit, and names no signal.
func TestAnInterruptDoesNotEndTheSurface(t *testing.T) {
	err := surfaceOf(tea.Sequence(
		func() tea.Msg {
			signalled(t, syscall.SIGINT)
			return nil
		},
		tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tea.QuitMsg{} }),
	))
	if err != nil {
		t.Errorf("runSurface = %v, want a clean end", err)
	}
}

// holds is a command that holds the terminal and outlives the signal that
// ended the surface, as an action that ignores a hangup does.
type holds struct {
	t       *testing.T
	release <-chan struct{}
}

func (holds) SetStdin(io.Reader)  {}
func (holds) SetStdout(io.Writer) {}
func (holds) SetStderr(io.Writer) {}
func (c holds) Run() error {
	signalled(c.t, syscall.SIGHUP)
	<-c.release
	return nil
}

// The surface whose terminal closed ends, with its line in the log, when the
// command that held the terminal goes on.
func TestASignalEndsTheSurfaceUnderACommandThatGoesOn(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	// The program reads its input again when the command ends, so it has one.
	in, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = in.Close()
		_ = out.Close()
	}()
	first := tea.Exec(holds{t: t, release: release}, func(error) tea.Msg { return nil })
	wantSignal(t, runSurface(program{first: first}, tea.WithInput(in), tea.WithOutput(io.Discard)), syscall.SIGHUP)
}

// boundedRuntime records whether the survey listed it under a deadline.
type boundedRuntime struct {
	*hosttest.FakeRuntime
	bounded bool
}

func (r *boundedRuntime) Instances(ctx context.Context) ([]revier.Instance, error) {
	_, r.bounded = ctx.Deadline()
	return r.FakeRuntime.Instances(ctx)
}

// `revier | grep` prints the table under the bound `revier list` has: a
// linked host whose remote revier hangs must not block the pipe for good.
func TestThePipedTableHasTheBoundOfList(t *testing.T) {
	rt := &boundedRuntime{FakeRuntime: hosttest.NewRuntime("tmux")}
	a := tuiApp(t.TempDir())
	a.stateRoot = t.TempDir()
	a.core = &core.Core{Runtime: rt}
	withState(t, a, &state.State{})

	ctx, cancel := context.WithTimeout(context.Background(), commandWait)
	defer cancel()
	output(t, a, func() error { return cmdTUI(ctx, a) })
	if !rt.bounded {
		t.Error("the piped table surveyed with no deadline")
	}
}
