package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

// hostWith is an app whose only host, buildbox, has the named projects.
func hostWith(t *testing.T, names ...string) (*app, *hosttest.FakeRemote) {
	t.Helper()
	var views []revier.ProjectView
	for _, n := range names {
		views = append(views, revier.ProjectView{Project: revier.Project{Name: revier.ProjectName(n), Path: "/home/user/dev/" + n}, PathExists: true})
	}
	remote := hosttest.NewRemote("buildbox", views...)
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), NewRemote: func(string) revier.Remote { return remote }}
	a := withState(t, &app{cfg: &config.Config{}, cfgRoot: t.TempDir(), stateRoot: t.TempDir(), core: c}, &state.State{})
	return a, remote
}

// `revier link <host>` is the host's projects, with the link here that
// already points at each.
func TestLinkListsTheHostsProjectsAndWhatIsLinked(t *testing.T) {
	a, _ := hostWith(t, "far", "other")
	a.projects = []core.Project{remoteProject(t)}

	out := output(t, a, func() error { return cmdLink(context.Background(), a, []string{"buildbox"}) })
	for _, want := range []string{"far", "/home/user/dev/far", "other"} {
		if !strings.Contains(out, want) {
			t.Errorf("table = %q, want %s", out, want)
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasSuffix(strings.TrimSpace(lines[1]), "far") || !strings.HasSuffix(strings.TrimSpace(lines[2]), "-") {
		t.Errorf("table = %q, want far linked as far and other unlinked", out)
	}
}

// `revier link` lists the machines config.toml declares, as the remote
// screen does, and not every host the ssh configuration names.
func TestLinkListsTheDeclaredMachines(t *testing.T) {
	a, _ := hostWith(t)
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\nHost github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)

	out := output(t, a, func() error { return cmdLink(context.Background(), a, nil) })
	if !strings.Contains(out, "no machines in "+config.File(a.cfgRoot)) || !strings.Contains(out, "alt+n") {
		t.Errorf("printed %q, want no machines and how to add one", out)
	}

	if err := config.AddMachine(a.cfgRoot, "buildbox"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Read(a.cfgRoot)
	if err != nil {
		t.Fatal(err)
	}
	a.cfg = cfg
	out = output(t, a, func() error { return cmdLink(context.Background(), a, nil) })
	if out != "buildbox\n" {
		t.Errorf("printed %q, want buildbox alone", out)
	}
}

// A host need not be declared to be linked.
func TestLinkAsksAHostThatIsNotDeclared(t *testing.T) {
	a, remote := hostWith(t, "far")
	out := output(t, a, func() error { return cmdLink(context.Background(), a, []string{"buildbox"}) })
	if !strings.Contains(out, "far") {
		t.Errorf("table = %q, want the host's project", out)
	}
	if len(remote.Asked) != 1 {
		t.Errorf("asked %v, want the host asked once", remote.Asked)
	}
}

// This machine is refused before it is asked: the ask would be ssh to
// itself, and the link would be left out of every load (decisions.md D131).
func TestLinkRefusesThisMachineBeforeAskingIt(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	a, remote := hostWith(t, "far")
	for _, args := range [][]string{{machine}, {machine, "far"}} {
		err := cmdLink(context.Background(), a, args)
		if err == nil || !strings.Contains(err.Error(), "is this machine") {
			t.Errorf("link %v: err = %v, want this machine refused", args, err)
		}
	}
	if len(remote.Asked) != 0 {
		t.Errorf("asked %v, want no ask of this machine", remote.Asked)
	}
}

// `revier link <host> <project>` writes the link and loads it: the host is
// asked first, and a project it does not have is refused before any file.
func TestLinkWritesALinkToAProjectTheHostHas(t *testing.T) {
	a, _ := hostWith(t, "far")

	out := output(t, a, func() error { return cmdLink(context.Background(), a, []string{"buildbox", "far", "--name", "build"}) })
	file := config.ProjectFile(a.cfgRoot, "build")
	if !strings.Contains(out, file) {
		t.Errorf("printed %q, want the file named", out)
	}
	body, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(body), `host = "buildbox"`) || !strings.Contains(string(body), `project = "far"`) {
		t.Errorf("file = %q, %v; want the remote table with both names", body, err)
	}
	if len(a.projects) != 1 || a.projects[0].Name != "build" || a.projects[0].Remote.Project != "far" {
		t.Errorf("projects = %+v, want the link loaded", a.projects)
	}

	err = cmdLink(context.Background(), a, []string{"buildbox", "nope"})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("err = %v, want the missing project refused", err)
	}
	if _, err := os.Stat(config.ProjectFile(a.cfgRoot, "nope")); err == nil {
		t.Error("a refused link left a file behind")
	}
	err = cmdLink(context.Background(), a, []string{"buildbox", "far", "--name", "build"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want the taken name refused", err)
	}
}

func TestLinkReportsAHostThatDoesNotAnswer(t *testing.T) {
	a, remote := hostWith(t)
	remote.Err = errors.New("buildbox: connection refused")
	err := cmdLink(context.Background(), a, []string{"buildbox"})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want the ssh failure", err)
	}
}
