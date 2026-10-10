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
	out.Reset()
	if err := run(&out, []string{"assist", "--print-brief", "-p", "any"}); err != nil {
		t.Errorf("assist on a broken configuration: %v", err)
	}
	// What is wrong is the first thing the agent is told: the user may say
	// no more than "fix it".
	if !strings.Contains(out.String(), "The configuration does not load") {
		t.Errorf("brief does not say that the configuration does not load:\n%s", out.String())
	}
}

// The agent is told the project the user was on and why it does not load
// whole, so "this project" and "fix it" mean something to it.
func TestTheBriefNamesTheProjectAndItsProblems(t *testing.T) {
	cfg := configRoot(t, "", map[string]string{"demo.toml": "path = \"/tmp/demo\"\n", "sound.toml": sound})
	t.Setenv("REVIER_STATE_HOME", t.TempDir())

	var out strings.Builder
	if err := run(&out, []string{"assist", "--print-brief", "-p", "demo"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`On the project "demo"`, filepath.Join(cfg, "projects", "demo.toml"), "does not load whole", "no target is marked home"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("brief does not name %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := run(&out, []string{"assist", "--print-brief", "-p", "sound"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "does not load") {
		t.Errorf("brief reports a problem for a project that loads whole:\n%s", out.String())
	}
	if err := run(&out, []string{"assist", "--print-brief", "-p", "nosuch"}); err == nil {
		t.Error("assist took a project nothing names")
	}
}

// The format comes from the binary, so it is the one this version reads: the
// configuration part of the document, without the parts about writing code.
func TestTheReferenceIsTheConfigurationFormat(t *testing.T) {
	var out strings.Builder
	if err := run(&out, []string{"assist", "--reference"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[[target]]", "[target.runtime]", "config.toml"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("reference does not hold %q", want)
		}
	}
	if strings.Contains(out.String(), "## Level 2") || strings.HasPrefix(out.String(), "# Extending") {
		t.Errorf("reference holds more than the configuration level:\n%.200s", out.String())
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
