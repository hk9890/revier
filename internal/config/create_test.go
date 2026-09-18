package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A directory is refused for the local project that has it, and for a name
// a project already holds; a link's path is on its host, so a directory here
// of the same path is free. The home directory is refused whatever the
// projects are.
func TestCanCreateRefusesOnlyWhatALocalProjectHolds(t *testing.T) {
	dir := t.TempDir()
	local := core.PrepareProject(revier.Project{Name: "app", Path: dir})
	local.File = "/cfg/projects/app.toml"
	link := core.PrepareProject(revier.Project{Name: "far", Path: dir, Remote: &revier.Link{Host: "buildbox"}})
	projects := []core.Project{local, link}

	if err := config.CanCreate(projects, "other", dir); err == nil || !strings.Contains(err.Error(), `already project "app"`) {
		t.Errorf("a local project's directory: err = %v, want it refused", err)
	}
	if err := config.CanCreate(projects, "app", filepath.Join(dir, "sub")); err == nil || !strings.Contains(err.Error(), `project "app" already exists`) {
		t.Errorf("a taken name: err = %v, want it refused", err)
	}
	if err := config.CanCreate([]core.Project{link}, "other", dir); err != nil {
		t.Errorf("a link's path: err = %v, want the directory free", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if err := config.CanCreate(nil, "home", filepath.Clean(home)); err == nil || !strings.Contains(err.Error(), "home directory") {
		t.Errorf("the home directory: err = %v, want it refused", err)
	}
}
