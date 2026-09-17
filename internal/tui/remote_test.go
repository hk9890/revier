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
// checkout is there, and its one agent is in the given state.
func hostSays(name string, status revier.Status) revier.ProjectView {
	return revier.ProjectView{
		Project:    revier.Project{Name: revier.ProjectName(name), Path: "/home/hans/dev/" + name},
		PathExists: true,
		Agents:     []revier.AgentView{{Panel: "1", State: revier.AgentState{Harness: "claude", Status: status}}},
	}
}

// A remote project's row shows what its host's agent is doing, read from
// the host, and the pane names the host.
func TestARemoteProjectShowsItsHostsAgent(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusAttention))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := resize(refreshed(t, c, remoteOnDisk(t, "alpha"), stateWith(t, nil), nil), 80, 20)

	if row := rows(m)[0]; !strings.Contains(row, theme.Default().Glyphs.NeedsYou+"1") {
		t.Errorf("row = %q, want the host's attention", row)
	}
	if body := pane(resize(m, 140, 30)); !strings.Contains(body, "buildbox") {
		t.Errorf("pane = %q, want the host named", body)
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

// Enter on a remote project opens the pane here whatever the host said
// about its checkout: cloning is the host's, done by the revier the pane
// runs there.
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
	if len(rt.Opened) != 1 || rt.Opened[0].Launch[0] != "ssh" {
		t.Errorf("runtime Opened = %v, want the ssh pane", rt.Opened)
	}
}
