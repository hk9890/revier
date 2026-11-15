package main

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// attachingRuntime is a runtime that attaches with argv.
type attachingRuntime struct {
	*hosttest.FakeRuntime
	argv []string
}

func (r attachingRuntime) AttachCommand(revier.TargetRef) ([]string, error) { return r.argv, nil }

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
