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

// capture points the default logger at a buffer for one test.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	was := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(was) })
	return &buf
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
	buf := capture(t)
	r := &repeats{now: time.Now, failing: map[string]string{}, slow: map[string]time.Time{}}
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
	buf := capture(t)
	now := time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local)
	r := &repeats{now: func() time.Time { return now }, failing: map[string]string{}, slow: map[string]time.Time{}}
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
