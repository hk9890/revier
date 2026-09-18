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
	root := projectsRoot(t, sharedConfig, map[string]string{"demo.toml": body})
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return config.ProjectFile(root, "demo"), cfg.Targets
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
		t.Errorf("targets = %+v, want only the derived home: no shared target here has a remote part", p.Targets)
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

// A write is refused for what it breaks, not for what was broken before it:
// a file with two refused targets is repaired one at a time, and a change
// that leaves the target it writes refused has not repaired it.
func TestAProjectIsRepairedOneTargetAtATime(t *testing.T) {
	body := "path = \"/tmp/demo\"\n\n[[target]]\nname = \"home\"\nhome = true\n  [target.runtime]\n  name = \"home\"\n  launch = [\"sh\"]\n  match = { title = \"^home$\" }\n\n" +
		"[[target]]\nname = \"web\"\n  [target.window]\n  launch = [\"browser\", \"{{.Vars.absent}}\"]\n  match = { class = \"^browser$\" }\n\n" +
		"[[target]]\nname = \"docs\"\n  [target.window]\n  launch = [\"zeal\"]\n  match = { class = \"^zeal($\" }\n"
	file := write(t, t.TempDir(), "demo.toml", body)
	if probs := config.Problems(config.LoadProject(file, nil)); len(probs) != 2 {
		t.Fatalf("problems = %v, want web and docs refused", probs)
	}

	web := revier.Target{Name: "web", Window: &revier.Realization{Launch: []string{"browser"}, Match: revier.Match{Class: "^browser$"}}}
	p, err := config.SaveProjectTarget(file, nil, "web", config.TargetEdit{Target: web})
	if err != nil {
		t.Fatalf("SaveProjectTarget(web): %v, want the repair written with docs still refused", err)
	}
	if probs := config.Problems(p); len(probs) != 1 || !strings.Contains(probs[0].Error(), "docs") {
		t.Errorf("problems = %v, want docs alone", probs)
	}

	stillBroken := revier.Target{Name: "docs", Window: &revier.Realization{Launch: []string{"zeal", "--new"}, Match: revier.Match{Class: "^zeal($"}}}
	if _, err := config.SaveProjectTarget(file, nil, "docs", config.TargetEdit{Target: stillBroken}); err == nil || !strings.Contains(err.Error(), "not written") {
		t.Errorf("err = %v, want a change that leaves docs refused refused", err)
	}
	docs := revier.Target{Name: "docs", Window: &revier.Realization{Launch: []string{"zeal"}, Match: revier.Match{Class: "^zeal$"}}}
	p, err = config.SaveProjectTarget(file, nil, "docs", config.TargetEdit{Target: docs})
	if err != nil {
		t.Fatalf("SaveProjectTarget(docs): %v", err)
	}
	if probs := config.Problems(p); len(probs) != 0 {
		t.Errorf("problems = %v, want the file whole", probs)
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
	// The panels are written by kind alone: the argv that reaches the host
	// is the remote port's, and a declared home with kind-only panels gets
	// it as the derived one does.
	if got := read(t, file); !strings.Contains(got, "[[target]]") || !strings.Contains(got, `kind = "agent"`) || strings.Contains(got, "ssh") {
		t.Errorf("file =\n%s\nwant the home written whole, by panel kind", got)
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

// A link's project screen shows the shared targets it has through their
// remote part, and an edit of one is written under [target.remote]
// (decisions.md D82).
func TestProjectScreenWritesALinksOverrideUnderRemote(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", sharedConfig+`
[[target]]
name = "remote-editor"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}", "{{.Path}}"]
  match = { class = "^Code$" }
`)
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := write(t, dir, "far.toml", "path = \"/srv/far\"\n"+link)
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	shared := cfg.Targets

	text, err := config.ReadProject(file, shared)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	var editor config.ProjectTarget
	for _, pt := range text.Targets {
		if pt.Target.Name == "remote-editor" {
			editor = pt
		}
	}
	if editor.Source != config.FromShared || editor.Shared == nil {
		t.Fatalf("editor = %+v, want the shared target's remote part", editor)
	}

	want := editor.Target
	want.Window.Match.Title = "^far \\[SSH: buildbox\\]"
	loaded, err := config.SaveProjectTarget(file, shared, "remote-editor", config.TargetEdit{Target: want})
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got := read(t, file); !strings.Contains(got, "[target.remote.window]") {
		t.Errorf("file =\n%s\nwant the override under [target.remote.window]", got)
	}
	got, _ := loaded.Target("remote-editor")
	if got.Window.Match.Title != "^far \\[SSH: buildbox\\]" || got.Window.Launch[0] != "code" {
		t.Errorf("editor = %+v, want the title overridden and the shared launch kept", got.Window)
	}
}
