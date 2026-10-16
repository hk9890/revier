package core_test

import (
	"context"
	"errors"
	"slices"
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

// framedProbe finds its harness's frame at the line that reads "frame".
type framedProbe struct{ hosttest.TitleProbe }

func (framedProbe) Frame(lines []string) int {
	if i := slices.Index(lines, "frame"); i >= 0 {
		return i
	}
	return len(lines)
}

// The panes show an agent's screen above the frame its harness draws, as the
// probe that named the harness finds it, and the screen whole for a harness
// whose probe cannot look.
func TestMirrorCutsTheFrameTheHarnessProbeFinds(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	ref := rt.Add("session:demo", "kitty", revier.Panel{ID: "7"})
	screen := "said\x1b[1m\n\x1b[2mframe\x1b[m\nstatus"
	rt.Screens = map[revier.PanelID]string{"7": screen}
	probes := []revier.AgentProbe{
		framedProbe{hosttest.TitleProbe{Harness: "claude"}},
		hosttest.TitleProbe{Harness: "opencode"},
	}
	c := &core.Core{Runtime: rt, Probes: probes}

	for harness, want := range map[string]string{"claude": "said\x1b[1m", "opencode": screen} {
		agent := revier.AgentView{Ref: ref, Panel: "7", State: revier.AgentState{Harness: harness}}
		got, err := c.Mirror(context.Background(), agent, false)
		if err != nil || got != want {
			t.Errorf("%s: Mirror = %q, %v; want %q", harness, got, err, want)
		}
	}
}
