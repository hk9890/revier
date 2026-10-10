package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

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
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["code", "{{.Path}}"]
  match = { class = "^code$" }
`

const sharedConfig = `
[[target]]
name = "home"
home = true
key = "ctrl-shift-u"
  [target.runtime]
  name = "session:{{.Name}}"
  match = { title = "^session:{{.Name}}$" }
    [[target.runtime.panels]]
    kind = "agent"
    command = ["claude"]
    [[target.runtime.panels]]
    kind = "shell"

[[target]]
name = "editor"
key = "ctrl-shift-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea", title = "^{{.Name}}( |$)" }
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
