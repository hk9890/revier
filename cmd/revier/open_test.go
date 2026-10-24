package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// enclosingRuntime is a runtime that attaches with argv, and that reports
// the calling process in a pane of its server when inside is set.
type enclosingRuntime struct {
	*hosttest.FakeRuntime
	argv   []string
	inside bool
	err    error
}

func (r enclosingRuntime) AttachCommand(revier.TargetRef) ([]string, error) { return r.argv, nil }

func (r enclosingRuntime) Encloses(context.Context) (bool, error) { return r.inside, r.err }

// openAttach runs `revier open demo --attach` against rt, with the exec
// recorded instead of done, and returns what it printed and the argv exec'd.
func openAttach(t *testing.T, rt enclosingRuntime) (string, []string) {
	t.Helper()
	rt.Add("home", "")
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demoProject(t)}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{})
	var execd []string
	saved := execve
	execve = func(_ string, argv, _ []string) error {
		execd = argv
		return nil
	}
	t.Cleanup(func() { execve = saved })
	out := output(t, a, func() error { return cmdOpen(context.Background(), a, []string{"demo", "--attach"}) })
	return out, execd
}

// In a pane of the server that holds the workspace, the focus has put the
// pane on it already, and an attach would nest the server in itself, which
// tmux and herdr refuse. The command says what it focused and execs nothing.
func TestOpenAttachInAPaneOfTheServerExecsNothing(t *testing.T) {
	for _, name := range []string{"herdr", "tmux"} {
		t.Run(name, func(t *testing.T) {
			out, execd := openAttach(t, enclosingRuntime{FakeRuntime: hosttest.NewRuntime(name), argv: []string{"sh"}, inside: true})
			if execd != nil {
				t.Errorf("exec'd %q, want nothing", execd)
			}
			if !strings.HasPrefix(out, "demo: home ("+name+"/") {
				t.Errorf("output = %q, want the focused workspace named", out)
			}
		})
	}
}

// Outside a pane of that server, the terminal becomes the runtime's attach.
func TestOpenAttachOutsideAPaneExecsTheAttach(t *testing.T) {
	argv := []string{"sh", "-c", "attach"}
	_, execd := openAttach(t, enclosingRuntime{FakeRuntime: hosttest.NewRuntime("tmux"), argv: argv})
	if !slices.Equal(execd, argv) {
		t.Errorf("exec'd %q, want %q", execd, argv)
	}
}

// A check that cannot tell still attaches: it only spares a refusal, and the
// attach worked before there was a check.
func TestOpenAttachExecsTheAttachWhenTheCheckFails(t *testing.T) {
	argv := []string{"sh", "-c", "attach"}
	_, execd := openAttach(t, enclosingRuntime{FakeRuntime: hosttest.NewRuntime("tmux"), argv: argv, err: errors.New("list-panes failed")})
	if !slices.Equal(execd, argv) {
		t.Errorf("exec'd %q, want %q", execd, argv)
	}
}
