package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/session"
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

// agentProject is a workspace whose home lists one tab, agent, that holds an
// agent beside a shell, which is the layout a resume has to find its way back
// into.
func agentProject() revier.Project {
	return revier.Project{
		Name: "revier",
		Path: "/home/user/dev/github/revier",
		Targets: []revier.Target{
			{
				Name: "home", Home: true,
				Runtime: &revier.Realization{
					Name:  "session:revier",
					Match: revier.Match{Title: "^session:revier$"},
					Tabs:  []revier.TargetName{"agent"},
				},
			},
			{
				Name: "notes",
				Runtime: &revier.Realization{
					Name: "notes:revier", Launch: []string{"less"},
					Match: revier.Match{Title: "^notes:revier$"},
				},
			},
			{
				Name: "editor",
				Window: &revier.Realization{
					Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
				},
			},
			{
				Name: "agent",
				Runtime: &revier.Realization{
					Inside: "home",
					Panels: []revier.PanelSpec{
						{Kind: revier.PanelShell, Command: []string{"zsh"}},
						{Kind: revier.PanelAgent, Title: "Claude Code", Command: []string{"claude", "--model", "opus"}},
					},
				},
			},
		},
	}
}

func resumable() *hosttest.FakeResumableProbe { return hosttest.NewResumableProbe("claude", "claude") }

// agent is a live panel the fake probe claims, holding a conversation when id
// is not empty, in dir when dir is not empty.
func agent(panel revier.PanelID, id, dir string) revier.Panel {
	vars := map[string]string{}
	if id != "" {
		vars["session"] = id
	}
	if dir != "" {
		vars["dir"] = dir
	}
	return revier.Panel{ID: panel, Kind: revier.PanelTool, Title: "claude", Vars: vars}
}

// launched restores one target from a recording and returns the panels the
// host was asked to open.
func launched(t *testing.T, c *core.Core, proj revier.Project, resumes []core.Resume) []revier.PanelSpec {
	t.Helper()
	return restored(t, c, proj, resumes).Opened[0].Panels
}

// restored restores one target from a recording and returns the runtime, for
// the panels it opened and the agent tabs it added after.
func restored(t *testing.T, c *core.Core, proj revier.Project, resumes []core.Resume) *hosttest.FakeRuntime {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c.Runtime = rt
	if _, err := pressResuming(context.Background(), c, prepared(t, proj), "home", resumes); err != nil {
		t.Fatalf("ActivateWaiting: %v", err)
	}
	if len(rt.Opened) != 1 {
		t.Fatalf("Opened = %+v, want one launch", rt.Opened)
	}
	return rt
}

// samePanels reports every difference between two panel lists.
func samePanels(t *testing.T, what string, got, want []revier.PanelSpec) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %+v, want %d panels", what, got, len(want))
	}
	for i := range want {
		if !slices.Equal(got[i].Command, want[i].Command) || got[i].Dir != want[i].Dir ||
			got[i].Kind != want[i].Kind || got[i].Title != want[i].Title {
			t.Errorf("%s panel %d = %+v, want %+v", what, i, got[i], want[i])
		}
	}
}

// agentPanel is a panel hosttest.TitleProbe claims for the harness "agent",
// in the state its title names.
func agentPanel(id, title string) revier.Panel {
	return revier.Panel{ID: revier.PanelID(id), Kind: revier.PanelTool, Title: title, Command: []string{"agent"}}
}

func shellPanel(id string) revier.Panel {
	return revier.Panel{ID: revier.PanelID(id), Kind: revier.PanelShell, Title: "zsh", Command: []string{"zsh"}}
}

// agentCore is a runtime holding the project's home workspace with the given
// panels, and the core reading it with hosttest.TitleProbe.
func agentCore(panels ...revier.Panel) (*core.Core, *hosttest.FakeRuntime) {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty", panels...)
	return &core.Core{Runtime: rt, Probes: []revier.AgentProbe{hosttest.TitleProbe{Harness: "agent"}}}, rt
}

// agentsOf is the agents a survey reports for the one project.
func agentsOf(t *testing.T, c *core.Core, p core.Project) []revier.AgentView {
	t.Helper()
	return survey(t, c, []core.Project{p}, nil).Views[0].Agents
}

// openWorkspace is agentProject with its home open on a fresh runtime, holding
// the given live panels.
func openWorkspace(t *testing.T, panels ...revier.Panel) (*core.Core, *hosttest.FakeRuntime, core.Project, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	c := &core.Core{Runtime: rt, Probes: []revier.AgentProbe{resumable()}}
	ref := rt.Add("session:revier", "", panels...)
	return c, rt, prepared(t, agentProject()), ref
}

// newAgent is `revier agent new -p <project>:<target>`: the workspace found,
// then the tab opened in it.
func newAgent(t *testing.T, c *core.Core, p core.Project, target revier.TargetName, r core.Resume) (core.AgentOutcome, error) {
	t.Helper()
	w, err := c.AgentWorkspace(context.Background(), p, target)
	if err != nil {
		t.Fatalf("AgentWorkspace: %v", err)
	}
	return c.NewAgent(context.Background(), w, r)
}

// linkProject is a link to far on buildbox, as a link's file loads: its home
// lists the tab agent, whose panels are the agent and the shell on the host by
// kind alone, so the core gives each the ssh that reaches it. logs is a window
// of its own on this machine.
func linkProject(t *testing.T) core.Project {
	t.Helper()
	return prepared(t, revier.Project{Name: "far", Remote: &revier.Link{Host: "buildbox", Project: "far-there"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{Name: "far", Match: revier.Match{Title: "^far$"},
			Tabs: []revier.TargetName{"agent"}}},
		{Name: "logs", Runtime: &revier.Realization{Name: "far-logs", Match: revier.Match{Title: "^far-logs$"},
			Launch: []string{"zsh"}}},
		{Name: "agent", Runtime: &revier.Realization{Inside: "home",
			Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
}

// sshArgv is the argv hosttest.FakeRemote gives a panel of linkProject, with
// what the core appended to it.
func sshArgv(kind revier.PanelKind, appended ...string) []string {
	return append([]string{"ssh", "buildbox", "revier", string(kind), "exec", "-p", "far-there"}, appended...)
}

// hostAgent is one agent as a link's host reports it: under the tag its panel
// on the other machine gave it, in the instance of the served processes.
func hostAgent(tag string, status revier.Status) revier.AgentView {
	return revier.AgentView{
		Panel: revier.PanelID(tag),
		Ref:   revier.TargetRef{Host: "proc", ID: "session:far-there"},
		State: revier.AgentState{Harness: "claude", Status: status},
	}
}

// linked is the link's workspace open here with one ssh panel on pid 4242, and
// the host answering with the agents given.
func linked(t *testing.T, agents ...revier.AgentView) (*core.Core, *hosttest.FakeRuntime, *hosttest.FakeRemote, revier.TargetRef) {
	t.Helper()
	remote := hosttest.NewRemote("buildbox", revier.ProjectView{Project: revier.Project{Name: "far-there"}, PathExists: true, Agents: agents})
	rt := hosttest.NewRuntime("rt")
	pane := rt.Add("far", "",
		revier.Panel{ID: "9", Kind: revier.PanelTool, PID: 4242, Title: "fixing the build", Command: []string{"ssh", "-t", "buildbox"}},
		revier.Panel{ID: "10", Kind: revier.PanelTool, PID: 4250, Command: []string{"ssh", "-t", "buildbox"}})
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	return c, rt, remote, pane
}

func stepFor(plan []core.CloseStep, ref revier.TargetRef) (core.CloseStep, bool) {
	i := slices.IndexFunc(plan, func(s core.CloseStep) bool { return s.Ref == ref })
	if i < 0 {
		return core.CloseStep{}, false
	}
	return plan[i], true
}

// openDesktop is agentProject with every target open: the workspace holds a
// shell and an agent in the given status, notes runs on its own, and the editor
// is a window.
func openDesktop(t *testing.T, status revier.Status) (*core.Core, *hosttest.FakeRuntime, *hosttest.Fake, []core.Project) {
	t.Helper()
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:revier", "kitty",
		revier.Panel{ID: "1", Kind: revier.PanelShell, Title: "zsh"},
		agent("2", "abc-123", ""),
	)
	rt.Add("notes:revier", "kitty", revier.Panel{ID: "3", Kind: revier.PanelTool, Title: "less"})
	wm := hosttest.New("wm")
	wm.Add("revier - code", "code")
	probe := &hosttest.FakeProbe{Harness: "claude", Marker: "claude", State: revier.AgentState{Harness: "claude", Status: status}}
	c := &core.Core{Runtime: rt, Window: wm, Probes: []revier.AgentProbe{probe}}
	return c, rt, wm, []core.Project{prepared(t, agentProject())}
}

// reading is the close every test asks for that is not about the busy guard:
// the normal path, whose recheck reads these projects again before it closes
// anything.
func reading(projects []core.Project) core.ShutdownOpts {
	return core.ShutdownOpts{Projects: projects}
}

func survey(t *testing.T, c *core.Core, projects []core.Project, attached map[revier.ProjectName][]revier.TargetRef) core.Report {
	t.Helper()
	if attached != nil {
		c.Ledger = attachments(attached)
	}
	r, err := c.Survey(context.Background(), projects)
	if err != nil {
		t.Fatalf("Survey: %v", err)
	}
	return r
}

// tabProject has a workspace that lists its agent tab, and a ticket viewer
// declared as a tab of it that it does not list.
func tabProject() revier.Project {
	return revier.Project{Name: "revier", Path: "/p", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "session:revier", Match: revier.Match{Title: "^session:revier$"},
			Tabs: []revier.TargetName{"agent"}}},
		{Name: "tickets", Key: "ctrl-shift-t", Runtime: &revier.Realization{
			Inside: "home", Launch: []string{"taskmgr-ui"}}},
		{Name: "agent", Runtime: &revier.Realization{
			Inside: "home", Panels: []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"claude"}}}}},
	}}
}

// tabHosts is a kitty-like runtime holding the workspace, and a window host
// that lists its OS window.
func tabHosts(t *testing.T) (*hosttest.FakeRuntime, *hosttest.Fake, revier.TargetRef) {
	t.Helper()
	rt := hosttest.NewRuntime("kitty")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	rt.Add("session:revier", "kitty", revier.Panel{ID: "1", Kind: revier.PanelTool})
	wm := hosttest.New("wm")
	osw := wm.AddInstance(revier.Instance{Title: "session:revier", Class: "kitty", PID: 1001})
	return rt, wm, osw
}

func popupArgv(context.Context) []string {
	return []string{"kitty", "--class", core.PopupClass, "-e", "revier"}
}

// surfaceTerminal is a window host and a runtime that pair one window with
// one terminal, as kitty and GNOME do: the terminal's panels are the given
// ones, and the second is the current one.
func surfaceTerminal(panels ...revier.Panel) (*hosttest.Fake, *hosttest.FakeRuntime, revier.TargetRef, revier.TargetRef) {
	wm := hosttest.New("wm")
	rt := hosttest.NewRuntime("rt")
	rt.SetCapabilities(revier.Capabilities{OSWindows: true})
	window := wm.AddInstance(revier.Instance{Title: "work", Class: "kitty", PID: 4000})
	terminal := rt.AddInstance(revier.Instance{Title: "work", Class: "kitty", PID: 4000, Panels: panels})
	wm.SetFocus(window)
	_ = rt.FocusPanel(context.Background(), terminal, panels[len(panels)-1].ID)
	rt.PanelFocuses = nil
	return wm, rt, window, terminal
}

// recording gives the test an event file of its own, and returns what is in
// it when called.
func recording(t *testing.T) func() []revier.Event {
	t.Helper()
	root := t.TempDir()
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
	return func() []revier.Event {
		t.Helper()
		got, err := events.Read(root, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range got {
			got[i].Time = time.Time{}
		}
		return got
	}
}

// remoteProject is a project on buildbox: its home target here is the ssh
// pane onto the workspace there.
func remoteProject(name string) revier.Project {
	return revier.Project{
		Name:   revier.ProjectName(name),
		Path:   "~/dev/" + name,
		Remote: &revier.Link{Host: "buildbox", Project: revier.ProjectName(name)},
		Targets: []revier.Target{{
			Name: "home", Home: true,
			Runtime: &revier.Realization{
				Name:   "session:" + name,
				Launch: []string{"ssh", "-t", "buildbox", "revier", "open", name, "--attach"},
				Match:  revier.Match{Title: "^session:" + name + "$"},
			},
		}},
	}
}

// restoreOf surveys the agent project and restores s over it.
func restoreOf(t *testing.T, c *core.Core, s session.Session) (core.Restored, *ledger, error) {
	t.Helper()
	projects := []core.Project{prepared(t, agentProject())}
	report := survey(t, c, projects, nil)
	l := &ledger{}
	c.Ledger = l
	out, back := c.Restore(context.Background(), s, report, projects)
	return out, l, back
}

// keyProject is a project with a home target and an editor target, each on a
// chord that another program's shortcut holds in these tests.
func keyProject(t *testing.T, name revier.ProjectName) core.Project {
	t.Helper()
	return prepared(t, revier.Project{
		Name: name,
		Path: "/home/user/dev/github/" + string(name),
		Targets: []revier.Target{
			{
				Name: "home", Home: true, Key: "ctrl-shift-u",
				Runtime: &revier.Realization{
					Launch: []string{"kitty"},
					Match:  revier.Match{Title: "^session:" + string(name) + "$"},
				},
			},
			{
				Name: "editor", Key: "ctrl-shift-o",
				Window: &revier.Realization{
					Launch: []string{"code"}, Match: revier.Match{Class: "^code$"},
				},
			},
		},
	})
}

func benchProjects(n int) []revier.Project {
	out := make([]revier.Project, n)
	for i := range out {
		out[i] = benchProject(i)
	}
	return out
}

// benchProject mirrors the real shape: templated names, matches and launch
// argv, five targets a key reaches, and the tab of two panels home lists.
func benchProject(i int) revier.Project {
	name := revier.ProjectName(fmt.Sprintf("project-%03d", i))
	return revier.Project{
		Name: name,
		Path: "/home/user/dev/" + string(name),
		Vars: map[string]string{"url": "https://example.invalid/" + string(name)},
		Targets: []revier.Target{
			{Name: "home", Home: true, Key: "ctrl-shift-h", Runtime: &revier.Realization{
				Name: "{{.Name}}", Match: revier.Match{Title: "^{{.Name}}$"},
				Tabs: []revier.TargetName{"agent"},
			}},
			{Name: "editor", Key: "ctrl-o", Window: &revier.Realization{
				Launch: []string{"code", "{{.Path}}"},
				Match:  revier.Match{Class: "^code$", Title: "{{.Name}}"},
			}},
			{Name: "pulls", Key: "ctrl-g", Window: &revier.Realization{
				Launch: []string{"chrome", "--app={{.Vars.url}}", "--class=revier-{{.Name}}"},
				Match:  revier.Match{Class: "^revier-{{.Name}}$"},
			}},
			{Name: "tickets", Key: "ctrl-t", Runtime: &revier.Realization{
				Name: "tickets-{{.Name}}", Launch: []string{"taskmgr-ui"},
				Match: revier.Match{Title: "^tickets-{{.Name}}$"},
			}},
			{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
				Name: "diff-{{.Name}}", Launch: []string{"nvim", "-d"},
				Match: revier.Match{Title: "^diff-{{.Name}}$"},
			}},
			{Name: "agent", Runtime: &revier.Realization{
				Inside: "home",
				Panels: []revier.PanelSpec{
					{Kind: revier.PanelAgent, Command: []string{"claude"}},
					{Kind: revier.PanelShell},
				},
			}},
		},
	}
}
