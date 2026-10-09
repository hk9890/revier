package core_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// A close waits only for the linked hosts it is about (decisions.md D115). A
// link with nothing open here has nothing a close could end, so neither the
// survey a plan is drawn from nor the recheck inside the close asks its host:
// a host that is gone cost every close of a project on this machine a wait
// for it, twice.
func TestACloseAsksNoHostOfALinkWithNothingOpenHere(t *testing.T) {
	c, rt, _, projects := openDesktop(t, revier.StatusIdle)
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{Project: revier.Project{Name: "far-there"}})
	c.Remotes = map[string]revier.Remote{"buildbox": remote}
	projects = append(projects, linkProject(t))

	var plan []core.CloseStep
	for _, only := range []revier.ProjectName{projects[0].Name, ""} {
		r := c.SurveyToClose(context.Background(), projects, only)
		if plan = c.ShutdownPlan(r, only, core.ShutdownAll); len(plan) == 0 {
			t.Fatalf("plan for %q is empty, want what is open on this machine", only)
		}
	}
	if _, err := c.Shutdown(context.Background(), plan, 0, reading(projects)); err != nil || len(rt.Closed) == 0 {
		t.Fatalf("shutdown = %v, closed %v; want the plan closed", err, rt.Closed)
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %d times, want never: nothing of its link is open here", len(remote.Asked))
	}
}

// The host of a link that is open here is still asked, by the plan's survey
// and by the recheck: its agents are its word, and a busy one refuses the
// close (decisions.md D84, D99). A close of another project leaves it alone.
func TestACloseAsksTheHostOfTheLinkItCloses(t *testing.T) {
	c, rt, remote, pane := linked(t, hostAgent("box.4242", revier.StatusIdle))
	projects := []core.Project{prepared(t, project()), linkProject(t)}

	if c.SurveyToClose(context.Background(), projects, "revier"); len(remote.Asked) != 0 {
		t.Fatalf("a close of revier asked the link's host %d times, want it left alone", len(remote.Asked))
	}
	var plan []core.CloseStep
	for i, only := range []revier.ProjectName{"far", ""} {
		r := c.SurveyToClose(context.Background(), projects, only)
		if len(remote.Asked) != i+1 {
			t.Fatalf("SurveyToClose(%q) left the host asked %d times, want %d", only, len(remote.Asked), i+1)
		}
		plan = c.ShutdownPlan(r, only, core.ShutdownAll)
		if s, ok := stepFor(plan, pane); !ok || len(s.Agents) != 1 {
			t.Fatalf("plan for %q = %+v, want the workspace with the host's agent", only, plan)
		}
	}
	if _, err := c.Shutdown(context.Background(), plan, 0, reading(projects)); err != nil || len(rt.Closed) == 0 {
		t.Fatalf("shutdown = %v, closed %v; want the link's workspace closed", err, rt.Closed)
	}
	if len(remote.Asked) != 3 {
		t.Errorf("the host was asked %d times, want 3: once a plan and once by the recheck", len(remote.Asked))
	}
}

// A close of one project asks that project's host whatever the listing here
// says is open of it: a panel that shows its agent can sit in an instance the
// link no longer holds, and only the host says whose agent that is.
func TestACloseOfOneLinkAsksItsHostWithNothingOfItHeld(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{Project: revier.Project{Name: "far-there"}})
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}

	if c.SurveyToClose(context.Background(), []core.Project{linkProject(t)}, "far"); len(remote.Asked) != 1 {
		t.Errorf("the host was asked %d times, want once", len(remote.Asked))
	}
}
