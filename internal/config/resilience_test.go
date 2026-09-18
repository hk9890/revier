package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
)

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

func named(projects []core.Project, name string) (core.Project, bool) {
	for _, p := range projects {
		if string(p.Name) == name {
			return p, true
		}
	}
	return core.Project{}, false
}

// Rule one. One project file that cannot be read costs that project and
// nothing else: every other file loads, and every command that needs them
// still runs. The whole set used to be refused, which took the surface, the
// keybindings and `revier popup` down with one bad file (decisions.md D85).
func TestOneBadFileDoesNotCostTheOthers(t *testing.T) {
	root := projectsRoot(t, "", map[string]string{
		"good.toml":   valid,
		"broken.toml": "path = \"/p\"\nthis is not toml\n",
		"later.toml":  valid,
	})

	_, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("%d projects, want all three listed", len(projects))
	}
	for _, name := range []string{"good", "later"} {
		p, ok := named(projects, name)
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		if probs := config.Problems(p); len(probs) > 0 {
			t.Errorf("%s: %v, want it whole", name, errors.Join(probs...))
		}
		if len(p.Targets) != 2 {
			t.Errorf("%s: %d targets, want both", name, len(p.Targets))
		}
	}
	bad, ok := named(projects, "broken")
	if !ok {
		t.Fatal("the broken project is not listed; its reason has nowhere to be read")
	}
	if bad.Invalid == nil {
		t.Error("Invalid = nil, want the parse failure")
	}
}

// Rule two. One target the project refuses costs that target and nothing
// else: the project loads, its other targets resolve, and the refused one
// carries why.
func TestOneBadTargetDoesNotCostTheProject(t *testing.T) {
	body := valid + `
[[target]]
name = "web"
  [target.window]
  launch = ["browser", "{{.Vars.absent}}"]
  match = { class = "^browser$" }
`
	root := projectsRoot(t, "", map[string]string{"demo.toml": body})
	_, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := named(projects, "demo")
	if !ok {
		t.Fatal("demo is missing")
	}
	if p.Invalid != nil {
		t.Fatalf("Invalid = %v, want the project to load", p.Invalid)
	}
	if len(p.Targets) != 3 {
		t.Fatalf("%d targets, want the refused one kept among them", len(p.Targets))
	}
	for i, want := range map[int]bool{0: false, 1: false, 2: true} {
		if got := p.TargetErr(i) != nil; got != want {
			t.Errorf("target %q refused = %v, want %v", p.Targets[i].Name, got, want)
		}
	}
	if err := p.TargetErr(2); !strings.Contains(err.Error(), `"web"`) {
		t.Errorf("the refusal should name the target: %v", err)
	}
}

// Rule three. A key nothing reads is not a refusal. The decoder ignores it,
// and the project it is written in loads whole, because a typo in a field
// that changes no behaviour must not cost a target that works.
func TestAnUnknownKeyIsIgnored(t *testing.T) {
	body := valid + "\nnot_a_key = \"whatever\"\n"
	root := projectsRoot(t, "", map[string]string{"demo.toml": body})
	_, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, _ := named(projects, "demo")
	if probs := config.Problems(p); len(probs) > 0 {
		t.Errorf("problems = %v, want an unknown key ignored", errors.Join(probs...))
	}
}

// A project-wide rule refuses the project, which is still listed and still
// carries its targets: the surface is where the reason is read, so a project
// that vanishes takes the reason with it.
func TestAProjectWideRefusalStillLists(t *testing.T) {
	body := strings.Replace(valid, "home = true\n", "", 1)
	root := projectsRoot(t, "", map[string]string{"demo.toml": body})
	_, projects, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := named(projects, "demo")
	if !ok {
		t.Fatal("demo is missing")
	}
	if p.Invalid == nil || !strings.Contains(p.Invalid.Error(), "no target is marked home") {
		t.Errorf("Invalid = %v, want the missing home named", p.Invalid)
	}
	if len(p.Targets) != 2 {
		t.Errorf("%d targets, want them kept for the surface to show", len(p.Targets))
	}
}

// A shared target is part of every project, so an edit to one is refused for
// what it breaks.
func TestASharedTargetEditIsRefusedForWhatItBreaks(t *testing.T) {
	shared := "[[target]]\nname = \"home\"\nhome = true\n  [target.runtime]\n  name = \"home\"\n  launch = [\"sh\"]\n  match = { title = \"^{{.Name}}$\" }\n"
	root := projectsRoot(t, shared, map[string]string{"demo.toml": "path = \"/tmp/demo\"\n"})

	_, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := config.RemoveTarget(root, 0, sharedOf(t, root)); err == nil {
		t.Fatal("removing the only home target was written, and demo has nothing to open")
	} else if !strings.Contains(err.Error(), "it would break") {
		t.Errorf("err = %v, want it to name what the write breaks", err)
	}
}

// A project that is already broken cannot veto an edit to the shared targets.
// Under the old rule one unfixed file locked the config screen against every
// other edit, which is the failure this whole change is about.
func TestAnAlreadyBrokenProjectDoesNotVetoASharedEdit(t *testing.T) {
	shared := "[[target]]\nname = \"editor\"\n  [target.window]\n  launch = [\"idea\"]\n  match = { class = \"^idea$\" }\n"
	root := projectsRoot(t, shared, map[string]string{
		"demo.toml":   valid,
		"broken.toml": "path = \"/p\"\nthis is not toml\n",
	})

	if _, err := config.RemoveTarget(root, 0, sharedOf(t, root)); err != nil {
		t.Fatalf("RemoveTarget: %v, want the edit written despite broken.toml", err)
	}
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
