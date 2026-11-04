package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// The mirror reads the panel that shows the agent, through the runtime that
// holds it, with the scrollback only when asked.
func TestScreenReadsThePanelThatShowsTheAgent(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:demo", "kitty", revier.Panel{ID: "7"})
	rt.Screens = map[revier.PanelID]string{"7": "what the agent shows"}
	c := &core.Core{Runtime: rt}
	agent := revier.AgentView{Ref: ref, Panel: "7"}

	for _, scrollback := range []bool{false, true} {
		got, err := c.Screen(context.Background(), agent, scrollback)
		if err != nil || got != "what the agent shows" {
			t.Fatalf("Screen = %q, %v; want the panel's screen", got, err)
		}
	}
	want := []hosttest.ScreenRead{{Panel: "7"}, {Panel: "7", Scrollback: true}}
	if !slices.Equal(rt.ScreenReads, want) {
		t.Errorf("reads = %+v, want %+v", rt.ScreenReads, want)
	}
}

// An agent no panel of this machine's runtime shows has no screen to read
// here, which is a normal outcome and no read at all.
func TestScreenOfAnAgentElsewhereIsNoScreen(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	for name, c := range map[string]*core.Core{"no runtime": {}, "another host's agent": {Runtime: rt}} {
		agent := revier.AgentView{Ref: revier.TargetRef{Host: "proc", ID: "1"}, Panel: "7"}
		if _, err := c.Screen(context.Background(), agent, false); !errors.Is(err, core.ErrNoScreen) {
			t.Errorf("%s: err = %v, want ErrNoScreen", name, err)
		}
	}
	if len(rt.ScreenReads) != 0 {
		t.Errorf("reads = %+v, want none", rt.ScreenReads)
	}
}

// An agent this machine serves to a terminal elsewhere is drawn by that
// terminal, and the refusal names its machine, so the reader knows where to
// read it. A machine's name may hold a dot.
func TestScreenOfAServedAgentNamesTheMachineOfItsPanel(t *testing.T) {
	served := hosttest.New("proc")
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Served: served}
	for tag, machine := range map[revier.PanelID]string{"box.4242": "box", "box.lan.4242": "box.lan"} {
		agent := revier.AgentView{Ref: revier.TargetRef{Host: "proc", ID: "session:revier"}, Panel: tag}
		_, err := c.Screen(context.Background(), agent, false)
		if want := "its panel is on " + machine + "; read it there"; !errors.Is(err, core.ErrNoScreen) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want ErrNoScreen saying %q", tag, err, want)
		}
	}
	if len(rt.ScreenReads) != 0 {
		t.Errorf("reads = %+v, want none", rt.ScreenReads)
	}
}
