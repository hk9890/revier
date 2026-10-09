//go:build live

// Layer L4 for `revier run`: an action is a real process, so what the command
// records of one that ran, failed or took its time is tested against one.
package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// acting is the app over one project and the actions it can run.
func acting(root string, p core.Project, c *core.Core, actions ...config.Action) *app {
	c.Ledger = ledger.File{Root: root}
	return &app{cfg: &config.Config{Actions: actions}, projects: []core.Project{p}, stateRoot: root, core: c, out: &bytes.Buffer{}}
}

// localProject is a project on this machine, in a directory that exists.
func localProject(t *testing.T) core.Project {
	t.Helper()
	return core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir()})
}

// An action that ran is an event; one that failed is the log's alone.
func TestAnActionThatRanIsAnEvent(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux")}
	a := acting(root, localProject(t), c, config.Action{Name: "broken", Run: []string{"false"}}, config.Action{Name: "sync", Run: []string{"true"}})

	if err := cmdRun(context.Background(), a, []string{"broken", "-p", "demo"}); !errors.Is(err, errActionFailed) {
		t.Fatalf("run = %v, want the action's failure", err)
	}
	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "demo"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != revier.EventAction || got[0].Project != "demo" || got[0].Action != "sync" {
		t.Errorf("events = %+v, want one action sync of demo", got)
	}
}

// A link's action runs as a `revier run` on its host, which records it there:
// one here as well would be a second line of `revier events` for one action.
func TestALinksActionIsNoEventHere(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"true"}
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}

	if err := cmdRun(context.Background(), acting(root, remoteProject(t), c), []string{"sync", "-p", "far"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("events = %+v, want none: the host recorded it", got)
	}
}

// An action is stamped when it started: an editor exits hours after the press
// that opened it.
func TestAnActionsEventCarriesTheTimeItStarted(t *testing.T) {
	root := t.TempDir()
	recordingTo(t, root)
	const ran = 100 * time.Millisecond
	a := acting(root, localProject(t), &core.Core{Runtime: hosttest.NewRuntime("tmux")}, config.Action{Name: "wait", Run: []string{"sleep", "0.1"}})

	if err := cmdRun(context.Background(), a, []string{"wait", "-p", "demo"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	exited := time.Now()

	got, err := events.Read(root, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || exited.Sub(got[0].Time) < ran {
		t.Errorf("events = %+v, want one action stamped before the %s it ran, which ended at %s", got, ran, exited)
	}
}

// An action on a remote project runs on the host, through the remote's
// command, and needs no action of that name in the configuration here.
func TestRunOnARemoteProjectRunsOnTheHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.RunArgv = []string{"true"}
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{remoteProject(t)}, stateRoot: t.TempDir(), core: c}, &state.State{})

	if err := cmdRun(context.Background(), a, []string{"sync", "-p", "far"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(remote.Runs) != 1 || remote.Runs[0] != (hosttest.Run{Project: "far", Action: "sync"}) {
		t.Errorf("runs = %+v, want sync on far", remote.Runs)
	}
}
