package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

// projectRoot is a configuration root with the shared targets of sharedConfig
// and one project file, and what the screen is handed: the file and the
// shared targets.
func projectRoot(t *testing.T, body string) (string, []map[string]any) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := write(t, dir, "demo.toml", body)
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return file, cfg.Targets
}

func read(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const overriding = `# demo
path = "~/demo"

[[target]]
name = "editor"
key = "ctrl-o"

[[target]]
name = "pulls"
  [target.window]
  launch = ["chrome"]
  match = { class = "^chrome$" }
`

func TestReadProjectSaysWhereEachTargetComesFrom(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, err := config.ReadProject(file, shared)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if p.Path != "~/demo" {
		t.Errorf("path = %q, want it as written", p.Path)
	}
	want := map[revier.TargetName]config.Source{"home": config.FromShared, "editor": config.Overridden, "pulls": config.FromProject}
	if len(p.Targets) != len(want) {
		t.Fatalf("targets = %+v", p.Targets)
	}
	for _, pt := range p.Targets {
		if pt.Source != want[pt.Target.Name] {
			t.Errorf("%s: source %d, want %d", pt.Target.Name, pt.Source, want[pt.Target.Name])
		}
		if (pt.Shared != nil) != (pt.Source != config.FromProject) {
			t.Errorf("%s: shared = %v", pt.Target.Name, pt.Shared)
		}
	}
	editor := p.Targets[1]
	if editor.Target.Key != "ctrl-o" || editor.Shared.Key != "ctrl-shift-o" || editor.Target.Window.Match.Class != "^jetbrains-idea" {
		t.Errorf("editor = %+v, want the merged target and the shared one beside it", editor)
	}
}

func TestReadProjectMarksALinksDerivedHome(t *testing.T) {
	file, shared := projectRoot(t, link)
	p, err := config.ReadProject(file, shared)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "demo" {
		t.Errorf("remote = %+v, want the host and the derived name there", p.Remote)
	}
	if len(p.Targets) != 1 || p.Targets[0].Source != config.Derived || !p.Targets[0].Target.Home {
		t.Errorf("targets = %+v, want only the derived home: a link has no shared targets", p.Targets)
	}
}

// A shared target changed on the screen is written as what differs from
// config.toml, next to the comments, and nothing else.
func TestSaveProjectTargetWritesOnlyWhatDiffersFromShared(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, _ := config.ReadProject(file, shared)
	home := p.Targets[0].Target
	home.Runtime = new(*home.Runtime)
	home.Runtime.Match.Title = "^work$"

	if _, err := config.SaveProjectTarget(file, shared, "home", config.TargetEdit{Target: home, PanelFrom: []int{0, 1}}); err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	want := overriding + "\n[[target]]\nname = \"home\"\n  [target.runtime]\n  match = { title = \"^work$\" }\n"
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// Setting an override back to the shared value leaves the entry with nothing
// to say, and the entry goes.
func TestSaveProjectTargetDropsAnOverrideEqualToShared(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, _ := config.ReadProject(file, shared)
	editor := p.Targets[1].Target
	editor.Key = p.Targets[1].Shared.Key

	if _, err := config.SaveProjectTarget(file, shared, "editor", config.TargetEdit{Target: editor}); err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got := read(t, file); strings.Contains(got, `"editor"`) {
		t.Errorf("file =\n%s\nwant the editor entry gone", got)
	}
}

func TestSaveProjectTargetChangesAnOwnTargetInPlace(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, _ := config.ReadProject(file, shared)
	pulls := p.Targets[2].Target
	pulls.Name, pulls.Key = "prs", "ctrl-g"

	if _, err := config.SaveProjectTarget(file, shared, "pulls", config.TargetEdit{Target: pulls}); err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	want := strings.Replace(overriding, "name = \"pulls\"\n", "name = \"prs\"\nkey = \"ctrl-g\"\n", 1)
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// Each refusal leaves the file as it was.
func TestSaveProjectTargetRefuses(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, _ := config.ReadProject(file, shared)
	home, pulls := p.Targets[0].Target, p.Targets[2].Target

	cleared := home
	cleared.Key = ""
	renamedShared := home
	renamedShared.Name = "work"
	taken := pulls
	taken.Name = "editor"
	for name, c := range map[string]struct {
		was  revier.TargetName
		edit revier.Target
		want string
	}{
		"a shared value emptied":  {"home", cleared, "cannot empty it"},
		"a shared target renamed": {"home", renamedShared, "keeps its name"},
		"a name another has":      {"pulls", taken, "already"},
	} {
		if _, err := config.SaveProjectTarget(file, shared, c.was, config.TargetEdit{Target: c.edit}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	if got := read(t, file); got != overriding {
		t.Errorf("file =\n%s\nwant it unchanged", got)
	}
}

// Editing a link's derived home declares one, which replaces the derived
// pane.
func TestSaveProjectTargetDeclaresALinksHome(t *testing.T) {
	file, shared := projectRoot(t, link)
	p, _ := config.ReadProject(file, shared)
	home := p.Targets[0].Target
	home.Key = "ctrl-h"

	loaded, err := config.SaveProjectTarget(file, shared, "home", config.TargetEdit{Target: home, PanelFrom: nil})
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got, _ := loaded.Home(); got.Key != "ctrl-h" {
		t.Errorf("home = %+v, want the declared one", got)
	}
	if got := read(t, file); !strings.Contains(got, "[[target]]") || !strings.Contains(got, `"ssh"`) {
		t.Errorf("file =\n%s\nwant the home written whole", got)
	}
}

func TestRemoveProjectTargetResetsAnOverride(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, err := config.RemoveProjectTarget(file, shared, "editor")
	if err != nil {
		t.Fatalf("RemoveProjectTarget: %v", err)
	}
	for _, tg := range p.Targets {
		if tg.Name == "editor" && tg.Key != "ctrl-shift-o" {
			t.Errorf("editor key = %q, want the shared one back", tg.Key)
		}
	}
	if _, err := config.RemoveProjectTarget(file, shared, "home"); err == nil {
		t.Error("removing a target the file does not declare: no error")
	}
}

func TestSetProjectValue(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	if _, err := config.SetProjectValue(file, shared, "git_url", "git@github.com:me/demo.git"); err != nil {
		t.Fatalf("set git_url: %v", err)
	}
	want := strings.Replace(overriding, "path = \"~/demo\"\n", "path = \"~/demo\"\ngit_url = \"git@github.com:me/demo.git\"\n", 1)
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if _, err := config.SetProjectValue(file, shared, "git_url", ""); err != nil {
		t.Fatalf("clear git_url: %v", err)
	}
	if _, err := config.SetProjectValue(file, shared, "path", ""); err == nil {
		t.Error("clearing the path: no error")
	}
	if got := read(t, file); got != overriding {
		t.Errorf("file =\n%s\nwant it as it started", got)
	}
}
