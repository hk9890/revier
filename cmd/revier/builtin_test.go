package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// A runtime starts the panel in its own environment, so the roots this
// command resolved go with the panel's command, in full: the agent edits the
// configuration the surface reads, and starts in the project's directory.
func TestTheAssistantRunsWithTheRootsItWasGiven(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	st := t.TempDir()
	p, err := assistProject("cfg", st)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(p.Path); err != nil || !info.IsDir() {
		t.Fatalf("the assistant's directory %s is not there: %v", p.Path, err)
	}
	tab := slices.IndexFunc(p.Targets, func(t revier.Target) bool { return t.Name == "agent" })
	command := p.Targets[tab].Runtime.Panels[0].Command
	for _, want := range []string{"REVIER_CONFIG_HOME=" + filepath.Join(here, "cfg"), "REVIER_STATE_HOME=" + st} {
		if !slices.Contains(command, want) {
			t.Errorf("panel command = %q, want %q in it", command, want)
		}
	}
	if !slices.Equal(command[len(command)-2:], []string{"assist", "--exec"}) {
		t.Errorf("panel command = %q, want it to end in assist --exec", command)
	}
}

// The assistant is no project of the user's: a list of it alone still says
// where to add one.
func TestAListOfTheAssistantAloneSaysWhereToAddOne(t *testing.T) {
	configRoot(t, "", nil)
	builtin := config.Builtin(t.TempDir(), []string{"/bin/revier", "assist", "--exec"})
	c := &core.Core{Runtime: hosttest.NewRuntime("tmux"), Window: hosttest.New("gnome")}
	a := withState(t, &app{cfg: &config.Config{}, projects: []core.Project{builtin}, assistant: builtin, stateRoot: t.TempDir(), core: c}, &state.State{})

	out := output(t, a, func() error { return cmdList(context.Background(), a, nil) })
	if !strings.Contains(out, string(config.Assistant)) || !strings.Contains(out, "no projects. add one") {
		t.Errorf("list = %q, want the assistant's row and where to add a project", out)
	}
}

// The panel closes when --exec ends: with no terminal to wait on, a failure
// is returned at once, and with its own exit status.
func TestAssistExecWithNoAgentFailsAtOnce(t *testing.T) {
	configRoot(t, "", nil)
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	testenv.NoTools(t)

	if err := run(&strings.Builder{}, []string{"assist", "--exec"}); !errors.Is(err, errNoAssistant) {
		t.Errorf("err = %v, want %v", err, errNoAssistant)
	}
}
