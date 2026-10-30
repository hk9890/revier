package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// agent new takes one name at most.
func TestAgentNewTakesOneName(t *testing.T) {
	err := cmdAgentNew(io.Discard, []string{"dtop", "review"})
	if err == nil || !strings.Contains(err.Error(), "usage: revier agent new [<name>]") {
		t.Errorf("err = %v, want the usage", err)
	}
}

// shellTab is demo's home workspace on a fake runtime, holding panel 1, a
// shell, as the agent panel of a new tab is until its harness has started.
// open makes the app on it, for agentWait.
func shellTab(t *testing.T) (*hosttest.FakeRuntime, func(context.Context, io.Writer) (*app, error)) {
	t.Helper()
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "", revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh", Command: []string{"zsh"}})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}
	a := withState(t, &app{cfg: &config.Config{}, cfgRoot: t.TempDir(), stateRoot: t.TempDir(), core: c, projects: []core.Project{demoProject(t)}}, &state.State{})
	return rt, func(_ context.Context, out io.Writer) (*app, error) {
		a.out = out
		return a, nil
	}
}

// The address agent new --no-focus prints names a shell until the harness
// starts in it: wait waits for that, then for the status. The agent goes idle
// only after the lookup that finds it busy, so the wait after the lookup is
// what sees it go idle.
func TestAgentWaitWaitsForTheHarnessToStart(t *testing.T) {
	rt, open := shellTab(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		rt.Replace(revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "agent busy", Command: []string{"agent"}})
		time.Sleep(core.AgentPoll + 250*time.Millisecond)
		rt.Retitle("1", "agent idle")
	}()
	var out bytes.Buffer
	if err := agentWait(&out, []string{"demo:home:1", "--until", "idle", "--timeout", "10"}, open); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(out.String()) != "idle" {
		t.Errorf("wait printed %q, want idle", out.String())
	}
}

// A panel that never becomes an agent's ends the wait on its timeout, which
// says what the panel still is rather than a status it never had.
func TestAgentWaitOnAShellTimesOutSayingSo(t *testing.T) {
	_, open := shellTab(t)
	start := time.Now()
	err := agentWait(io.Discard, []string{"demo:home:1", "--until", "idle", "--timeout", "0.3"}, open)
	if !errors.Is(err, errWaitTimeout) || !errors.Is(err, core.ErrNotAgent) || !strings.Contains(err.Error(), "its foreground is a shell") {
		t.Fatalf("err = %v, want the timeout, saying the panel is still a shell", err)
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Error("the wait gave up before its timeout")
	}
}

// Only a panel address waits for its harness: a project with no agent in it
// fails at once, as prompt does.
func TestAgentWaitOnAProjectWithNoAgentFailsAtOnce(t *testing.T) {
	_, open := shellTab(t)
	err := agentWait(io.Discard, []string{"demo", "--until", "idle", "--timeout", "10"}, open)
	if !errors.Is(err, core.ErrNoAgent) {
		t.Fatalf("err = %v, want ErrNoAgent", err)
	}
}
