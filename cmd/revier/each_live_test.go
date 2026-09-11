//go:build live

// `revier each` against real processes: the command, the filter, and the files
// they leave. Every test runs on a scratch config and state root, and every
// command it starts is sh in a temporary directory.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/runlog"
)

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func lastRun(t *testing.T, state string) runlog.Run {
	t.Helper()
	runs, err := runlog.List(state)
	if err != nil || len(runs) == 0 {
		t.Fatalf("no run recorded: %v", err)
	}
	return runs[0]
}

// One project fails, and the one after it still runs. The failure is the
// command's exit status, the output is kept per project, and the missing
// directory is skipped and named.
func TestEachRunsInEveryDirectoryAndKeepsTheOutput(t *testing.T) {
	work := t.TempDir()
	state := eachScratch(t, map[string]string{
		"broken": filepath.Join(work, "broken"),
		"clean":  filepath.Join(work, "clean"),
		"gone":   filepath.Join(work, "gone"),
	}, "broken", "clean")
	if err := os.WriteFile(filepath.Join(work, "broken", "fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	err := cmdEach(&out, []string{"--", "sh", "-c", "pwd; if test -e fail; then echo no >&2; exit 7; fi"})
	if !errors.Is(err, errEachFailed) {
		t.Fatalf("err = %v, want errEachFailed: one project failed\n%s", err, out.String())
	}
	for _, want := range []string{"failed:   broken", "missing:  gone", "1 ok, 1 failed, 1 skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary does not carry %q:\n%s", want, out.String())
		}
	}

	r := lastRun(t, state)
	byName := map[string]runlog.Result{}
	for _, res := range r.Results {
		byName[string(res.Project)] = res
	}
	if res := byName["broken"]; res.Status != runlog.StatusFailed || res.Exit != 7 {
		t.Errorf("broken = %+v, want failed with the command's exit 7", res)
	}
	if res := byName["clean"]; res.Status != runlog.StatusOK {
		t.Errorf("clean = %+v, want ok: a failure must not stop the projects after it", res)
	}
	if r.Finished.IsZero() {
		t.Error("the run was not recorded as finished")
	}

	// pwd proves the directory; stderr lands in the same file as stdout.
	for name, want := range map[string]string{"clean": filepath.Join(work, "clean"), "broken": "no"} {
		b, err := os.ReadFile(filepath.Join(r.Dir, byName[name].Output))
		if err != nil || !strings.Contains(string(b), want) {
			t.Errorf("%s output = %q, %v; want it to contain %q", name, b, err, want)
		}
	}
}

func TestEachFilterNarrowsTheSelection(t *testing.T) {
	work := t.TempDir()
	eachScratch(t, map[string]string{
		"repo":  filepath.Join(work, "repo"),
		"plain": filepath.Join(work, "plain"),
	}, "repo", "plain")
	mkdirs(t, filepath.Join(work, "repo", ".git"))

	var out strings.Builder
	if err := cmdEach(&out, []string{"--filter", "test -d .git", "--", "touch", "ran"}); err != nil {
		t.Fatalf("each: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(work, "repo", "ran")); err != nil {
		t.Error("the command did not run where the filter passed")
	}
	if _, err := os.Stat(filepath.Join(work, "plain", "ran")); err == nil {
		t.Error("the command ran where the filter failed")
	}
	if !strings.Contains(out.String(), "filtered") {
		t.Errorf("the filtered project is not reported:\n%s", out.String())
	}
}

// The dry run asks the filter, because the selection is what it shows, and
// still runs nothing.
func TestEachDryRunAppliesTheFilter(t *testing.T) {
	work := t.TempDir()
	eachScratch(t, map[string]string{
		"repo":  filepath.Join(work, "repo"),
		"plain": filepath.Join(work, "plain"),
	}, "repo", "plain")
	mkdirs(t, filepath.Join(work, "repo", ".git"))

	var out strings.Builder
	if err := cmdEach(&out, []string{"-n", "--filter", "test -d .git", "--", "touch", "ran"}); err != nil {
		t.Fatalf("each -n: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "1 would run, 1 skipped") {
		t.Errorf("dry run selection:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(work, "repo", "ran")); err == nil {
		t.Error("the dry run ran the command")
	}
}

// Two projects on one directory - here one path is a symlink to the other -
// run the command once.
func TestEachRunsASharedDirectoryOnce(t *testing.T) {
	work := t.TempDir()
	repo := filepath.Join(work, "repo")
	mkdirs(t, repo)
	if err := os.Symlink(repo, filepath.Join(work, "link")); err != nil {
		t.Fatal(err)
	}
	eachScratch(t, map[string]string{"a-repo": repo, "b-link": filepath.Join(work, "link")})

	var out strings.Builder
	if err := cmdEach(&out, []string{"--", "sh", "-c", "echo x >> count"}); err != nil {
		t.Fatalf("each: %v\n%s", err, out.String())
	}
	b, _ := os.ReadFile(filepath.Join(repo, "count"))
	if string(b) != "x\n" {
		t.Errorf("count = %q, want one line: the directory ran twice\n%s", b, out.String())
	}
	if !strings.Contains(out.String(), "same directory as a-repo") {
		t.Errorf("the duplicate is not reported:\n%s", out.String())
	}
}

func TestEachLogReadsAPastRunBack(t *testing.T) {
	work := t.TempDir()
	state := eachScratch(t, map[string]string{
		"alpha": filepath.Join(work, "alpha"),
		"gone":  filepath.Join(work, "gone"),
	}, "alpha")
	if err := cmdEach(&strings.Builder{}, []string{"--", "echo", "hello from alpha"}); err != nil {
		t.Fatal(err)
	}
	id := lastRun(t, state).ID

	var list strings.Builder
	if err := cmdEach(&list, []string{"log"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.String(), id) || !strings.Contains(list.String(), "echo hello from alpha") {
		t.Errorf("each log does not list the run:\n%s", list.String())
	}

	var one strings.Builder
	if err := cmdEach(&one, []string{"log", id}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alpha", "ok", "missing:  gone"} {
		if !strings.Contains(one.String(), want) {
			t.Errorf("each log %s does not carry %q:\n%s", id, want, one.String())
		}
	}

	var output strings.Builder
	if err := cmdEach(&output, []string{"log", id, "alpha"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hello from alpha\n" {
		t.Errorf("each log %s alpha = %q, want what the command printed", id, output.String())
	}
	if err := cmdEach(&strings.Builder{}, []string{"log", id, "gone"}); err == nil {
		t.Error("a skipped project has no output; asking for it must say so")
	}
}
