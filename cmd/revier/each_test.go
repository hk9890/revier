package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/runlog"
)

const eachProjectTOML = `path = %q
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["true"]
  match = { title = "^home$" }
`

// eachScratch points revier at a scratch config holding one project per entry,
// name to directory, and at a scratch state root, which it returns. A
// directory is created only when create names it, so the others are missing.
func eachScratch(t *testing.T, dirs map[string]string, create ...string) string {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range dirs {
		body := fmt.Sprintf(eachProjectTOML, dir)
		if err := os.WriteFile(filepath.Join(projects, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range create {
		if err := os.MkdirAll(dirs[name], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, "state")
	t.Setenv("REVIER_CONFIG_HOME", root)
	t.Setenv("REVIER_STATE_HOME", state)
	return state
}

// A dry run with no filter starts no process at all, which is why it can sit
// in this layer: it reads the project list and stats directories.
func TestEachDryRunPrintsTheSelectionAndRunsNothing(t *testing.T) {
	work := t.TempDir()
	state := eachScratch(t, map[string]string{
		"alpha": filepath.Join(work, "alpha"),
		"gone":  filepath.Join(work, "gone"),
	}, "alpha")

	var out strings.Builder
	marker := filepath.Join(work, "ran")
	if err := cmdEach(&out, []string{"--dry-run", "--", "touch", marker}); err != nil {
		t.Fatalf("each --dry-run: %v\n%s", err, out.String())
	}
	for _, want := range []string{"alpha", "would run", "gone", "missing", "1 would run, 1 skipped", "missing:  gone"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run does not print %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the dry run ran the command")
	}
	// Nothing ran, so there is nothing for `each log` to read back.
	if runs, _ := runlog.List(state); len(runs) != 0 {
		t.Errorf("the dry run left a run record: %+v", runs)
	}
}

// The command goes after `--`. Without the rule, `revier each log` and a
// command named log would be the same words.
func TestEachWantsTheCommandAfterTheSeparator(t *testing.T) {
	for _, args := range [][]string{
		{"git", "pull"},
		{"--dry-run", "--"},
		{"--dry-run", "git", "--", "pull"},
	} {
		if err := cmdEach(&strings.Builder{}, args); err == nil {
			t.Errorf("each %v: want a usage error", args)
		}
	}
}

func TestEachLogWithNoRunsSaysSo(t *testing.T) {
	eachScratch(t, nil)
	var out strings.Builder
	if err := cmdEach(&out, []string{"log"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no runs yet") {
		t.Errorf("each log = %q", out.String())
	}
}

// A failure is only useful with the file that says why beside it.
func TestAFailedProjectNamesItsOutput(t *testing.T) {
	word, detail := resultLine(runlog.Result{Project: "beta", Status: runlog.StatusFailed, Exit: 2, Output: "beta.log"}, "/runs/x")
	if word != "failed" || !strings.Contains(detail, "exit 2") || !strings.Contains(detail, "/runs/x/beta.log") {
		t.Errorf("failed line = %q %q", word, detail)
	}
}

func TestTheSummaryNamesWhatNeedsTheUser(t *testing.T) {
	var out strings.Builder
	printRunSummary(&out, []runlog.Result{
		{Project: "alpha", Status: runlog.StatusOK},
		{Project: "beta", Status: runlog.StatusFailed, Exit: 1},
		{Project: "gone", Status: runlog.StatusSkipped, Skip: core.SkipMissing},
		{Project: "plain", Status: runlog.StatusSkipped, Skip: core.SkipFiltered},
	}, "/runs/x")
	for _, want := range []string{"1 ok, 1 failed, 2 skipped", "failed:   beta", "missing:  gone", "output:   /runs/x"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary does not carry %q:\n%s", want, out.String())
		}
	}
	// A filtered project is what the user asked for, not something to fix.
	if strings.Contains(out.String(), "plain") {
		t.Errorf("summary names a filtered project:\n%s", out.String())
	}
}
