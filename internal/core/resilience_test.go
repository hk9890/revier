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

// refusing is the project with one target whose launch argv cannot be
// rendered: {{.Vars.absent}} is a key the project does not declare.
func refusing() revier.Project {
	p := project()
	p.Targets = append(p.Targets, revier.Target{
		Name: "web",
		Window: &revier.Realization{
			Launch: []string{"browser", "{{.Vars.absent}}"},
			Match:  revier.Match{Class: "^browser$"},
		},
	})
	return p
}

func viewByName(v revier.ProjectView) map[revier.TargetName]revier.TargetView {
	out := map[revier.TargetName]revier.TargetView{}
	for _, tv := range v.Targets {
		out[tv.Name] = tv
	}
	return out
}

// A refused target reports itself unavailable with the reason, and the rest of
// the project surveys exactly as it did before. The survey is where a user
// reads why a target is not there, so the reason travels with it
// (decisions.md D85).
func TestSurveyReportsARefusedTargetWithItsReason(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty")
	win := hosttest.New("win")
	c := &core.Core{Runtime: rt, Window: win}

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, refusing())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	v := report.Views[0]
	by := viewByName(v)

	web := by["web"]
	if web.Available {
		t.Error("a target whose launch does not render must not report available")
	}
	if !strings.Contains(web.Reason, "absent") {
		t.Errorf("Reason = %q, want the template key that is missing", web.Reason)
	}
	// The others are untouched: this is the whole point of the change.
	if !by["editor"].Available || !by["diff"].Available {
		t.Errorf("the sound targets should still resolve: %+v", v.Targets)
	}
	if !v.Running {
		t.Error("home is up, so the project is running")
	}
}

// A target no host here can realize is the expected result on a machine
// without that host, and says nothing further. Only a target its own config
// refused carries a Reason, so the two never read alike.
func TestAMissingHostIsNotAReason(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt} // no window host

	report, err := c.Survey(context.Background(), []core.Project{prepared(t, project())}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	editor := viewByName(report.Views[0])["editor"]
	if editor.Available {
		t.Fatal("editor is window-only and no window host is configured")
	}
	if editor.Reason != "" {
		t.Errorf("Reason = %q, want none: nothing in the file is wrong", editor.Reason)
	}
}

// Pressing a refused target's key fails loudly, with the reason. Nothing is
// launched from a launch argv that did not render: this is the cost of
// keeping the project loaded, and it is paid at the one key it belongs to.
func TestGoOnARefusedTargetFailsWithTheReason(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	win := hosttest.New("win")
	c := &core.Core{Runtime: rt, Window: win}

	_, err := c.Go(context.Background(), prepared(t, refusing()), "web", nil)
	if err == nil {
		t.Fatal("want the keypress refused")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("err = %v, want the reason the target was refused", err)
	}
	if len(win.Opened) != 0 {
		t.Errorf("opened %v, want nothing launched", win.Opened)
	}
}

// An invalid project keeps its place in the view, carrying why. It is listed
// precisely so the reason has somewhere to be read.
func TestSurveyListsAnInvalidProject(t *testing.T) {
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt}

	p := core.PrepareProject(revier.Project{Name: "broken"})
	p.Invalid = errors.New("demo.toml: expected a key at line 2")

	report, err := c.Survey(context.Background(), []core.Project{p}, nil, nil)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	if len(report.Views) != 1 {
		t.Fatalf("%d views, want the invalid project listed", len(report.Views))
	}
	if got := report.Views[0].Invalid; !strings.Contains(got, "line 2") {
		t.Errorf("Invalid = %q, want the reason carried into the view", got)
	}
}
