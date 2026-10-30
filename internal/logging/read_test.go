package logging_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hk9890/revier/internal/logging"
)

// writeLog writes one day file of the log under root.
func writeLog(t *testing.T, root, day, text string) {
	t.Helper()
	dir := logging.Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "revier-"+day+".log"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func msgs(lines []logging.Line) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Msg)
	}
	return out
}

var readNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local)

// The screen lists what went wrong, newest first across the days, and the
// INFO lines of every operation that went right are not on it.
func TestReadIsTheWarningsAndErrorsNewestFirst(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "2026-10-29",
		`{"time":"2026-10-29T09:00:00+01:00","level":"ERROR","msg":"first","pid":7,"cmd":"tui","err":"boom"}`+"\n"+
			`{"time":"2026-10-29T10:00:00+01:00","level":"WARN","msg":"second","pid":7,"cmd":"tui","project":"alpha"}`+"\n")
	writeLog(t, root, "2026-10-30",
		`{"time":"2026-10-30T09:00:00+01:00","level":"INFO","msg":"went fine","pid":8,"cmd":"go"}`+"\n"+
			`{"time":"2026-10-30T10:00:00+01:00","level":"ERROR","msg":"third","pid":8,"cmd":"go","err":"no window"}`+"\n")
	lines, skipped, err := logging.Read(root, readNow)
	if err != nil || skipped != 0 {
		t.Fatalf("Read = %v, skipped %d", err, skipped)
	}
	if got, want := msgs(lines), []string{"third", "second", "first"}; !slices.Equal(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	l := lines[0]
	if l.Level != "ERROR" || l.Pid != 8 || l.Cmd != "go" || l.Err != "no window" || !l.Time.Equal(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("line = %+v", l)
	}
	if lines[1].Project != "alpha" {
		t.Errorf("project = %q, want alpha", lines[1].Project)
	}
}

// A line that does not decode - one that is broken, or the last one another
// process is still appending - is counted, and the rest are read.
func TestReadCountsTheLinesItCannotDecode(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "2026-10-30",
		`{"time":"2026-10-30T09:00:00Z","level":"ERROR","msg":"kept","pid":1}`+"\n"+
			"not json\n"+
			`{"time":"2026-10-30T10:00:00Z","level":"ERR`)
	lines, skipped, err := logging.Read(root, readNow)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 || !slices.Equal(msgs(lines), []string{"kept"}) {
		t.Errorf("lines = %v, skipped %d; want [kept], 2", msgs(lines), skipped)
	}
}

// Only the days the log keeps are read, and a file that is not a day file of
// the log is not revier's.
func TestReadKeepsToTheDayFiles(t *testing.T) {
	root := t.TempDir()
	line := func(msg string) string {
		return `{"time":"2026-10-01T09:00:00Z","level":"ERROR","msg":"` + msg + `"}` + "\n"
	}
	writeLog(t, root, "2026-10-17", line("kept"))
	writeLog(t, root, "2026-10-16", line("too old"))
	if err := os.WriteFile(filepath.Join(logging.Dir(root), "notes.log"), []byte(line("not ours")), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, _, err := logging.Read(root, readNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := msgs(lines); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("lines = %v, want [kept]", got)
	}
}

func TestReadWithNoLogIsNothing(t *testing.T) {
	lines, skipped, err := logging.Read(t.TempDir(), readNow)
	if lines != nil || skipped != 0 || err != nil {
		t.Errorf("Read = %v, %d, %v; want nothing", lines, skipped, err)
	}
}

// Every field comes back in the order it was written, an object as its JSON.
func TestFieldsKeepTheFileOrder(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "2026-10-30",
		`{"time":"2026-10-30T09:00:00Z","level":"ERROR","msg":"bind","pid":3,"ref":{"host":"tmux","id":"%1"},"duration_ms":12,"err":"a\nb"}`+"\n")
	lines, _, err := logging.Read(root, readNow)
	if err != nil || len(lines) != 1 {
		t.Fatalf("Read = %v, %v", lines, err)
	}
	want := []logging.Field{
		{Key: "time", Value: "2026-10-30T09:00:00Z"},
		{Key: "level", Value: "ERROR"},
		{Key: "msg", Value: "bind"},
		{Key: "pid", Value: "3"},
		{Key: "ref", Value: `{"host":"tmux","id":"%1"}`},
		{Key: "duration_ms", Value: "12"},
		{Key: "err", Value: "a\nb"},
	}
	if got := lines[0].Fields(); !slices.Equal(got, want) {
		t.Errorf("Fields = %v, want %v", got, want)
	}
}
