package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

const targetsConfig = `[ui]
theme = "catppuccin-mocha"

# Every project has these.
[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  place = "right top 75% 100%"
    # the agent
    [[target.runtime.panels]]
    kind = "agent"
    title = "Claude Code"
    command = ["claude"] # plain claude
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
`

// targetsRoot is a configuration root holding text as config.toml and one
// project with no targets of its own, and the shared targets as read.
func targetsRoot(t *testing.T, text string) (string, []map[string]any) {
	t.Helper()
	root := projectsRoot(t, text, map[string]string{"demo.toml": "path = \"/tmp/demo\"\n"})
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return root, cfg.Targets
}

func sharedTyped(t *testing.T, shared []map[string]any) []revier.Target {
	t.Helper()
	typed, err := config.DecodeTargets(shared)
	if err != nil {
		t.Fatal(err)
	}
	return typed
}

// A changed value is written in place; everything else, comments included,
// stays as it was.
func TestReplaceTargetWritesOnlyWhatChanged(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	editor := sharedTyped(t, shared)[1]
	editor.Window.Launch = []string{"code", "{{.Path}}"}
	editor.Window.Match.Title = "^{{.Name}}$"

	if _, err := config.ReplaceTarget(root, 1, shared, config.TargetEdit{Target: editor}); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`launch = ["idea", "{{.Path}}"] # IntelliJ`, `launch = ["code", "{{.Path}}"] # IntelliJ`,
		`title = "^{{.Name}}( |$)" }`, `title = "^{{.Name}}$" }`,
	).Replace(targetsConfig)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// Panels: a deleted one loses its entry and keeps its comments, a kept one is
// changed value by value, and a new one comes after the last.
func TestReplaceTargetEditsPanels(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	home := sharedTyped(t, shared)[0]
	home.Runtime.Panels = []revier.PanelSpec{
		{Kind: revier.PanelShell, Title: "zsh"},
		{Kind: revier.PanelTool, Title: "tests", Command: []string{"mise", "watch"}},
	}
	edit := config.TargetEdit{Target: home, PanelFrom: []int{1, -1}}
	if _, err := config.ReplaceTarget(root, 0, shared, edit); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(targetsConfig, `    # the agent
    [[target.runtime.panels]]
    kind = "agent"
    title = "Claude Code"
    command = ["claude"] # plain claude
    [[target.runtime.panels]]
    kind = "shell"
    title = "shell"
`, `    # the agent
    # plain claude
    [[target.runtime.panels]]
    kind = "shell"
    title = "zsh"
    [[target.runtime.panels]]
    kind = "tool"
    title = "tests"
    command = ["mise", "watch"]
`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A realization can be taken away and another given: the window table goes,
// comments kept, and a runtime table is added at the end of the entry.
func TestReplaceTargetSwapsARealization(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	editor := sharedTyped(t, shared)[1]
	editor.Window = nil
	editor.Runtime = &revier.Realization{
		Name:   "editor:{{.Name}}",
		Launch: []string{"nvim"},
		Match:  revier.Match{Title: "^editor:{{.Name}}$"},
	}
	if _, err := config.ReplaceTarget(root, 1, shared, config.TargetEdit{Target: editor}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(targetsConfig, `  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
`, `  # IntelliJ
  [target.runtime]
  name = "editor:{{.Name}}"
  launch = ["nvim"]
  match = { title = "^editor:{{.Name}}$" }
`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A new target is a new entry at the end, indented as the others are, and
// every project has it at once.
func TestAddTargetWritesAnEntry(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	tickets := revier.Target{
		Name: "tickets",
		Runtime: &revier.Realization{
			Name:   "tickets:{{.Name}}",
			Match:  revier.Match{Title: "^tickets:{{.Name}}$"},
			Panels: []revier.PanelSpec{{Kind: revier.PanelTool, Command: []string{"tt"}}},
		},
	}
	written, err := config.AddTarget(root, shared, tickets)
	if err != nil {
		t.Fatal(err)
	}
	want := targetsConfig + `
[[target]]
name = "tickets"
  [target.runtime]
  name = "tickets:{{.Name}}"
  match = { title = "^tickets:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "tool"
    command = ["tt"]
`
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
	if len(written.Shared) != 3 || len(written.Projects) != 1 || len(written.Projects[0].Targets) != 3 {
		t.Errorf("written = %d shared, projects %+v; want 3 shared and demo with 3 targets", len(written.Shared), written.Projects)
	}
}

// A removed target loses its tables and values; its comments stay.
func TestRemoveTargetKeepsItsComments(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	if _, err := config.RemoveTarget(root, 1, shared); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(targetsConfig, `
[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"] # IntelliJ
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
`, `
  # IntelliJ
`, 1)
	if got := readConfig(t, root); got != want {
		t.Errorf("config.toml =\n%s\nwant\n%s", got, want)
	}
}

// A change a project would not load with is not written, and the project
// file is named: without the shared home, demo has no home.
func TestATargetChangeThatBreaksAProjectIsRefused(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	_, err := config.RemoveTarget(root, 0, shared)
	if err == nil || !strings.Contains(err.Error(), "demo.toml") || !strings.Contains(err.Error(), "home") {
		t.Errorf("RemoveTarget = %v, want demo.toml named without a home", err)
	}
	if got := readConfig(t, root); got != targetsConfig {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// Shared targets that are not what the caller read are not touched.
func TestATargetEditOverAChangedFileIsRefused(t *testing.T) {
	root, shared := targetsRoot(t, targetsConfig)
	stale := shared[:1]
	if _, err := config.RemoveTarget(root, 0, stale); !errors.Is(err, config.ErrTargetsChanged) {
		t.Errorf("RemoveTarget = %v, want ErrTargetsChanged", err)
	}
	if got := readConfig(t, root); got != targetsConfig {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

// refusedConfig is targetsConfig with a third shared target that Load
// refuses for a value of the wrong type: launch is a list.
const refusedConfig = targetsConfig + `
[[target]]
name = "web"
  [target.window]
  launch = "firefox"
  match = { class = "^firefox$" }
`

// A shared target Load refuses is listed by its name, so the config screen
// shows it and can delete it (decisions.md D85).
func TestARefusedSharedTargetCanBeRemoved(t *testing.T) {
	root, shared := targetsRoot(t, refusedConfig)
	listed := config.ListTargets(shared)
	if len(listed) != 3 || listed[2].Name != "web" {
		t.Fatalf("ListTargets = %+v, want the refused web last, by name", listed)
	}
	if _, err := config.RemoveTarget(root, 2, shared); err != nil {
		t.Fatalf("RemoveTarget(web) = %v, want the refused target deleted", err)
	}
	if got := readConfig(t, root); strings.Contains(got, `name = "web"`) {
		t.Errorf("config.toml still holds web:\n%s", got)
	}
}

// A refused shared target costs the edit of another nothing.
func TestARefusedSharedTargetDoesNotBlockAnotherEdit(t *testing.T) {
	root, shared := targetsRoot(t, refusedConfig)
	edit := sharedTyped(t, shared[:1])[0]
	edit.Key = "ctrl-shift-h"
	if _, err := config.ReplaceTarget(root, 0, shared, config.TargetEdit{Target: edit, PanelFrom: []int{0, 1}}); err != nil {
		t.Errorf("ReplaceTarget(home) = %v, want it written", err)
	}
}

// A project is read with the shared targets it gets, and a refused one is
// not among them.
func TestAProjectReadsWithTheUsableSharedTargets(t *testing.T) {
	root, shared := targetsRoot(t, refusedConfig)
	if _, err := config.ReadProject(config.ProjectFile(root, "demo"), config.Usable(shared)); err != nil {
		t.Errorf("ReadProject = %v, want demo read without the refused web", err)
	}
}

// Two stray keys under [target.remote] are one problem, reported the same
// way on every parse, so an unrelated write is not refused for it.
func TestAnUnrelatedWriteIsNotRefusedForStrayRemoteKeys(t *testing.T) {
	root, _ := targetsRoot(t, targetsConfig+`
[[target]]
name = "web"
  [target.window]
  launch = ["firefox"]
  match = { class = "^firefox$" }
  [target.remote]
  name = "x"
  key = "y"
  prefer = "window"
`)
	for i := range 20 {
		shared := sharedOf(t, root)
		edit := sharedTyped(t, shared[:1])[0]
		edit.Key = []string{"ctrl-shift-h", "ctrl-shift-u"}[i%2]
		if _, err := config.ReplaceTarget(root, 0, shared, config.TargetEdit{Target: edit, PanelFrom: []int{0, 1}}); err != nil {
			t.Fatalf("ReplaceTarget(home) = %v, want it written", err)
		}
	}
}
