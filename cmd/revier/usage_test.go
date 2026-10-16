package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

// A doctor that found problems exits 1 and says nothing more, and its
// command line in the log is its outcome: at Info, with how many files.
func TestADoctorThatFoundProblemsIsLoggedAsItsOutcome(t *testing.T) {
	found := problemsFound{files: 2}
	if status, say := outcome(found); status != 1 || say {
		t.Errorf("outcome = %d, %v; want 1, silent", status, say)
	}

	var b bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logCommand(time.Now(), []string{"doctor"}, found, 1)

	var line map[string]any
	if err := json.Unmarshal(b.Bytes(), &line); err != nil {
		t.Fatalf("%v: %s", err, b.String())
	}
	if line["level"] != "INFO" || line["problems"] != float64(2) || line["exit"] != float64(1) {
		t.Errorf("logged %s, want INFO with problems 2 and exit 1", b.String())
	}
	if _, ok := line["err"]; ok {
		t.Errorf("logged %s, want no err", b.String())
	}
}
