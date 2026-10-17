package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/testenv"
	"github.com/hk9890/revier/pkg/revier"
)

// TestMain keeps every test off the user's own revier files (testenv.Run).
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}

const valid = `
path = "/home/user/dev/github/revier"

[vars]
url = "https://example.invalid/pulls"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "home"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["agent"]

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["code", "{{.Path}}"]
  match = { class = "^code$" }

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]
`

const sharedConfig = `
[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["agent"]

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"
`

// link is a project on another machine, in the smallest file that says so.
const link = `
[remote]
host = "buildbox"
`

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func read(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// projectsRoot writes a configuration root whose projects directory holds each
// of files, keyed by file name.
func projectsRoot(t *testing.T, cfg string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if cfg != "" {
		write(t, root, "config.toml", cfg)
	}
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		write(t, dir, name, body)
	}
	return root
}

// tabsConfig is shared targets in which home lists one of its two tabs.
const tabsConfig = `
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
  tabs = ["tickets"] # in this order

[[target]]
name = "agent"
  [target.runtime]
  inside = "home"
    [[target.runtime.panels]]
    kind = "agent"
    [[target.runtime.panels]]
    kind = "shell"

[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
  launch = ["taskmgr-ui"]
`

// writeConfig puts text in config.toml under a fresh root.
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	return projectsRoot(t, text, nil)
}

func readConfig(t *testing.T, root string) string {
	t.Helper()
	return read(t, config.File(root))
}

// sharedOf is the shared targets config.toml holds, as the caller of a write
// has them.
func sharedOf(t *testing.T, root string) []map[string]any {
	t.Helper()
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.Targets
}

// target is the named target of a loaded project.
func target(t *testing.T, p core.Project, name revier.TargetName) revier.Target {
	t.Helper()
	for _, tg := range p.Targets {
		if tg.Name == name {
			return tg
		}
	}
	t.Fatalf("project %s has no target %q", p.Name, name)
	return revier.Target{}
}

// names is the names of a loaded project's targets, in order.
func names(p core.Project) []revier.TargetName {
	var out []revier.TargetName
	for _, tg := range p.Targets {
		out = append(out, tg.Name)
	}
	return out
}

// refusalOf is why the named target of a loaded project was refused, or nil.
func refusalOf(p core.Project, name revier.TargetName) error {
	for i, t := range p.Targets {
		if t.Name == name {
			return p.TargetErr(i)
		}
	}
	return errors.New("no such target")
}

// projectRoot is a configuration root with the shared targets of sharedConfig
// and one project file, and what the screen is handed: the file and the
// shared targets.
func projectRoot(t *testing.T, body string) (string, []map[string]any) {
	t.Helper()
	root := projectsRoot(t, sharedConfig, map[string]string{"demo.toml": body})
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return config.ProjectFile(root, "demo"), cfg.Targets
}
