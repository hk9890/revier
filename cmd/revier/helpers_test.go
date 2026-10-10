package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/events"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/state"
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

// withState is the app on the state file under its state root, which holds
// st: what a command finds there when it starts.
func withState(t *testing.T, a *app, st *state.State) *app {
	t.Helper()
	if err := st.Save(a.stateRoot); err != nil {
		t.Fatal(err)
	}
	a.core.Ledger = ledger.File{Root: a.stateRoot}
	return a
}

// cwd is the working directory with symlinks resolved, so it compares with a
// temporary directory whatever the platform links /tmp to.
func cwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return resolved(t, dir)
}

func resolved(t *testing.T, dir string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// tuiApp is an app that knows one local project, at dir.
func tuiApp(dir string) *app {
	return &app{cfg: &config.Config{}, projects: []core.Project{
		core.PrepareProject(revier.Project{Name: "demo", Path: dir}),
	}}
}

// eachScratch points revier at a scratch config holding one project per entry,
// name to directory, and at a scratch state root, which it returns. A
// directory is created only when create names it, so the others are missing.
func eachScratch(t *testing.T, dirs map[string]string, create ...string) string {
	t.Helper()
	files := map[string]string{}
	for name, dir := range dirs {
		files[name+".toml"] = fmt.Sprintf(eachProjectTOML, dir)
	}
	root := configRoot(t, "", files)
	for _, name := range create {
		if err := os.MkdirAll(dirs[name], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, "state")
	t.Setenv("REVIER_STATE_HOME", state)
	return state
}

// recordingTo makes root the state root of this process, as openLog does for a
// real one, and stops the recording when the test ends.
func recordingTo(t *testing.T, root string) {
	t.Helper()
	t.Setenv("REVIER_STATE_HOME", root)
	events.Setup(root)
	t.Cleanup(func() { events.Setup("") })
}

const eachProjectTOML = `path = %q
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  launch = ["true"]
  match = { title = "^home$" }
`
