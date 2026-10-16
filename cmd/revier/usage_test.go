package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// `revier help` names every command the dispatch accepts, and a family's own
// usage names each of its subcommands. A command added without its line fails
// here, so the list cannot fall behind again.
func TestUsageNamesEveryCommandTheDispatchAccepts(t *testing.T) {
	for _, cmd := range dispatched(t) {
		if !strings.Contains(usage, "  revier "+cmd) {
			t.Errorf("revier help does not name %q", cmd)
		}
		family, _, isSub := strings.Cut(cmd, " ")
		if isSub && !strings.Contains(familyUsage[family], "revier "+cmd) {
			t.Errorf("revier %s help does not name %q", family, cmd)
		}
	}
}

// Every command answers --help and -h with its usage on stdout and ends with
// status 0. Nothing reaches the flag parser, whose own answer is an error.
func TestEveryCommandAnswersHelpWithItsUsage(t *testing.T) {
	for _, cmd := range dispatched(t) {
		for _, flag := range []string{"--help", "-h"} {
			args := append(strings.Fields(cmd), flag)
			var out bytes.Buffer
			if err := run(&out, args); err != nil {
				t.Errorf("revier %s: err = %v, want the usage", strings.Join(args, " "), err)
				continue
			}
			word, _, _ := strings.Cut(cmd, " ")
			if !strings.Contains(out.String(), "usage:") || !strings.Contains(out.String(), "revier "+word) {
				t.Errorf("revier %s printed no usage:\n%s", strings.Join(args, " "), out.String())
			}
		}
	}
}

// `revier link help` is a request for the usage, not a host called help.
func TestLinkHelpPrintsTheUsage(t *testing.T) {
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	var out bytes.Buffer
	if err := run(&out, []string{"link", "help"}); err != nil {
		t.Fatalf("err = %v, want the usage", err)
	}
	if !strings.Contains(out.String(), "revier link [host [project]]") {
		t.Errorf("printed %q, want the link usage", out.String())
	}
}

// A help flag after `--` is text for the command, not a request for help.
func TestAHelpFlagAfterTheSeparatorIsAnArgument(t *testing.T) {
	if asksHelp("agent", []string{"prompt", "demo", "--", "-h"}) {
		t.Error("asksHelp read the prompt text -h as a help request")
	}
	fs := flag.NewFlagSet("agent send-keys", flag.ContinueOnError)
	pos, err := parseArgs(fs, []string{"demo", "--", "a", "-h"})
	if err != nil || !slices.Equal(pos, []string{"demo", "a", "-h"}) {
		t.Errorf("parseArgs = %q, %v; want every word after -- as positional", pos, err)
	}
}

// The flag parser prints nothing of its own: main prints the error once.
func TestAnUnknownFlagIsNotPrintedByTheParser(t *testing.T) {
	stderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	err = run(io.Discard, []string{"each", "--bogus", "--", "true"})
	os.Stderr = stderr
	_ = w.Close()
	printed, _ := io.ReadAll(r)

	if err == nil || !strings.Contains(err.Error(), "-bogus") {
		t.Errorf("err = %v, want the unknown flag", err)
	}
	if len(printed) > 0 {
		t.Errorf("the parser printed %q, want nothing before main prints the error", printed)
	}
}

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
