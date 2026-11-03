package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A window host that did not answer lists no window for anything, so every
// raise through it is refused with what went wrong, and never as a window
// the host does not list: the two read the same to the user and only one of
// them is true (decisions.md D89). Each test below is one way to an OS
// window of a runtime that reports OSWindows.

// assertHostSilence checks that err names the window host's own reason, is
// told apart from ErrUnraisable, and that nothing was focused.
func assertHostSilence(t *testing.T, err error, rt *hosttest.FakeRuntime, wm *hosttest.Fake) {
	t.Helper()
	if err == nil {
		t.Fatal("succeeded; want the refusal the window host's silence causes")
	}
	if !strings.Contains(err.Error(), "wctl timed out") {
		t.Errorf("err = %v, want the window host's own reason", err)
	}
	if errors.Is(err, core.ErrUnraisable) {
		t.Errorf("err = %v, want it told apart from a window the host does not list", err)
	}
	if len(rt.Focuses) != 0 || len(wm.Focuses) != 0 {
		t.Errorf("focuses = %v %v, want none", rt.Focuses, wm.Focuses)
	}
}

func TestPressNamesTheWindowHostThatDidNotAnswer(t *testing.T) {
	rt, wm, _, _ := osWindowHosts()
	wm.SetInstancesErr(errors.New("wctl timed out"))
	c := &core.Core{Runtime: rt, Window: wm}

	_, err := press(context.Background(), c, prepared(t, osWindowProject()), "diff")
	assertHostSilence(t, err, rt, wm)
}

func TestTabPressNamesTheWindowHostThatDidNotAnswer(t *testing.T) {
	rt, wm, _, _ := osWindowHosts()
	wm.SetInstancesErr(errors.New("wctl timed out"))
	c := &core.Core{Runtime: rt, Window: wm}

	_, err := press(context.Background(), c, prepared(t, tabProject()), "tickets")
	assertHostSilence(t, err, rt, wm)
	if len(rt.Tabs) != 0 {
		t.Errorf("Tabs = %+v, want none opened", rt.Tabs)
	}
}

func TestNewAgentNamesTheWindowHostThatDidNotAnswer(t *testing.T) {
	rt, wm, _, _ := osWindowHosts()
	wm.SetInstancesErr(errors.New("wctl timed out"))
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{resumable()}}

	_, err := newAgent(t, c, prepared(t, agentProject()), "home", core.Resume{})
	assertHostSilence(t, err, rt, wm)
	if len(rt.Tabs) != 0 {
		t.Errorf("Tabs = %+v, want none opened", rt.Tabs)
	}
}

func TestGoingToAnAgentNamesTheWindowHostThatDidNotAnswer(t *testing.T) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	ref := rt.Add("session:revier", "kitty", agent("2", "abc-123", ""))
	wm := hosttest.New("wm")
	wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	wm.SetInstancesErr(errors.New("wctl timed out"))
	c := &core.Core{Runtime: rt, Window: wm}

	err := c.FocusAgent(context.Background(), "revier", revier.AgentView{Ref: ref, Panel: "2"})
	assertHostSilence(t, err, rt, wm)
}
