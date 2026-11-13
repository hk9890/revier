package panelstart_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// A program not on PATH is refused by the panel's kind and its program; a
// shell, and a program named by a path, are left to the runtime.
func TestStartableRefusesAProgramNotOnPATH(t *testing.T) {
	testenv.NoTools(t)

	shell := revier.PanelSpec{Kind: revier.PanelShell}
	byPath := revier.PanelSpec{Kind: revier.PanelTool, Command: []string{"./missing"}}
	if err := panelstart.Startable([]revier.PanelSpec{shell, byPath}); err != nil {
		t.Fatalf("Startable = %v, want nil", err)
	}

	missing := revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"no-such-agent", "--flag"}}
	err := panelstart.Startable([]revier.PanelSpec{shell, missing})
	if want := `the agent panel's command "no-such-agent" is not on PATH`; err == nil || err.Error() != want {
		t.Fatalf("Startable = %v, want %q", err, want)
	}
}

// The PATH searched is the one given, not this process's: herdr starts a
// panel with its server's. Only an executable file there is a program.
func TestStartableOnSearchesTheGivenPATH(t *testing.T) {
	testenv.NoTools(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"agent"}}
	if err := panelstart.StartableOn([]revier.PanelSpec{agent}, dir); err != nil {
		t.Errorf("StartableOn(%s) = %v, want nil", dir, err)
	}
	err := panelstart.StartableOn([]revier.PanelSpec{agent}, "")
	if want := `the agent panel's command "agent" is not on PATH`; err == nil || err.Error() != want {
		t.Errorf("StartableOn on an empty PATH = %v, want %q", err, want)
	}
	plain := revier.PanelSpec{Kind: revier.PanelTool, Command: []string{"plain"}}
	err = panelstart.StartableOn([]revier.PanelSpec{plain}, dir)
	if want := `the tool panel's command "plain" is not on PATH`; err == nil || err.Error() != want {
		t.Errorf("StartableOn with a file that is not executable = %v, want %q", err, want)
	}
}

// A bare program is resolved to its file on the given PATH, its arguments
// kept; a program named by a path is kept as written, and a panel with no
// command has none. A program not on PATH is refused as StartableOn refuses
// it.
func TestCommandsOnResolvesEachBareProgram(t *testing.T) {
	testenv.NoTools(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := panelstart.CommandsOn([]revier.PanelSpec{
		{Kind: revier.PanelAgent, Command: []string{"agent", "-c"}},
		{Kind: revier.PanelTool, Command: []string{"./tool"}},
		{Kind: revier.PanelShell},
	}, dir)
	want := [][]string{{filepath.Join(dir, "agent"), "-c"}, {"./tool"}, nil}
	if err != nil || !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("CommandsOn = %q, %v; want %q", got, err, want)
	}
	_, err = panelstart.CommandsOn([]revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"agent"}}}, "")
	if want := `the agent panel's command "agent" is not on PATH`; err == nil || err.Error() != want {
		t.Errorf("CommandsOn on an empty PATH = %v, want %q", err, want)
	}
}

// A panel is named by its kind, and a launch, which has none, by its command
// alone.
func TestExitedAtOnceNamesThePanel(t *testing.T) {
	for _, tc := range []struct {
		panel revier.PanelSpec
		want  string
	}{
		{revier.PanelSpec{Kind: revier.PanelShell}, "the shell panel's shell exited as soon as it started"},
		{revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"claude", "-c"}}, `the agent panel's command "claude -c" exited as soon as it started`},
		{revier.PanelSpec{Command: []string{"htop"}}, `the command "htop" exited as soon as it started`},
	} {
		if got := panelstart.ExitedAtOnce(tc.panel).Error(); got != tc.want {
			t.Errorf("ExitedAtOnce(%+v) = %q, want %q", tc.panel, got, tc.want)
		}
	}
}

// The runtime's reason follows the panel's name, and stays an error of its
// own that a caller can still find.
func TestDidNotStartNamesThePanelAndKeepsTheReason(t *testing.T) {
	reason := errors.New("no interpreter")
	for _, tc := range []struct {
		panel revier.PanelSpec
		want  string
	}{
		{revier.PanelSpec{Kind: revier.PanelShell}, "the shell panel's shell did not start: no interpreter"},
		{revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"claude", "-c"}}, `the agent panel's command "claude -c" did not start: no interpreter`},
	} {
		err := panelstart.DidNotStart(tc.panel, reason)
		if err.Error() != tc.want || !errors.Is(err, reason) {
			t.Errorf("DidNotStart(%+v) = %q, want %q wrapping the reason", tc.panel, err, tc.want)
		}
	}
}
