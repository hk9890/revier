package core_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A project on this machine runs its action rendered against it, in its
// checkout.
func TestActionCommandRendersALocalActionInTheCheckout(t *testing.T) {
	p := core.Project{Project: revier.Project{Name: "demo", Path: "/src/demo"}}
	argv, dir, err := (&core.Core{}).ActionCommand(p, "sync", []string{"git", "-C", "{{.Path}}", "pull"})
	if err != nil {
		t.Fatalf("ActionCommand: %v", err)
	}
	if want := []string{"git", "-C", "/src/demo", "pull"}; !slices.Equal(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
	if dir != "/src/demo" {
		t.Errorf("dir = %q, want the checkout", dir)
	}
}

// A project on another machine runs the action there. Its path is on that
// host, so the command runs in no local directory.
func TestActionCommandRunsARemoteActionOnItsHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"ssh", "buildbox", "revier", "run", "sync", "-p", "demo"}
	c := &core.Core{Remotes: map[string]revier.Remote{"buildbox": remote}}
	p := prepared(t, remoteProject("demo"))

	argv, dir, err := c.ActionCommand(p, "sync", nil)
	if err != nil {
		t.Fatalf("ActionCommand: %v", err)
	}
	if !slices.Equal(argv, remote.RunArgv) {
		t.Errorf("argv = %q, want the host's %q", argv, remote.RunArgv)
	}
	if dir != "" {
		t.Errorf("dir = %q, want none", dir)
	}
	if len(remote.Runs) != 1 || remote.Runs[0].Action != "sync" || remote.Runs[0].Project != "demo" {
		t.Errorf("runs = %+v, want sync on demo", remote.Runs)
	}
}

// An action that renders to nothing is refused by name, not run as argv[0].
func TestActionCommandRefusesAnActionThatRunsNothing(t *testing.T) {
	p := core.Project{Project: revier.Project{Name: "demo", Path: "/src/demo"}}
	_, _, err := (&core.Core{}).ActionCommand(p, "sync", nil)
	if err == nil || !strings.Contains(err.Error(), `action "sync" runs nothing`) {
		t.Errorf("err = %v, want the action named", err)
	}
}
