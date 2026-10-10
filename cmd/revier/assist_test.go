package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/tui"
)

// The brief names the files of the configuration revier reads, so the agent
// edits those and not the ones a default path would give.
func TestTheBriefNamesTheConfigurationAndTheLogs(t *testing.T) {
	cfg := configRoot(t, "", nil)
	st := t.TempDir()
	t.Setenv("REVIER_STATE_HOME", st)

	var out strings.Builder
	if err := run(&out, []string{"assist", "--print-brief"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(cfg, "config.toml"), filepath.Join(cfg, "projects"), filepath.Join(st, "logs"), " doctor"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("brief does not name %q:\n%s", want, out.String())
		}
	}
}

// The agent starts in a directory of its own, where a root given relative to
// this one would name another place: the brief names every path in full.
func TestTheBriefNamesARelativeRootInFull(t *testing.T) {
	t.Setenv("REVIER_CONFIG_HOME", "cfg")
	t.Setenv("REVIER_STATE_HOME", "state")
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := run(&out, []string{"assist", "--print-brief"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(here, "cfg", "config.toml"), filepath.Join(here, "state", "logs")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("brief does not name %q:\n%s", want, out.String())
		}
	}
}

// assist is the command that repairs a configuration, so it starts on one no
// other command loads.
func TestAssistStartsOnAConfigurationThatDoesNotLoad(t *testing.T) {
	configRoot(t, "this is not = = toml", nil)
	t.Setenv("REVIER_STATE_HOME", t.TempDir())

	var out strings.Builder
	if err := run(&out, []string{"doctor"}); err == nil {
		t.Fatal("doctor loaded the broken configuration; the test proves nothing")
	}
	if err := run(&out, []string{"assist", "--print-brief"}); err != nil {
		t.Errorf("assist on a broken configuration: %v", err)
	}
}

// With no agent installed the command ends with the status the surface reads
// after the hand-over, and names what to install.
func TestAssistWithNoAgentInstalledSaysSo(t *testing.T) {
	configRoot(t, "", nil)
	t.Setenv("REVIER_STATE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	err := run(&strings.Builder{}, []string{"assist"})
	if !errors.Is(err, tui.ErrNoAssistant) {
		t.Fatalf("assist with no agent on PATH: %v, want %v", err, tui.ErrNoAssistant)
	}
	if status, say := outcome(err); status != tui.ExitNoAssistant || !say {
		t.Errorf("outcome = %d, %v; want %d and the message printed", status, say, tui.ExitNoAssistant)
	}
}
