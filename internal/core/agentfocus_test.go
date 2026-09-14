package core_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// An agent on this machine is reached by its tab: the tab becomes current and
// the OS window holding it is raised.
func TestGoAgentFocusesItsTabAndRaisesTheWindow(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	rt.Add("session:revier", "kitty", shellPanel("1"), agentPanel("2", "idle"), agentPanel("3", "busy"))
	wm := hosttest.New("wm")
	osw := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{titleProbe{}}}

	if _, err := c.GoAgent(context.Background(), prepared(t, project()), "3", nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(rt.PanelFocuses) != 1 || rt.PanelFocuses[0] != "3" {
		t.Errorf("panel focuses = %v, want panel 3", rt.PanelFocuses)
	}
	if len(wm.Focuses) != 1 || wm.Focuses[0] != osw {
		t.Errorf("window focuses = %v, want the workspace raised", wm.Focuses)
	}
}

// A runtime that cannot focus a panel still brings the user to the instance
// the agent is in.
func TestGoAgentWithoutTabsFocusesTheInstance(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:revier", "kitty", agentPanel("2", "idle"))
	c := &core.Core{Runtime: noTabs{rt}, Probes: []revier.AgentProbe{titleProbe{}}}

	if _, err := c.GoAgent(context.Background(), prepared(t, project()), "2", nil); err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(rt.Focuses) != 1 || rt.Focuses[0] != ref {
		t.Errorf("focuses = %v, want the instance %v", rt.Focuses, ref)
	}
}

// A link's agent is the host's: the host focuses its tab, by the panel id its
// own survey reported, and the pane onto the host is raised here.
func TestGoAgentOnALinkFocusesOnTheHostAndRaisesThePane(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	rt := hosttest.NewRuntime("rt")
	pane := rt.Add("far", "")
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}

	res, err := c.GoAgent(context.Background(), linkProject(t), "7", nil)
	if err != nil {
		t.Fatalf("GoAgent: %v", err)
	}
	if len(remote.Focused) != 1 || remote.Focused[0] != "far-there:7" {
		t.Errorf("remote focused %v, want far-there:7", remote.Focused)
	}
	if res.Target != "home" || res.Ref != pane {
		t.Errorf("result = %+v, want the home pane %v", res, pane)
	}
}
