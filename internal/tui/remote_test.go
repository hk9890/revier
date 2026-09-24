package tui_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// remoteFile is a project on buildbox: its path is in that machine's terms,
// and the home target here is the ssh pane onto the workspace there.
const remoteFile = `
[remote]
host = "buildbox"
`

// remoteOnDisk writes one remote project file and loads it the way the CLI
// does.
func remoteOnDisk(t *testing.T, name string) []core.Project {
	t.Helper()
	dir := t.TempDir()
	body := strings.ReplaceAll(remoteFile, "%NAME%", name)
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	projects, err := config.LoadProjects(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return projects
}

// hostSays is what the revier on buildbox answers about a project: the
// checkout is there, and its one agent is in the given state. The agent is
// named by the tag a panel of machine box on pid 4242 gave it, which is what
// openHere opens.
func hostSays(name string, status revier.Status) revier.ProjectView {
	return revier.ProjectView{
		Project:    revier.Project{Name: revier.ProjectName(name), Path: "/home/user/dev/" + name},
		PathExists: true,
		Agents:     []revier.AgentView{{Panel: "box.4242", State: revier.AgentState{Harness: "claude", Status: status}}},
	}
}

// openHere is the link's workspace open on this machine: one panel running
// the ssh, on the pid the host tags what that panel started with.
func openHere(name string) *hosttest.FakeRuntime {
	rt := hosttest.NewRuntime("rt")
	rt.Add("session:"+name, "kitty", revier.Panel{ID: "9", Kind: revier.PanelTool, PID: 4242,
		Command: []string{"ssh", "-t", "buildbox"}})
	return rt
}

// A link with nothing open here sorts with the closed projects, however
// much its host's agent wants the user: that agent is on no row (D104), so
// it lifts nothing above the projects that are open here.
func TestALinkNothingHereHoldsSortsBelowTheOpenProjects(t *testing.T) {
	rt, _, _, local := world(t, 3) // project-02 is open
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusAttention))
	c := &core.Core{Runtime: rt, Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote},
		Probes: []revier.AgentProbe{&hosttest.FakeProbe{
			Harness: "claude", Marker: "claude",
			State: revier.AgentState{Harness: "claude", Status: revier.StatusRunning},
		}}}
	projects := append(remoteOnDisk(t, "alpha"), local...)
	m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20)

	// Rows are two lines: the name, then the path under it.
	for i, name := range []string{"project-02", "alpha@buildbox", "project-00", "project-01"} {
		if row := rows(m)[i*2]; !strings.Contains(row, name) {
			t.Errorf("row %d = %q, want %s", i, row, name)
		}
	}
}

// A remote project's row shows what its host's agent is doing, read from
// the host, and the pane names the host. The agent is in a panel here, so
// the row is one the user can act on.
func TestARemoteProjectShowsItsHostsAgent(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusAttention))
	c := &core.Core{Runtime: openHere("alpha"), Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	if row := rows(m)[0]; !strings.Contains(row, theme.Default().Glyphs.NeedsYou+" 1") {
		t.Errorf("row = %q, want the host's attention", row)
	}
	if body := pane(resize(m, 140, 30)); !strings.Contains(body, "buildbox") {
		t.Errorf("pane = %q, want the host named", body)
	}
}

// A link with nothing open here counts no agents, whatever its host reports.
// The host lists every agent in its project, including ones opened on that
// machine, and no panel here shows those: the row would otherwise count an
// agent that Enter cannot reach, in a project the same row calls closed
// (decisions.md D104).
func TestALinkNothingHereHoldsCountsNoAgent(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusAttention))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Machine: "box", Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	if row := rows(m)[0]; strings.Contains(row, theme.Default().Glyphs.NeedsYou) {
		t.Errorf("row = %q, want no agent: the project is closed here", row)
	}
	if body := pane(resize(m, 140, 30)); strings.Contains(body, "Agents") {
		t.Errorf("pane = %q, want no agents section", body)
	}
}

// The row names the host after the project, so two projects of one name
// on two machines read apart, and the icon column says it is remote.
func TestARemoteProjectsRowNamesItsHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	row := rows(m)[0]
	if !strings.Contains(row, "alpha@buildbox") {
		t.Errorf("row = %q, want alpha@buildbox", row)
	}
	if !strings.Contains(row, theme.Default().Glyphs.Remote) {
		t.Errorf("row = %q, want the remote glyph in the icon column", row)
	}
}

// An action on a remote project runs on the host: the remote is asked for
// the command, and the action need not exist in the configuration here.
func TestAnActionOnARemoteProjectRunsOnTheHost(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	remote.RunArgv = []string{"true"}
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), []config.Action{{Key: "ctrl-y", Name: "sync"}})

	_, cmd := send(m, tea.KeyMsg{Type: tea.KeyCtrlY})
	if cmd == nil {
		t.Fatal("ctrl+y ran no action")
	}
	if len(remote.Runs) != 1 || remote.Runs[0] != (hosttest.Run{Project: "alpha", Action: "sync"}) {
		t.Errorf("runs = %+v, want sync on alpha", remote.Runs)
	}
}

// A host that did not answer is said in the pane, in full.
func TestAnUnreachableHostIsSaidInThePane(t *testing.T) {
	remote := hosttest.NewRemote("buildbox")
	remote.Err = errors.New("buildbox: connection refused")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	body := pane(resize(m, 140, 30))
	if !strings.Contains(body, "unreachable") || !strings.Contains(body, "connection refused") {
		t.Errorf("pane = %q, want the status and the failure", body)
	}
}

// A checkout missing on the host is said in the pane, naming the host.
func TestAMissingCheckoutOnTheHostNamesTheHost(t *testing.T) {
	said := hostSays("alpha", revier.StatusIdle)
	said.PathExists, said.Agents = false, nil
	remote := hosttest.NewRemote("buildbox", said)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	if body := pane(resize(m, 140, 30)); !strings.Contains(body, "not on buildbox") {
		t.Errorf("pane = %q, want the host named", body)
	}
}

// Enter on a remote project opens its workspace here whatever the host said
// about its checkout: cloning is the host's, done by the revier the agent
// panel runs there.
func TestEnterOnARemoteProjectOpensThePaneWithoutCloning(t *testing.T) {
	said := hostSays("alpha", revier.StatusIdle)
	said.PathExists, said.Agents = false, nil
	rt := hosttest.NewRuntime("rt")
	remote := hosttest.NewRemote("buildbox", said)
	c := &core.Core{Runtime: rt, Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil)

	_, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("want the launch command")
	}
	cmd()
	if len(rt.Opened) != 1 || len(rt.Opened[0].Panels) != 2 {
		t.Errorf("runtime Opened = %v, want the agent and the shell over ssh", rt.Opened)
	}
}
