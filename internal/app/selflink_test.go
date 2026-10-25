package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/app"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

// A link to this machine would be left out by every load, so none is
// written.
func TestLinkProjectRefusesThisMachine(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	root := t.TempDir()
	_, err = app.LinkProject(root, &ledger{}, "", machine, revier.Project{Name: "far", Path: "/srv/far"})
	if err == nil || !strings.Contains(err.Error(), "is this machine") {
		t.Errorf("err = %v, want this machine refused", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "far")); !os.IsNotExist(err) {
		t.Errorf("project file: %v, want none written", err)
	}
}
