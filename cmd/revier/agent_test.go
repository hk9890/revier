package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
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

// agentDesktop is the demo workspace open on the runtime: its own tab holds
// a shell and the declared agent, panels 1 and 2, and the tab `revier agent
// new` added holds an agent and the shell beside it, panels 3 and 4. Every
// agent reports status.
func agentDesktop(t *testing.T, status revier.Status) (*app, *hosttest.FakeRuntime) {
	t.Helper()
	p := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Match: revier.Match{Title: "^home$"}, Tabs: []revier.TargetName{"agent"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh", Tab: "t1"},
		revier.Panel{ID: "2", Kind: revier.PanelAgent, Title: "claude", Tab: "t1"},
		revier.Panel{ID: "3", Kind: revier.PanelAgent, Title: "claude", Tab: "t2"},
		revier.Panel{ID: "4", Kind: revier.PanelShell, Title: "zsh", Tab: "t2"},
	)
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: status}}
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{p}, stateRoot: t.TempDir(), core: c}, &state.State{})
	return a, rt
}

// closeAgent runs `revier agent close` on the app, and returns what it
// printed and its error.
func closeAgent(a *app, address string, force bool) (string, error) {
	var out strings.Builder
	a.out = &out
	err := a.closeAgent(context.Background(), address, force)
	return out.String(), err
}

// An agent in the tab `revier agent new` opened closes with that tab: the
// shell beside it goes too, and the command says it closed the tab.
func TestAgentCloseClosesTheTabAgentNewOpened(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	out, err := closeAgent(a, "demo:home:3", false)
	if err != nil {
		t.Fatalf("err = %v, want the agent closed", err)
	}
	if out != "closed the tab\n" {
		t.Errorf("output = %q, want the tab said closed", out)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"3", "4"}) {
		t.Errorf("panels closed = %v, want the agent and the shell beside it", rt.ClosedPanels)
	}
}

// An agent in the workspace's own tab closes as its panel alone: the shell
// the tab holds beside it stays (decisions.md D94), and the command says so.
func TestAgentCloseInTheWorkspacesOwnTabClosesTheAgentPanelAlone(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	out, err := closeAgent(a, "demo:home:2", false)
	if err != nil {
		t.Fatalf("err = %v, want the agent closed", err)
	}
	if out != "closed the agent panel\n" {
		t.Errorf("output = %q, want the agent panel said closed", out)
	}
	if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"2"}) {
		t.Errorf("panels closed = %v, want the agent's panel alone", rt.ClosedPanels)
	}
}

// A busy agent is refused and nothing closes; --force closes it.
func TestAgentCloseRefusesABusyAgentUnlessForced(t *testing.T) {
	for _, status := range []revier.Status{revier.StatusRunning, revier.StatusAttention} {
		a, rt := agentDesktop(t, status)

		out, err := closeAgent(a, "demo:home:3", false)
		if err == nil || !strings.Contains(err.Error(), "--force") || out != "" {
			t.Errorf("%s: err = %v, output = %q; want a refusal that names --force", status, err, out)
		}
		if len(rt.ClosedPanels) != 0 {
			t.Errorf("%s: panels closed = %v after the refusal, want none", status, rt.ClosedPanels)
		}

		if out, err := closeAgent(a, "demo:home:3", true); err != nil || out != "closed the tab\n" {
			t.Errorf("%s: forced: err = %v, output = %q; want the tab closed", status, err, out)
		}
		if !slices.Equal(rt.ClosedPanels, []revier.PanelID{"3", "4"}) {
			t.Errorf("%s: forced: panels closed = %v, want the agent's tab", status, rt.ClosedPanels)
		}
	}
}

// A panel that runs no agent is refused with that said, and nothing closes.
func TestAgentCloseRefusesAPanelThatRunsNoAgent(t *testing.T) {
	a, rt := agentDesktop(t, revier.StatusIdle)

	_, err := closeAgent(a, "demo:home:4", false)
	if err == nil || err.Error() != "demo:home:4: no agent runs in that panel" {
		t.Errorf("err = %v, want no agent said to run in the panel", err)
	}
	if len(rt.ClosedPanels) != 0 {
		t.Errorf("panels closed = %v, want none", rt.ClosedPanels)
	}
}

// testPoll is how often the core of shellTab reads an agent again.
const testPoll = 10 * time.Millisecond

// shellTab is demo's home workspace on a fake runtime, holding panel 1, a
// shell, as the agent panel of a new tab is until its harness has started.
// open makes the app on it, for agentWait. Its core reads every testPoll.
func shellTab(t *testing.T) (*hosttest.FakeRuntime, func(context.Context, io.Writer) (*app, error)) {
	t.Helper()
	rt := hosttest.NewRuntime("tmux")
	rt.Add("home", "", revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh", Command: []string{"zsh"}})
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}, Poll: testPoll}
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
		time.Sleep(2 * testPoll)
		rt.Replace(revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "agent busy", Command: []string{"agent"}})
		time.Sleep(25 * testPoll)
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

// A timeout too large to be a duration, infinite or not a number is refused
// before any host is asked: converted, it would end the wait at once or never.
func TestAgentWaitRefusesATimeoutItCannotWait(t *testing.T) {
	for _, timeout := range []string{"1e10", "inf", "NaN"} {
		opened := false
		open := func(context.Context, io.Writer) (*app, error) {
			opened = true
			return nil, errors.New("no host in this test")
		}
		err := agentWait(io.Discard, []string{"demo:home:1", "--until", "idle", "--timeout", timeout}, open)
		if err == nil || !strings.Contains(err.Error(), "--timeout") || opened {
			t.Errorf("--timeout %s: err = %v, opened = %v; want --timeout refused before any host is asked", timeout, err, opened)
		}
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

// With --json, wait prints the id of the conversation the agent holds beside
// the status: how a script learns it for the agent it started. A harness that
// names none gives an empty id, which stays in the output for the script to
// read.
func TestAgentWaitJSONPrintsTheConversation(t *testing.T) {
	for _, session := range []revier.SessionID{"abc-123", ""} {
		rt := hosttest.NewRuntime("tmux")
		rt.Add("home", "", revier.Panel{ID: "1", Kind: revier.PanelTool, Title: "agent", Command: []string{"agent"}})
		probe := &hosttest.FakeProbe{Harness: "agent", State: revier.AgentState{Harness: "agent", Status: revier.StatusIdle, Session: session}}
		c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{probe}, Poll: testPoll}
		a := withState(t, &app{cfg: &config.Config{}, cfgRoot: t.TempDir(), stateRoot: t.TempDir(), core: c, projects: []core.Project{demoProject(t)}}, &state.State{})
		open := func(_ context.Context, out io.Writer) (*app, error) {
			a.out = out
			return a, nil
		}
		var out bytes.Buffer
		if err := agentWait(&out, []string{"demo:home:1", "--until", "idle", "--timeout", "10", "--json"}, open); err != nil {
			t.Fatalf("wait: %v", err)
		}
		var got map[string]string
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("wait printed %q, not JSON: %v", out.String(), err)
		}
		want := map[string]string{"status": "idle", "session": string(session)}
		if !maps.Equal(got, want) {
			t.Errorf("wait printed %v, want %v", got, want)
		}
	}
}

// focusedOther is two open workspaces, demo's and other's. other's is
// focused and other is the project revier acted on last, so the focused
// window and the last project both answer other. It returns demo's home.
func focusedOther(t *testing.T) (*app, *hosttest.FakeRuntime, core.Project, revier.TargetRef) {
	t.Helper()
	demo := demoProject(t)
	other := core.PrepareProject(revier.Project{Name: "other", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "other", Launch: []string{"x"}, Match: revier.Match{Title: "^other$"}}},
	}})
	rt := hosttest.NewRuntime("tmux")
	home := rt.Add("home", "")
	rt.SetFocus(rt.Add("other", ""))
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{demo, other}, stateRoot: t.TempDir(), core: &core.Core{Runtime: rt}}, &state.State{Current: other.Name})
	return a, rt, demo, home
}

// Without -p, --panel and --dir, a tab is asked for from the working
// directory: one in no project fails with core.ErrNoProject and opens no tab,
// though the focused window and the last project answer other.
func TestATabFromAWorkingDirectoryInNoProjectFails(t *testing.T) {
	a, rt, _, _ := focusedOther(t)
	nowhere := t.TempDir()
	t.Chdir(nowhere)

	err := a.newShell(t.Context(), "", "", "")
	if !errors.Is(err, core.ErrNoProject) || !strings.Contains(err.Error(), nowhere) {
		t.Errorf("shell new in %s: err = %v, want core.ErrNoProject naming the directory", nowhere, err)
	}
	if len(rt.Tabs) != 0 {
		t.Errorf("tabs = %+v, want none", rt.Tabs)
	}
}

// Without -p, --panel and --dir, a tab opens in the project of the working
// directory, not in the focused window's or the last one.
func TestATabOpensInTheProjectOfItsWorkingDirectory(t *testing.T) {
	a, rt, demo, home := focusedOther(t)
	t.Chdir(demo.Path)

	if err := a.newShell(t.Context(), "", "", ""); err != nil {
		t.Fatalf("shell new in %s: %v", demo.Path, err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != home {
		t.Errorf("tabs = %+v, want one in %v", rt.Tabs, home)
	}
}

// -p :<target> names a target and no project: the tab opens in that target
// of the working directory's project.
func TestATabForATargetAloneOpensInTheProjectOfItsWorkingDirectory(t *testing.T) {
	a, rt, demo, _ := focusedOther(t)
	diff := rt.Add("diff", "")
	t.Chdir(demo.Path)

	if err := a.newShell(t.Context(), ":diff", "", ""); err != nil {
		t.Fatalf("shell new -p :diff in %s: %v", demo.Path, err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != diff {
		t.Errorf("tabs = %+v, want one in %v", rt.Tabs, diff)
	}
}

// Without -p and --panel, a tab asked for with --dir opens in the project
// --dir is in, not in the one of the working directory or the focused
// window's.
func TestATabFromADirOpensInItsProject(t *testing.T) {
	a, rt, demo, home := focusedOther(t)
	t.Chdir(t.TempDir())

	if err := a.newShell(t.Context(), "", "", demo.Path); err != nil {
		t.Fatalf("shell new --dir %s: %v", demo.Path, err)
	}
	if len(rt.Tabs) != 1 || rt.Tabs[0].Ref != home {
		t.Errorf("tabs = %+v, want one in %v", rt.Tabs, home)
	}
}
