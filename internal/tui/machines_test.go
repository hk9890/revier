package tui_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// machinesWorld is a surface whose config.toml is text, over an ssh
// configuration of sshHosts, where each host is the fake remote of its name.
func machinesWorld(t *testing.T, text, sshHosts string, remotes map[string]*hosttest.FakeRemote) (tui.Model, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	if err := os.WriteFile(config.File(root), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte(sshHosts), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), NewRemote: func(host string) revier.Remote {
		if r, ok := remotes[host]; ok {
			return r
		}
		r := hosttest.NewRemote(host)
		r.Revier = "0.21.0"
		return r
	}}
	return resize(refreshed(t, c, nil, stateWith(t, nil), nil), 80, 20), root
}

// Each machine says what its revier answered before it is picked: its
// version, that the machine answers and runs none, or that it does not
// answer, and the pane says why.
func TestEachMachineShowsWhatItAnswered(t *testing.T) {
	up := hosttest.NewRemote("buildbox")
	up.Revier = "0.21.0"
	bare := hosttest.NewRemote("farbox")
	bare.Err = fmt.Errorf("farbox: no revier on the PATH ssh gives: %w", revier.ErrNoRevier)
	down := hosttest.NewRemote("deadbox")
	down.Err = errors.New("deadbox: no answer on port 22")
	text := "[[machine]]\nhost = \"buildbox\"\n\n[[machine]]\nhost = \"farbox\"\n\n[[machine]]\nhost = \"deadbox\"\n"
	m, _ := machinesWorld(t, text, "", map[string]*hosttest.FakeRemote{"buildbox": up, "farbox": bare, "deadbox": down})

	m = step(m, "alt+r")
	r := rows(m)
	for i, want := range []string{"revier 0.21.0", "no revier", "does not answer"} {
		if len(r) <= i || !strings.Contains(r[i], want) {
			t.Errorf("rows = %q, want row %d to say %q", r, i, want)
		}
	}
	m = resize(m, 120, 20)
	m = step(m, "down")
	m = step(m, "down")
	if p := pane(m); !strings.Contains(p, "deadbox") || !strings.Contains(p, "no answer on port 22") {
		t.Errorf("pane = %q, want why deadbox gave no version", p)
	}
}

// "new" offers the hosts of the ssh configuration that are not on the list,
// without this machine. Enter writes the host to config.toml at once, with
// the file's comments kept, and the machine is on the list, asked as the
// others were.
func TestNewAddsAHostFromTheSshConfiguration(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	text := "# my machines\n[[machine]]\nhost = \"buildbox\"\n"
	m, root := machinesWorld(t, text, "Host buildbox\nHost router\nHost "+machine+"\n", nil)

	m = step(m, "alt+r")
	m = step(m, "alt+n")
	if h := barLine(m); !strings.Contains(h, "Add a machine") {
		t.Errorf("header = %q, want the add step named", h)
	}
	r := rows(m)
	if len(r) < 1 || !strings.Contains(r[0], "router") || strings.Contains(screen(m), "buildbox") || strings.Contains(screen(m), machine) {
		t.Fatalf("rows = %q, want router alone", r)
	}

	m = step(m, "enter")
	got, err := os.ReadFile(config.File(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "# my machines") || !strings.Contains(string(got), "host = \"router\"") {
		t.Errorf("config.toml =\n%s\nwant router added and the comment kept", got)
	}
	r = rows(m)
	if len(r) < 2 || !strings.Contains(r[1], "router") || !strings.Contains(r[1], "revier 0.21.0") {
		t.Errorf("rows = %q, want router on the list, asked", r)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "router") {
		t.Errorf("selected %q, want the cursor on the machine added", row)
	}
}

// With every host of the ssh configuration on the list, "new" says so and
// opens no empty list.
func TestNewWithEveryHostOnTheListSaysSo(t *testing.T) {
	m, _ := machinesWorld(t, "[[machine]]\nhost = \"buildbox\"\n", "Host buildbox\n", nil)
	m = step(m, "alt+r")
	m = step(m, "alt+n")
	if f := footer(m); !strings.Contains(f, "every host in") {
		t.Errorf("footer = %q, want every host said to be on the list", f)
	}
	if h := barLine(m); !strings.Contains(h, "Remote machines") {
		t.Errorf("header = %q, want the machines still up", h)
	}
}

// "remove" takes a machine no link names off the list at once, and its
// table out of config.toml; the file's comments stay.
func TestRemoveTakesAMachineWithNoLinkOffAtOnce(t *testing.T) {
	text := "# my machines\n[[machine]]\nhost = \"buildbox\"\n\n[[machine]]\nhost = \"farbox\" # the far one\n"
	m, root := machinesWorld(t, text, "", nil)
	m = step(m, "alt+r")
	m = step(m, "down")
	m = step(m, "alt+d")
	if body := screen(m); strings.Contains(body, "farbox") || !strings.Contains(body, "buildbox") {
		t.Errorf("screen =\n%s\nwant farbox off the list", body)
	}
	got, err := os.ReadFile(config.File(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"farbox"`) || !strings.Contains(string(got), "# my machines") || !strings.Contains(string(got), "# the far one") {
		t.Errorf("config.toml =\n%s\nwant farbox gone and the comments kept", got)
	}
}

// A remove on the last row leaves the cursor on the new last row, and a
// machine added by hand since the screen opened is listed and asked.
func TestRemoveOnTheLastRowKeepsTheCursorThereAndAsksAMachineAddedByHand(t *testing.T) {
	text := "[[machine]]\nhost = \"buildbox\"\n\n[[machine]]\nhost = \"farbox\"\n\n[[machine]]\nhost = \"deadbox\"\n"
	m, root := machinesWorld(t, text, "", nil)
	m = step(m, "alt+r")
	if err := config.AddMachine(root, "router"); err != nil {
		t.Fatal(err)
	}
	m = step(m, "down")
	m = step(m, "down")
	m = step(m, "alt+d")
	if row := selectedRow(t, m); !strings.Contains(row, "router") {
		t.Errorf("selected %q, want the cursor on the new last row", row)
	}
	if r := rows(m); len(r) < 3 || !strings.Contains(r[2], "router") || !strings.Contains(r[2], "revier 0.21.0") {
		t.Errorf("rows = %q, want router listed and asked", r)
	}
}

// A remove's question holds the rows still: a second click on a row is no
// Enter that answers it.
func TestARemoveQuestionHoldsTheRowsStill(t *testing.T) {
	m, _, root := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha")
	m = step(m, "alt+r")
	m = step(m, "alt+d")
	_, x := margins(m)
	y := -1
	for i, line := range strings.Split(drawn(m), "\n") {
		if left, _, _ := strings.Cut(line, "│"); strings.Contains(left, "buildbox") && !strings.Contains(left, "Take") {
			y = i
			break
		}
	}
	m = clickAt(m, x+5, y)
	m = clickAt(m, x+5, y)
	cfg, err := config.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Declares("buildbox") {
		t.Error("a double click under the question took buildbox off the list")
	}
	if body := screen(m); !strings.Contains(body, "Take buildbox off the list?") {
		t.Errorf("screen =\n%s\nwant the question still up", body)
	}
}

// "remove" on a machine a link names asks first, and says the link stays;
// Esc keeps the machine, and Enter takes it off with the link left here.
func TestRemoveAsksFirstForAMachineALinkNames(t *testing.T) {
	m, _, root := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha")
	m = step(m, "alt+r")
	m = step(m, "alt+d")
	if body := screen(m); !strings.Contains(body, "Take buildbox off the list? 1 link to it stays here.") {
		t.Fatalf("screen =\n%s\nwant the question, and the link said to stay", body)
	}
	m = step(m, "esc")
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want buildbox kept", r)
	}

	m = step(m, "alt+d")
	m = step(m, "enter")
	cfg, err := config.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Declares("buildbox") {
		t.Error("buildbox is still declared, want it off the list")
	}
	m = step(m, "esc")
	if body := screen(m); !strings.Contains(body, "alpha") {
		t.Errorf("screen =\n%s\nwant the link still a row", body)
	}
}
