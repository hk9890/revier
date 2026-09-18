package core_test

import (
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/pkg/revier"
)

// prepared is the form Go and Survey take: rendered and compiled once, as
// config.Load does for a real project file.
func prepared(t *testing.T, p revier.Project) core.Project {
	t.Helper()
	return core.PrepareProject(p)
}

// bareRuntime is a runtime with none of the optional capabilities, and
// bareWindow a window host with none: the port interface alone is embedded,
// so the fake's optional methods are not promoted. A test that needs a host
// without one capability - no PanelWriter, no PanelOpener, no WindowPlacer -
// wraps the fake in one of these.
type bareRuntime struct{ revier.Runtime }

type bareWindow struct{ revier.WindowController }

// editor is a window-realized target; diff has both realizations; home is the
// project's workspace on the runtime.
func project() revier.Project {
	return revier.Project{
		Name: "revier",
		Path: "/home/user/dev/github/revier",
		Targets: []revier.Target{
			{
				Name: "home", Home: true,
				Runtime: &revier.Realization{
					Launch: []string{"kitty"},
					Match:  revier.Match{Title: "^session:revier$"},
				},
			},
			{
				Name: "editor", Key: "ctrl-o",
				Window: &revier.Realization{
					Launch: []string{"code", "/home/user/dev/github/revier"},
					Match:  revier.Match{Class: "^code$"},
				},
			},
			{
				Name: "diff", Key: "ctrl-shift-d",
				Window: &revier.Realization{
					Launch: []string{"meld"}, Match: revier.Match{Class: "^meld$"},
				},
				Runtime: &revier.Realization{
					Launch: []string{"nvim", "-d"}, Match: revier.Match{Title: "^diff:revier$"},
				},
			},
		},
	}
}

// osWindowProject is a workspace and a runtime-only diff target, as a kitty
// user has them: both are OS windows a window host also lists.
func osWindowProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Launch: []string{"x"}, Match: revier.Match{Title: "^session:revier$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff:revier", Launch: []string{"x"}, Match: revier.Match{Title: "^diff:revier$"}}},
	}}
}

// osWindowHosts builds a runtime that reports OSWindows and a window host that
// sees the same two windows, and returns the window host's refs for them.
func osWindowHosts() (*hosttest.FakeRuntime, *hosttest.Fake, revier.TargetRef, revier.TargetRef) {
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{Layout: true, OSWindows: true})
	wm := hosttest.New("wm")
	rt.Add("session:revier", "kitty")
	rt.Add("diff:revier", "kitty")
	// The window host reports the pid the runtime reports (hosttest assigns
	// 1000+id), because every OS window of one kitty process shares it.
	homeWm := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	diffWm := wm.AddInstance(revier.Instance{Title: "diff:revier", Class: "kitty", PID: 1002})
	wm.AddInstance(revier.Instance{Title: "Some Editor", Class: "code", PID: 4242})
	return rt, wm, homeWm, diffWm
}
