//go:build live

// Layer L4: the core's run-or-raise against a real hc core, headless. L2
// proves the core against a fake; this proves it against the host. hc keeps
// no focus, so a second press raises what the first opened rather than
// returning home.
package hc_test

import (
	"context"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func TestCoreRunOrRaiseAgainstRealHc(t *testing.T) {
	h, c := world(t), ctx(t)
	cr := &core.Core{Runtime: h}
	sleep := []string{"sh", "-c", "sleep 30"}
	p := core.PrepareProject(revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "home", Home: true, Runtime: &revier.Realization{
				Name: "home", Launch: sleep, Match: revier.Match{Title: "^home$"},
			}},
			{Name: "diff", Runtime: &revier.Realization{
				Name: "diff", Launch: sleep, Match: revier.Match{Title: "^diff$"},
			}},
		},
	})

	home, err := press(c, cr, p, "home")
	if err != nil {
		t.Fatalf("open home: %v", err)
	}
	diff, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("open diff: %v", err)
	}
	if diff.Ref.ID == home.Ref.ID {
		t.Fatal("diff and home must be different instances")
	}
	again, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diff.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.Ref.ID, diff.Ref.ID)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("got %d instances, want 2: run-or-raise must not duplicate: %+v", len(instances), instances)
	}
}

func press(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, nil)
	return res, err
}
