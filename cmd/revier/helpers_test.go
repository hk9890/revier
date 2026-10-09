package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// output runs f with the app's output captured, and returns what it printed.
func output(t *testing.T, a *app, f func() error) string {
	t.Helper()
	var b bytes.Buffer
	a.out = &b
	if err := f(); err != nil {
		t.Fatalf("%v\n%s", err, b.String())
	}
	return b.String()
}

// demoProject is a workspace and a diff pane on the runtime, and an editor
// window.
func demoProject(t *testing.T) core.Project {
	t.Helper()
	p := core.PrepareProject(revier.Project{Name: "demo", Path: t.TempDir(), Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "home", Launch: []string{"x"}, Match: revier.Match{Title: "^home$"}}},
		{Name: "diff", Key: "ctrl-shift-d", Runtime: &revier.Realization{
			Name: "diff", Launch: []string{"x"}, Match: revier.Match{Title: "^diff$"}}},
		{Name: "editor", Key: "ctrl-shift-o", Window: &revier.Realization{
			Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
	}})
	return p
}

// remoteProject is a project on buildbox, its home target here the ssh pane
// onto the workspace there.
func remoteProject(t *testing.T) core.Project {
	t.Helper()
	p := core.PrepareProject(revier.Project{Name: "far", Path: "~/dev/far", Remote: &revier.Link{Host: "buildbox", Project: "far"}, Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Name: "far", Match: revier.Match{Title: "^far$"}, Panels: []revier.PanelSpec{{Kind: revier.PanelAgent}, {Kind: revier.PanelShell}}}},
	}})
	return p
}

// configRoot writes a configuration root and points revier at it: cfg as
// config.toml, when it is not empty, and each of files in the projects
// directory, keyed by file name.
func configRoot(t *testing.T, cfg string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(file, body string) {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if cfg != "" {
		write(filepath.Join(root, "config.toml"), cfg)
	}
	for name, body := range files {
		write(filepath.Join(dir, name), body)
	}
	t.Setenv("REVIER_CONFIG_HOME", root)
	return root
}
