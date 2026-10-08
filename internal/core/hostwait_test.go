package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// mute is a host that answers a survey and never says what its agents are
// working on: the call returns when its context is done, as an ssh to a
// machine that froze after the survey does.
type mute struct{ revier.Remote }

func (mute) Conversations(ctx context.Context, _ []revier.ProjectName) ([]revier.ProjectView, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A save waits HostWait for the host of an open link and not its whole bound
// (decisions.md D115): a host that never answered held the save before a
// close for thirty seconds. The save names the host it could not ask.
func TestASaveWaitsHostWaitForAHostThatNeverAnswers(t *testing.T) {
	defer func(w time.Duration) { core.HostWait = w }(core.HostWait)
	core.HostWait = 20 * time.Millisecond
	c, _, remote, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	c.Remotes["buildbox"] = mute{remote}
	r := survey(t, c, []core.Project{linkProject(t)}, nil)

	done := make(chan core.SessionGaps, 1)
	go func() {
		_, gaps := c.Session(context.Background(), r, "")
		done <- gaps
	}()
	select {
	case gaps := <-done:
		if len(gaps.Failed) != 1 {
			t.Errorf("save gaps = %+v, want the one host that did not answer named", gaps.Failed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the save still waits for the host, on a context with no bound of its own")
	}
}
