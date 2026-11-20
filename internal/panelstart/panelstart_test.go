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
func TestCommandsOnRefusesAProgramNotOnPATH(t *testing.T) {
	path := t.TempDir()

	shell := revier.PanelSpec{Kind: revier.PanelShell}
	byPath := revier.PanelSpec{Kind: revier.PanelTool, Command: []string{"./missing"}}
	if _, err := panelstart.CommandsOn([]revier.PanelSpec{shell, byPath}, path); err != nil {
		t.Fatalf("CommandsOn = %v, want nil", err)
	}

	missing := revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"no-such-agent", "--flag"}}
	_, err := panelstart.CommandsOn([]revier.PanelSpec{shell, missing}, path)
	if want := `the agent panel's command "no-such-agent" is not on PATH`; err == nil || err.Error() != want {
		t.Fatalf("CommandsOn = %v, want %q", err, want)
	}
}

// The PATH searched is the one given, not this process's: hc starts a
// panel with its core's. Only an executable file there is a program.
func TestCommandsOnSearchesTheGivenPATH(t *testing.T) {
	testenv.NoTools(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := revier.PanelSpec{Kind: revier.PanelAgent, Command: []string{"agent"}}
	if _, err := panelstart.CommandsOn([]revier.PanelSpec{agent}, dir); err != nil {
		t.Errorf("CommandsOn(%s) = %v, want nil", dir, err)
	}
	_, err := panelstart.CommandsOn([]revier.PanelSpec{agent}, "")
	if want := `the agent panel's command "agent" is not on PATH`; err == nil || err.Error() != want {
		t.Errorf("CommandsOn on an empty PATH = %v, want %q", err, want)
	}
	plain := revier.PanelSpec{Kind: revier.PanelTool, Command: []string{"plain"}}
	_, err = panelstart.CommandsOn([]revier.PanelSpec{plain}, dir)
	if want := `the tool panel's command "plain" is not on PATH`; err == nil || err.Error() != want {
		t.Errorf("CommandsOn with a file that is not executable = %v, want %q", err, want)
	}
}

// A relative or empty PATH entry is passed over, also when this directory
// holds the program: hc reads it against the panel's directory. The search
// goes on past it.
func TestCommandsOnPassesOverARelativePATHEntry(t *testing.T) {
	here, abs, empty := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{here, filepath.Join(here, "sub"), abs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(here)
	agent := []revier.PanelSpec{{Kind: revier.PanelAgent, Command: []string{"agent"}}}

	for _, path := range []string{".", ":", empty + ":.", empty + "::", "sub"} {
		_, err := panelstart.CommandsOn(agent, path)
		if want := `the agent panel's command "agent" is not on PATH`; err == nil || err.Error() != want {
			t.Errorf("CommandsOn on PATH %q = %v, want %q", path, err, want)
		}
	}
	got, err := panelstart.CommandsOn(agent, ".:"+abs)
	if want := [][]string{{filepath.Join(abs, "agent")}}; err != nil || !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("CommandsOn on PATH .:%s = %q, %v; want %q", abs, got, err, want)
	}
}

// A bare program is resolved to its file on the given PATH, its arguments
// kept; a program named by a path is kept as written, and a panel with no
// command has none. A program not on PATH is refused before any panel
// starts.
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
