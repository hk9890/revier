package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// silentRemote is a host that takes the question and never answers: every
// call returns when its context is done, as an ssh that connected to a
// machine that then froze does.
type silentRemote struct{ revier.Remote }

func (silentRemote) Survey(ctx context.Context, _ []revier.ProjectName) ([]revier.ProjectView, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (silentRemote) Conversations(ctx context.Context, _ []revier.ProjectName) ([]revier.ProjectView, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A linked host that never answers holds a close and a save for HostWait and
// not for the bound of the phase that asked it (decisions.md D115): the plan's
// survey, the recheck and the save of an open link each waited thirty seconds
// for it. The save then says which host it could not ask, and the step of the
// link is left open.
func TestAHostThatNeverAnswersHoldsACloseForHostWaitAlone(t *testing.T) {
	defer func(w time.Duration) { core.HostWait = w }(core.HostWait)
	core.HostWait = 20 * time.Millisecond
	c, rt, remote, _ := linked(t, hostAgent("box.4242", revier.StatusIdle))
	c.Remotes["buildbox"] = silentRemote{remote}
	projects := []core.Project{linkProject(t)}

	start := time.Now()
	r, err := c.SurveyToClose(context.Background(), projects, nil, nil, "")
	if err != nil || r.Views[0].Unreachable == "" {
		t.Fatalf("SurveyToClose = %+v, %v; want the link marked unreachable", r.Views[0], err)
	}
	if _, gaps := c.Session(context.Background(), r, ""); len(gaps.Failed) != 1 {
		t.Errorf("save gaps = %+v, want the one host that did not answer named", gaps.Failed)
	}
	out, err := c.Shutdown(context.Background(), c.ShutdownPlan(r, "", core.ShutdownAll), 0, reading(projects))
	if err != nil || len(rt.Closed) != 0 {
		t.Fatalf("shutdown = %v, closed %v; want the link's steps left open", err, rt.Closed)
	}
	if _, open, _ := out.Counts(); open != len(out) || open == 0 {
		t.Errorf("counts = %d open of %d, want every step of the link left open", open, len(out))
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the three asks took %v, want each bounded by HostWait", took)
	}
}
