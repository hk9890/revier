//go:build live

// Layer L4: the core's run-or-raise against a real herdr server, headless.
// L2 proves the core against a fake that has no opinion about focus; this
// proves it against the host.
package herdr_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

func TestCoreRunOrRaiseAgainstRealHerdr(t *testing.T) {
	h, c := server(t), ctx(t)
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
			{Name: "agent", Runtime: &revier.Realization{Inside: "home", Panels: []revier.PanelSpec{
				{Kind: revier.PanelAgent, Command: sleep},
				{Kind: revier.PanelShell},
			}}},
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
		t.Fatal("diff and home must be different workspaces")
	}

	// Opening left diff focused, so the second press is the round trip home.
	back, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if back.Ref.ID != home.Ref.ID {
		t.Fatalf("toggle-back returned %s, want home %s", back.Ref.ID, home.Ref.ID)
	}

	// Third press raises the workspace rather than opening another.
	again, err := press(c, cr, p, "diff")
	if err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if again.Ref.ID != diff.Ref.ID {
		t.Errorf("raise returned %s, want the existing %s", again.Ref.ID, diff.Ref.ID)
	}

	// A tab target opens inside home, and a second press finds it by its
	// vars rather than opening another tab.
	agent, err := press(c, cr, p, "agent")
	if err != nil {
		t.Fatalf("open agent: %v", err)
	}
	if agent.Ref.ID != home.Ref.ID {
		t.Fatalf("agent opened in %s, want inside home %s", agent.Ref.ID, home.Ref.ID)
	}
	if _, err := press(c, cr, p, "diff"); err != nil {
		t.Fatalf("raise diff: %v", err)
	}
	if _, err := press(c, cr, p, "agent"); err != nil {
		t.Fatalf("raise agent: %v", err)
	}

	instances, err := h.Instances(c)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("got %d workspaces, want 2: run-or-raise must not duplicate", len(instances))
	}
	i := slices.IndexFunc(instances, func(in revier.Instance) bool { return in.Ref.ID == home.Ref.ID })
	if i < 0 || len(instances[i].Panels) != 3 {
		t.Fatalf("instances = %+v, want home with its own panel and the agent tab's two", instances)
	}
	focused, err := h.FocusedPanel(c, home.Ref)
	if err != nil {
		t.Fatalf("FocusedPanel: %v", err)
	}
	if j := slices.IndexFunc(instances[i].Panels, func(p revier.Panel) bool { return p.ID == focused }); j < 0 ||
		instances[i].Panels[j].Vars[core.PanelTargetVar] != "agent" {
		t.Errorf("FocusedPanel = %q, want a panel of the agent tab among %+v", focused, instances[i].Panels)
	}
}

func press(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, nil)
	return res, err
}
