package record_test

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/record"
)

func TestAWrittenLineIsReadBackStampedWithItsProcess(t *testing.T) {
	clock := at("2026-10-16", "09:00:00")
	root := scratch(t, &clock)

	record.Write(record.Line{Kind: record.KindGo, By: record.ByDid, Project: "revier", Target: "editor", Err: "gnome: wctl list: timeout"})

	got := read(t, root, clock)
	if len(got) != 1 {
		t.Fatalf("lines = %+v, want one", got)
	}
	l := got[0]
	if !l.Time.Equal(clock) || l.Pid != os.Getpid() || l.Cmd != "tui" || l.Err != "gnome: wctl list: timeout" {
		t.Errorf("line = %+v, want stamped at %v by this process with its error", l, clock)
	}
}

func TestEachLineGoesToTheFileOfItsDay(t *testing.T) {
	clock := at("2026-10-16", "23:59:59")
	root := scratch(t, &clock)

	record.Write(record.Line{Kind: record.KindGo, By: record.ByDid})
	clock = at("2026-10-17", "00:00:01")
	record.Write(record.Line{Kind: record.KindGo, By: record.ByDid})

	for _, day := range []string{"2026-10-16", "2026-10-17"} {
		if _, err := os.Stat(filepath.Join(record.Dir(root), day+".jsonl")); err != nil {
			t.Errorf("day file %s: %v", day, err)
		}
	}
}

func TestWithoutSetupNothingIsRecorded(t *testing.T) {
	root := t.TempDir()

	record.Write(record.Line{Kind: record.KindGo, By: record.ByDid})
	record.Saw(desktop())

	if _, err := os.Stat(record.Dir(root)); !os.IsNotExist(err) {
		t.Errorf("record directory: %v, want none", err)
	}
}

func TestNoDirectoryIsNoLines(t *testing.T) {
	if got := read(t, t.TempDir(), time.Now()); len(got) != 0 {
		t.Errorf("lines = %+v, want none", got)
	}
}

func TestALineThatIsNotOneIsSkipped(t *testing.T) {
	clock := at("2026-10-16", "09:00:00")
	root := scratch(t, &clock)
	record.Write(record.Line{Kind: record.KindGo, By: record.ByDid})
	f, err := os.OpenFile(filepath.Join(record.Dir(root), "2026-10-16.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"time\": not json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	record.Write(record.Line{Kind: record.KindClose, By: record.ByDid})

	if got := kinds(read(t, root, clock)); !slices.Equal(got, []string{record.KindGo, record.KindClose}) {
		t.Errorf("kinds = %v, want the two good lines", got)
	}
}

func TestConcurrentWritesKeepEveryLineWhole(t *testing.T) {
	clock := at("2026-10-16", "09:00:00")
	root := scratch(t, &clock)
	snapshot := desktop()

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { record.Write(record.Line{Kind: record.KindSnapshot, By: record.BySaw, Seen: &snapshot}) })
	}
	wg.Wait()

	if got := read(t, root, clock); len(got) != 50 {
		t.Errorf("lines read = %d, want 50", len(got))
	}
}

func TestASurfaceRecordsWhatItsSurveySaw(t *testing.T) {
	clock := at("2026-10-16", "09:00:00")
	root := scratch(t, &clock)

	record.Saw(desktop())
	clock = clock.Add(10 * time.Second)
	closed := desktop()
	closed.Agents = nil
	record.Saw(closed)

	if got := kinds(read(t, root, clock)); !slices.Equal(got, []string{record.KindSnapshot, record.KindAgentClosed}) {
		t.Errorf("kinds = %v, want a snapshot, then the agent closed", got)
	}
}

// A replay reads back no further than the last snapshot of this machine at
// or before its moment, and never a later day.
func TestReplayStartsAtTheLastSnapshot(t *testing.T) {
	days := func(lines []record.Line) []string {
		var out []string
		for _, l := range lines {
			if d := l.Time.Format(time.DateOnly); !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
		return out
	}
	root := fixture("days")

	if got := days(read(t, root, at("2026-10-16", "12:00:00"))); !slices.Equal(got, []string{"2026-10-15", "2026-10-16"}) {
		t.Errorf("days read at the 16th = %v, want the 15th and the 16th", got)
	}
	if got := days(read(t, root, at("2026-10-15", "08:30:00"))); !slices.Equal(got, []string{"2026-10-14", "2026-10-15"}) {
		t.Errorf("days read before the 15th's snapshot = %v, want the 14th and the 15th", got)
	}
}
