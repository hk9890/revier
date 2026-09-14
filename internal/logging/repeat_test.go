package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// capture is a repeats that logs into the buffer, and leaves the default
// logger alone.
func capture(now func() time.Time) (*repeats, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, nil))
	r := &repeats{log: func() *slog.Logger { return l }, now: now, failing: map[string]string{}, slow: map[string]time.Time{}}
	return r, &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for l := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatal(err)
		}
		out = append(out, rec)
	}
	return out
}

func TestARepeatedFailureIsLoggedOnceAndItsRecoveryOnce(t *testing.T) {
	r, buf := capture(time.Now)
	down, other := errors.New("host down"), errors.New("auth refused")
	for _, err := range []error{down, down, down, other, nil, nil} {
		r.repeat("remote a", "remote survey", err, nil)
	}
	r.repeat("remote b", "remote survey", down, nil)

	got := lines(t, buf)
	want := []struct{ level, msg, err string }{
		{"WARN", "remote survey", "host down"},
		{"WARN", "remote survey", "auth refused"},
		{"INFO", "remote survey: recovered", ""},
		{"WARN", "remote survey", "host down"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d:\n%s", len(got), len(want), buf)
	}
	for i, w := range want {
		if got[i]["level"] != w.level || got[i]["msg"] != w.msg || (w.err != "" && got[i]["err"] != w.err) {
			t.Errorf("line %d = %v, want %+v", i, got[i], w)
		}
	}
}

func TestASlowPollIsLoggedAtMostOncePerInterval(t *testing.T) {
	now := time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local)
	r, buf := capture(func() time.Time { return now })
	for range 30 {
		r.poll("survey", "survey", 2*SlowPoll, nil, nil)
		r.poll("survey", "survey", SlowPoll/2, nil, nil)
		now = now.Add(time.Second)
	}
	now = now.Add(SlowEvery)
	r.poll("survey", "survey", 2*SlowPoll, nil, nil)

	if n := len(lines(t, buf)); n != 2 {
		t.Errorf("%d lines, want 2:\n%s", n, buf)
	}
}
