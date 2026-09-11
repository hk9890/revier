package runlog_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/runlog"
)

var t0 = time.Date(2026, 9, 11, 14, 3, 22, 0, time.Local)

func TestARunIsKeptUnderTheStateRoot(t *testing.T) {
	state := t.TempDir()
	r, err := runlog.Start(state, []string{"git", "pull"}, "test -d .git", t0)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.HasPrefix(r.Dir, filepath.Join(state, "runs")+string(filepath.Separator)) {
		t.Errorf("run dir = %s, want it under the state root's runs/", r.Dir)
	}
	if !strings.HasPrefix(r.ID, "2026-09-11T14-03-22-") {
		t.Errorf("id = %s, want it to start with the start time", r.ID)
	}
}

func TestAProjectsOutputCanBeReadAfterTheRun(t *testing.T) {
	state := t.TempDir()
	r, err := runlog.Start(state, []string{"echo", "hi"}, "", t0)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	f, name, err := r.Output("group/alpha")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	_, _ = f.WriteString("hi\n")
	_ = f.Close()
	r.Results = append(r.Results,
		runlog.Result{Project: "group/alpha", Path: "/a", Status: runlog.StatusOK, Output: name},
		runlog.Result{Project: "gone", Path: "/gone", Status: runlog.StatusSkipped, Skip: core.SkipMissing},
	)
	r.Finished = t0.Add(time.Second)
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := runlog.Load(state, r.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Results) != 2 || got.Results[1].Skip != core.SkipMissing || got.Finished.IsZero() {
		t.Fatalf("loaded %+v", got)
	}
	// A project name with a slash would otherwise put its output in a
	// directory that does not exist.
	b, err := os.ReadFile(filepath.Join(got.Dir, got.Results[0].Output))
	if err != nil || string(b) != "hi\n" {
		t.Fatalf("output = %q, %v", b, err)
	}
}

// A run is saved before its first project, so one that is interrupted is still
// listed, and says it did not finish.
func TestAnInterruptedRunIsListedUnfinished(t *testing.T) {
	state := t.TempDir()
	if _, err := runlog.Start(state, []string{"sleep", "600"}, "", t0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	runs, err := runlog.List(state)
	if err != nil || len(runs) != 1 {
		t.Fatalf("List = %v, %v", runs, err)
	}
	if !runs[0].Finished.IsZero() || runs[0].Argv[0] != "sleep" {
		t.Errorf("run = %+v, want the argv and no finish time", runs[0])
	}
}

func TestListIsNewestFirst(t *testing.T) {
	state := t.TempDir()
	for _, at := range []time.Time{t0, t0.Add(time.Hour), t0.Add(-time.Hour)} {
		if _, err := runlog.Start(state, []string{"true"}, "", at); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	runs, err := runlog.List(state)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"2026-09-11T15", "2026-09-11T14", "2026-09-11T13"} {
		if !strings.HasPrefix(runs[i].ID, want) {
			t.Errorf("runs[%d] = %s, want %s...", i, runs[i].ID, want)
		}
	}
}

func TestNoRunsYetIsAnEmptyList(t *testing.T) {
	runs, err := runlog.List(t.TempDir())
	if err != nil || len(runs) != 0 {
		t.Fatalf("List = %v, %v", runs, err)
	}
}

// An id is a directory name. A path would read a summary from outside runs/.
func TestLoadRefusesAPath(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "summary.json"), []byte(`{"id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runlog.Load(state, ".."); !errors.Is(err, runlog.ErrNoRun) {
		t.Fatalf("Load(..) = %v, want ErrNoRun", err)
	}
	if _, err := runlog.Load(state, "absent"); !errors.Is(err, runlog.ErrNoRun) {
		t.Fatalf("Load(absent) = %v, want ErrNoRun", err)
	}
}
