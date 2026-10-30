package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/logging"
	"github.com/hk9890/revier/internal/theme"
)

// The logs screen runs with no root model: it opens on the line of the
// footer's error, shows a line's fields as text, and Esc steps back from the
// line to the lines and from the lines out.
func TestTheLogsScreenReadsALineWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local)
	me := strconv.Itoa(os.Getpid())
	line := func(at time.Time, msg, pid, extra string) string {
		return `{"time":"` + at.Format(time.RFC3339Nano) + `","level":"ERROR","msg":"` + msg + `","pid":` + pid + extra + "}\n"
	}
	dir := logging.Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := line(now.Add(-time.Minute), "go", me, `,"err":"kitty said \u001b[31mred\u001b[0m"`) +
		line(now.Add(time.Minute), "later", me, "")
	if err := os.WriteFile(filepath.Join(dir, "revier-2026-10-30.log"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), stateRoot: root, list: 60, page: 10}
	sc := newLogsScreen(th)
	if err := sc.open(sf, now, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if it := sc.list.SelectedItem().(logItem); it.line.Msg != "later" {
		t.Errorf("with no error, cursor on %q, want the newest line", it.line.Msg)
	}
	if err := sc.open(sf, now, now); err != nil {
		t.Fatal(err)
	}
	if it := sc.list.SelectedItem().(logItem); it.line.Msg != "go" {
		t.Fatalf("cursor on %q, want the newest line of this process at the error's time", it.line.Msg)
	}

	if res, _ := sc.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyEnter})); res.closed || !sc.reading {
		t.Fatal("enter did not show the line")
	}
	full := sc.full(sf)
	if !strings.Contains(full, "kitty said red") || strings.Contains(full, "\x1b[31m") {
		t.Errorf("the line in full = %q, want its err as text", full)
	}
	esc := pressedKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if res, _ := sc.key(sf, esc); res.closed || sc.reading {
		t.Errorf("esc on the line: closed %v, reading %v; want the lines", res.closed, sc.reading)
	}
	if res, _ := sc.key(sf, esc); !res.closed {
		t.Error("esc on the lines did not close the screen")
	}
}
