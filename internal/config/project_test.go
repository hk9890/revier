package config_test

import (
	"os"
	"path/filepath"
	"slices"
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
	want := map[revier.TargetName]config.Source{"home": config.FromShared, "editor": config.Overridden, "agent": config.FromShared, "pulls": config.FromProject}
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
	if len(p.Targets) != 2 || p.Targets[0].Source != config.Derived || !p.Targets[0].Target.Home ||
		p.Targets[1].Source != config.Derived || p.Targets[1].Target.Name != "agent" {
		t.Errorf("targets = %+v, want only the derived home and its tab: no shared target here has a remote part", p.Targets)
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

	if _, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: home, PanelFrom: []int{0, 1}}, shared); err != nil {
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

	if _, err := config.SaveProjectTarget(file, "editor", config.TargetEdit{Target: editor}, shared); err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got := read(t, file); strings.Contains(got, `"editor"`) {
		t.Errorf("file =\n%s\nwant the editor entry gone", got)
	}
}

func TestSaveProjectTargetChangesAnOwnTargetInPlace(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, _ := config.ReadProject(file, shared)
	pulls := p.Targets[3].Target
	pulls.Name, pulls.Key = "prs", "ctrl-g"

	if _, err := config.SaveProjectTarget(file, "pulls", config.TargetEdit{Target: pulls}, shared); err != nil {
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
		if _, err := config.SaveProjectTarget(file, c.was, config.TargetEdit{Target: c.edit}, shared); err == nil || !strings.Contains(err.Error(), c.want) {
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
	p, err := config.SaveProjectTarget(file, "web", config.TargetEdit{Target: web}, nil)
	if err != nil {
		t.Fatalf("SaveProjectTarget(web): %v, want the repair written with docs still refused", err)
	}
	if probs := config.Problems(p); len(probs) != 1 || !strings.Contains(probs[0].Error(), "docs") {
		t.Errorf("problems = %v, want docs alone", probs)
	}

	stillBroken := revier.Target{Name: "docs", Window: &revier.Realization{Launch: []string{"zeal", "--new"}, Match: revier.Match{Class: "^zeal($"}}}
	if _, err := config.SaveProjectTarget(file, "docs", config.TargetEdit{Target: stillBroken}, nil); err == nil || !strings.Contains(err.Error(), "not written") {
		t.Errorf("err = %v, want a change that leaves docs refused refused", err)
	}
	docs := revier.Target{Name: "docs", Window: &revier.Realization{Launch: []string{"zeal"}, Match: revier.Match{Class: "^zeal$"}}}
	p, err = config.SaveProjectTarget(file, "docs", config.TargetEdit{Target: docs}, nil)
	if err != nil {
		t.Fatalf("SaveProjectTarget(docs): %v", err)
	}
	if probs := config.Problems(p); len(probs) != 0 {
		t.Errorf("problems = %v, want the file whole", probs)
	}
}

// Editing a link's derived home declares one, which the derived name, match
// and tab still fill. The list of tabs it was shown with is derived again at
// every load, so it is not written.
func TestSaveProjectTargetDeclaresALinksHome(t *testing.T) {
	file, shared := projectRoot(t, link)
	p, _ := config.ReadProject(file, shared)
	home := p.Targets[0].Target
	home.Key = "ctrl-h"

	loaded, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: home, PanelFrom: nil}, shared)
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if got, _ := loaded.Home(); got.Key != "ctrl-h" || !slices.Equal(got.Runtime.Tabs, []revier.TargetName{"agent"}) {
		t.Errorf("home = %+v, want the declared one, with the derived tab", got)
	}
	if probs := config.Problems(loaded); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
	want := link + "\n[[target]]\nname = \"home\"\nkey = \"ctrl-h\"\nhome = true\n    [target.remote.runtime]\n    name = \"session:demo\"\n    match = { title = \"^session:demo$\" }\n"
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// Editing a link's derived agent tab declares it, with its panels by kind
// alone: the argv that reaches the host is the remote port's, and a declared
// tab with kind-only panels gets it as the derived one does.
func TestSaveProjectTargetDeclaresALinksAgentTab(t *testing.T) {
	file, shared := projectRoot(t, link)
	p, _ := config.ReadProject(file, shared)
	tab := p.Targets[1].Target
	tab.Key = "ctrl-a"

	loaded, err := config.SaveProjectTarget(file, "agent", config.TargetEdit{Target: tab, PanelFrom: []int{-1, -1}}, shared)
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	if probs := config.Problems(loaded); len(probs) > 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
	if got := target(t, loaded, "agent"); got.Key != "ctrl-a" || got.Runtime.Inside != "home" || len(got.Runtime.Panels) != 2 {
		t.Errorf("agent = %+v, want the declared tab of home", got)
	}
	got := read(t, file)
	if !strings.Contains(got, "[target.remote.runtime]\n    inside = \"home\"\n") || !strings.Contains(got, "[[target.remote.runtime.panels]]\n      kind = \"agent\"") || strings.Contains(got, "ssh") {
		t.Errorf("file =\n%s\nwant the tab written under [target.remote.runtime], by panel kind", got)
	}
}

func TestRemoveProjectTargetResetsAnOverride(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	p, err := config.RemoveProjectTarget(file, "editor", shared)
	if err != nil {
		t.Fatalf("RemoveProjectTarget: %v", err)
	}
	for _, tg := range p.Targets {
		if tg.Name == "editor" && tg.Key != "ctrl-shift-o" {
			t.Errorf("editor key = %q, want the shared one back", tg.Key)
		}
	}
	if _, err := config.RemoveProjectTarget(file, "home", shared); err == nil {
		t.Error("removing a target the file does not declare: no error")
	}
}

func TestSetProjectValue(t *testing.T) {
	file, shared := projectRoot(t, overriding)
	if _, err := config.SetProjectValue(file, "git_url", "git@github.com:me/demo.git", shared); err != nil {
		t.Fatalf("set git_url: %v", err)
	}
	want := strings.Replace(overriding, "path = \"~/demo\"\n", "path = \"~/demo\"\ngit_url = \"git@github.com:me/demo.git\"\n", 1)
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if _, err := config.SetProjectValue(file, "git_url", "", shared); err != nil {
		t.Fatalf("clear git_url: %v", err)
	}
	if _, err := config.SetProjectValue(file, "path", "", shared); err == nil {
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
	loaded, err := config.SaveProjectTarget(file, "remote-editor", config.TargetEdit{Target: want}, shared)
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

// The tabs and the active tab of a shared target are overridden as its other
// values are: written when they differ from config.toml, gone when they do
// not.
func TestSaveProjectTargetWritesTabsAndActive(t *testing.T) {
	const body = "path = \"/p\"\n"
	root := projectsRoot(t, tabsConfig, map[string]string{"demo.toml": body})
	file, shared := config.ProjectFile(root, "demo"), sharedOf(t, root)
	p, err := config.ReadProject(file, shared)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	home := p.Targets[0].Target
	home.Runtime = new(*home.Runtime)
	home.Runtime.Tabs, home.Runtime.Active = []revier.TargetName{"agent", "tickets"}, "tickets"

	loaded, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: home}, shared)
	if err != nil {
		t.Fatalf("SaveProjectTarget: %v", err)
	}
	want := body + "\n[[target]]\nname = \"home\"\n  [target.runtime]\n  tabs = [\"agent\", \"tickets\"]\n  active = \"tickets\"\n"
	if got := read(t, file); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	got, _ := loaded.Target("home")
	if !slices.Equal(got.Runtime.Tabs, home.Runtime.Tabs) || got.Runtime.Active != "tickets" {
		t.Errorf("home tabs = %v, active = %q; want them as written", got.Runtime.Tabs, got.Runtime.Active)
	}

	if _, err := config.SaveProjectTarget(file, "home", config.TargetEdit{Target: p.Targets[0].Target}, shared); err != nil {
		t.Fatalf("SaveProjectTarget back to shared: %v", err)
	}
	if got := read(t, file); strings.Contains(got, `"home"`) {
		t.Errorf("file =\n%s\nwant the home entry gone", got)
	}
}
