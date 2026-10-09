package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
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
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
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

// ledger is a ledger in memory: the state of one test, and every launch and
// landing written to it, in order. It hands out copies, as the file does, so
// a state read earlier is not changed by a later write. An event goes to the
// event file of the test, where recording reads it.
type ledger struct {
	mu     sync.Mutex
	st     state.State
	writes []string
}

func (l *ledger) State() *state.State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.copy()
}

func (l *ledger) Update(apply func(*state.State) bool) *state.State {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.copy(); apply(st) {
		if st.Launch != nil && (l.st.Launch == nil || *st.Launch != *l.st.Launch) {
			l.writes = append(l.writes, fmt.Sprintf("launched %s/%s", st.Launch.Project, st.Launch.Target))
		}
		for p, targets := range st.Bound {
			for t, ref := range targets {
				if l.st.Bound[p][t] != ref {
					l.writes = append(l.writes, fmt.Sprintf("landed %s/%s", p, t))
				}
			}
		}
		l.st = *st
	}
	return l.copy()
}

func (l *ledger) Record(e revier.Event) { events.Record(e) }

func (l *ledger) copy() *state.State {
	b, err := json.Marshal(l.st)
	if err != nil {
		panic(err)
	}
	var st state.State
	if err := json.Unmarshal(b, &st); err != nil {
		panic(err)
	}
	return &st
}

// bindings is a ledger that holds where a project's targets last landed.
func bindings(p revier.ProjectName, b core.Bindings) *ledger {
	l := &ledger{}
	for t, ref := range b {
		l.st.Bind(p, t, ref)
	}
	return l
}

// pendingLaunch is a ledger with a launch of the project's target on record now,
// its window still to come.
func pendingLaunch(p revier.ProjectName, t revier.TargetName) *ledger {
	return &ledger{st: state.State{Launch: &state.Launch{Project: p, Target: t, At: time.Now()}}}
}

// attachments is a ledger that holds what was attached to each project.
func attachments(attached map[revier.ProjectName][]revier.TargetRef) *ledger {
	return &ledger{st: state.State{Attached: attached}}
}

// press is one press of a target, as every surface makes it.
func press(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, nil)
	return res, err
}

// pressResuming is press with the conversations a restore starts the agents
// of a launch on.
func pressResuming(ctx context.Context, c *core.Core, p core.Project, name revier.TargetName, resumes []core.Resume) (core.Result, error) {
	_, res, err := c.ActivateWaiting(ctx, p, name, resumes)
	return res, err
}

// landed is where the ledger says the project revier's target last landed,
// or zero.
func landed(t *testing.T, c *core.Core, name revier.TargetName) revier.TargetRef {
	t.Helper()
	return c.Ledger.State().Bound["revier"][name]
}

// detached is a window host whose launch names no window, as a real one's
// does not, and whose windows appear by themselves: appear runs in place of
// the fake's own new window.
type detached struct {
	*hosttest.Fake
	appear func()
}

func (d detached) Open(context.Context, revier.Realization) (revier.TargetRef, error) {
	d.appear()
	return revier.TargetRef{}, nil
}

// shortBindWait makes the wait for a detached launch's window short, for a
// test whose window never comes.
func shortBindWait(t *testing.T) {
	t.Helper()
	was := core.BindWait
	core.BindWait = 20 * time.Millisecond
	t.Cleanup(func() { core.BindWait = was })
}

// attachedAfterAction is what the project has attached once an action of it
// launched and one window appeared on wm, the core's window host: a first
// settle of the windows there before, then one with the new window.
func attachedAfterAction(t *testing.T, c *core.Core, wm *hosttest.Fake, p core.Project, appear revier.Instance) []revier.TargetRef {
	t.Helper()
	now := time.Now()
	projects := []core.Project{p}
	c.Ledger = &ledger{st: state.State{Launch: &state.Launch{Project: p.Name, At: now}}}
	for range 2 {
		r, err := c.Survey(context.Background(), projects)
		if err != nil {
			t.Fatal(err)
		}
		c.Settle(r, projects, now)
		if appear.Class != "" {
			wm.AddInstance(appear)
			appear = revier.Instance{}
		}
	}
	return c.Ledger.State().Attached[p.Name]
}
