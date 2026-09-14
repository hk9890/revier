package logging

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestPruneKeepsTheLastFourteenDaysAndForeignFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local)
	for _, name := range []string{
		"revier-2026-09-14.log", // today
		"revier-2026-09-01.log", // the fourteenth day back
		"revier-2026-08-31.log", // the fifteenth
		"revier-2025-01-01.log",
		"revier-notaday.log",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := prune(dir, now); err != nil {
		t.Fatal(err)
	}

	want := []string{"notes.txt", "revier-2026-09-01.log", "revier-2026-09-14.log", "revier-notaday.log"}
	if got := names(t, dir); !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}
}

func TestAWriteAfterMidnightGoesToTheNextDaysFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 14, 23, 59, 0, 0, time.Local)
	d := &daily{dir: dir, now: func() time.Time { return now }}
	if _, err := d.open(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := d.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	d.pruning.Wait()

	for name, want := range map[string]string{
		"revier-2026-09-14.log": "before\n",
		"revier-2026-09-15.log": "after\n",
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
}

func TestOpenReportsCreatedOnlyForANewDaysFile(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time { return time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local) }
	for i, want := range []bool{true, false} {
		created, err := (&daily{dir: dir, now: now}).open()
		if err != nil {
			t.Fatal(err)
		}
		if created != want {
			t.Errorf("open %d: created = %v, want %v", i, created, want)
		}
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
