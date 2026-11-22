package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// failingOpenRuntime is a runtime whose every Open fails with err.
type failingOpenRuntime struct {
	*hosttest.FakeRuntime
	err error
}

func (r failingOpenRuntime) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	return revier.TargetRef{}, r.err
}

// An open that fails after it created the project keeps the file and names
// it, and the cause stays readable for the exit status. A project that was
// already there gets no such words: this call did not create it.
func TestAFailedOpenNamesTheFileOnlyWhenItCreatedIt(t *testing.T) {
	testenv.NoTools(t)
	root := configRoot(t, "", nil)
	t.Chdir(t.TempDir())
	cause := errors.New("the panel did not start")
	rt := failingOpenRuntime{FakeRuntime: hosttest.NewRuntime("hc"), err: cause}
	a := withState(t, &app{out: io.Discard, cfg: &config.Config{}, cfgRoot: root, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt, View: rt}}, &state.State{})

	err := cmdOpen(t.Context(), a, []string{"typo-name"})
	file := filepath.Join(config.ProjectsDir(root), "typo-name.toml")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), file) || !strings.Contains(err.Error(), "stays") {
		t.Fatalf("err = %v, want the cause, the file %s and that it stays", err, file)
	}
	if _, statErr := os.Stat(file); statErr != nil {
		t.Errorf("the project file is gone: %v", statErr)
	}

	err = cmdOpen(t.Context(), a, []string{"typo-name"})
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "stays") {
		t.Errorf("second open: err = %v, want the cause alone", err)
	}
}

// openAttach runs `revier open demo --attach` against rt, with the exec
// recorded instead of done, and returns the argv exec'd.
func openAttach(t *testing.T, rt attachingRuntime) []string {
	t.Helper()
	rt.Add("home", "")
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt, View: rt}}, &state.State{})
	var execd []string
	saved := execve
	execve = func(_ string, argv, _ []string) error {
		execd = argv
		return nil
	}
	t.Cleanup(func() { execve = saved })
	output(t, a, func() error { return cmdOpen(context.Background(), a, []string{"demo", "--attach"}) })
	return execd
}

// The terminal becomes the runtime's attach.
func TestOpenAttachExecsTheAttach(t *testing.T) {
	argv := []string{"sh", "-c", "attach"}
	if execd := openAttach(t, attachingRuntime{FakeRuntime: hosttest.NewRuntime("hc"), argv: argv}); !slices.Equal(execd, argv) {
		t.Errorf("exec'd %q, want %q", execd, argv)
	}
}

// An empty name is a name given, so it is refused: resolved as no name, it
// would open the project last focused, which the caller never named.
func TestOpenRefusesAnEmptyName(t *testing.T) {
	rt := hosttest.NewRuntime("hc")
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt, View: rt}}, &state.State{Current: "demo"})
	err := cmdOpen(t.Context(), a, []string{""})
	if err == nil || err.Error() != "the project name is empty" {
		t.Errorf("err = %v, want the project name is empty", err)
	}
	if len(rt.Opened) != 0 {
		t.Errorf("opened %v, want nothing", rt.Opened)
	}
}
