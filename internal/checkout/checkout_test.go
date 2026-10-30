package checkout_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/pkg/revier"
)

// A link records the host's path and repository for its targets to render, so
// both name a directory on another machine and neither is cloned here
// (decisions.md D83). The refusal names the host that holds the checkout.
func TestEnsureRefusesToCloneALink(t *testing.T) {
	p := revier.Project{
		Name:   "far",
		Path:   "/srv/far",
		GitURL: "git@github.com:example/far.git",
		Remote: &revier.Link{Host: "buildbox", Project: "far"},
	}
	cloned, err := checkout.Ensure(p, io.Discard)
	if cloned {
		t.Error("cloned a link")
	}
	if err == nil || !strings.Contains(err.Error(), "buildbox") {
		t.Errorf("err = %v, want the clone refused, naming the host that holds the checkout", err)
	}
}

// A clone refused before git runs is a clone line of the log too: the TUI
// names a failed clone alone and leaves why to the log.
func TestEnsureLogsAFailureBeforeGitRuns(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	file := filepath.Join(t.TempDir(), "demo")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := checkout.Ensure(revier.Project{Name: "demo", Path: file, GitURL: "/nowhere.git"}, io.Discard)
	if err == nil {
		t.Fatal("Ensure over a file cloned nothing and reported no error")
	}
	if line := logged.String(); !strings.Contains(line, `"level":"ERROR","msg":"clone"`) || !strings.Contains(line, "not a directory") {
		t.Errorf("log = %q, want the clone's error line", line)
	}
}
