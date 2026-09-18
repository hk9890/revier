package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/adapter/ssh"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// link is a project on another machine, in the smallest file that says so.
const link = `
[remote]
host = "buildbox"
`

func TestALinkDerivesItsPaneAndItsNameOnTheHost(t *testing.T) {
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", link), nil)
	if p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Fatalf("remote = %+v, want buildbox and the link's own name there", p.Remote)
	}
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want a derived runtime target", home)
	}
	panels := home.Runtime.Panels
	if len(panels) != 2 || panels[0].Kind != revier.PanelAgent || panels[1].Kind != revier.PanelShell {
		t.Fatalf("panels = %+v, want an agent and a shell", panels)
	}
	if got, want := panels[0].Command, ssh.PanelCommand("buildbox", "far", "agent"); !slices.Equal(got, want) {
		t.Errorf("agent = %q, want %q", got, want)
	}
	if got, want := panels[1].Command, ssh.PanelCommand("buildbox", "far", "shell"); !slices.Equal(got, want) {
		t.Errorf("shell = %q, want %q", got, want)
	}
	if len(home.Runtime.Launch) != 0 {
		t.Errorf("launch = %q, want none beside the panels", home.Runtime.Launch)
	}
	if home.Runtime.Name != "session:far" || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("name %q, match %q: the pane must be found again by its title", home.Runtime.Name, home.Runtime.Match.Title)
	}
	if home.Runtime.Dir != "" {
		t.Errorf("dir = %q, want none: nothing of the project is here", home.Runtime.Dir)
	}
}

// The link's name here and the project's name on the host may differ: the
// host is asked by its name, the list shows this one.
func TestALinkMayNameTheProjectDifferentlyOnTheHost(t *testing.T) {
	body := strings.Replace(link, `host = "buildbox"`, "host = \"buildbox\"\nproject = \"far\"", 1)
	p := config.LoadProject(write(t, t.TempDir(), "build.toml", body), nil)
	if p.Name != "build" || p.Remote.Project != "far" {
		t.Errorf("name %q, on the host %q; want build here and far there", p.Name, p.Remote.Project)
	}
	home, _ := p.Home()
	if got, want := home.Runtime.Panels[0].Command, ssh.PanelCommand("buildbox", "far", "agent"); !slices.Equal(got, want) {
		t.Errorf("agent = %q, want the project named as the host names it: %q", got, want)
	}
}

// A link may declare targets of its own, windows here that reach the project
// there; it writes them under [target.remote]. A declared home keeps what it
// says and the pane fills the rest. The host's path, when written, is kept as
// written for templates: it is not a path here.
func TestALinkKeepsItsOwnTargetsAndTheHostsPath(t *testing.T) {
	body := "path = \"~/dev/far\"\n" + link + `
[[target]]
name = "editor"
key = "ctrl-o"
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match = { title = "far \\[SSH: buildbox\\]" }
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if p.Path != "~/dev/far" {
		t.Errorf("path = %q, want the host's, kept as written", p.Path)
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Window.Launch[3] != "~/dev/far" {
		t.Errorf("editor = %+v, want the host's path rendered into its launch", editor)
	}
	if _, ok := p.Home(); !ok {
		t.Error("the pane is still derived beside a declared target")
	}
	if len(p.Targets) != 2 {
		t.Errorf("targets = %d, want the derived home and the editor", len(p.Targets))
	}

	own := link + `
[[target]]
name = "shell"
home = true
  [target.remote.runtime]
  name = "far"
  launch = ["ssh", "buildbox"]
  match = { title = "^far$" }
`
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", own), nil)
	if home, _ := p.Home(); home.Name != "shell" || len(p.Targets) != 1 {
		t.Errorf("targets = %+v, want the declared home alone", p.Targets)
	}
	if home, _ := p.Home(); !slices.Equal(home.Runtime.Launch, []string{"ssh", "buildbox"}) {
		t.Errorf("launch = %q, want the one the link declared", home.Runtime.Launch)
	}
}

// A home the link does not launch itself still reaches the workspace: the
// pane fills every field it left empty, so a placement alone is enough.
func TestALinkHomeTakesThePaneForWhatItLeavesOut(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
  place = "right top 75% 100%"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want the derived pane under the declared target", home)
	}
	if home.Runtime.Place != "right top 75% 100%" {
		t.Errorf("place = %q, want the one the link declared", home.Runtime.Place)
	}
	if len(home.Runtime.Panels) != 2 || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("home runtime = %+v, want the derived pane's panels and match", home.Runtime)
	}
}

// A shared target reaches a link through its remote part alone. One with no
// remote part is a local target, and no link has it (decisions.md D82).
func TestSharedTargetsReachALinkThroughTheirRemotePart(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "home"
home = true
key = "ctrl-u"
  [target.runtime]
  name = "session:{{.Name}}"
  launch = ["kitty"]
  match = { title = "^session:{{.Name}}$" }
  place = "left top 50% 100%"
  [target.remote.runtime]
  place = "right top 75% 100%"

[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["idea", "{{.Path}}"]
  match = { class = "^jetbrains-idea" }
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}", "{{.Path}}"]
  match = { class = "^Code$", title = "\\[SSH: {{.Remote.Host}}\\]" }

[[target]]
name = "tickets"
  [target.runtime]
  inside = "home"
  launch = ["tickets"]
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "far.toml", "path = \"/srv/far\"\n"+link)
	write(t, filepath.Join(root, "projects"), "near.toml", "path = \"/srv/near\"\n")
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, local := loaded[0], loaded[1]
	if p.Name != "far" {
		p, local = local, p
	}
	if _, ok := p.Target("tickets"); ok {
		t.Error("a shared target with no remote part is local; a link does not get it")
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Window == nil || editor.Key != "ctrl-o" {
		t.Fatalf("editor = %+v, want the remote realization under the shared name and key", editor)
	}
	want := []string{"code", "--remote", "ssh-remote+buildbox", "/srv/far"}
	if !slices.Equal(editor.Window.Launch, want) {
		t.Errorf("launch = %q, want %q", editor.Window.Launch, want)
	}
	if editor.Runtime != nil {
		t.Errorf("runtime = %+v, want the local part left behind", editor.Runtime)
	}
	home, _ := p.Home()
	if home.Runtime.Place != "right top 75% 100%" {
		t.Errorf("place = %q, want the remote part's", home.Runtime.Place)
	}
	if home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("match = %q, want the derived pane's, not the local part's", home.Runtime.Match.Title)
	}

	near, _ := local.Target("editor")
	if near.Window.Launch[0] != "idea" {
		t.Errorf("launch = %q, want the local part for a local project", near.Window.Launch)
	}
	if _, ok := local.Target("tickets"); !ok {
		t.Error("a local project keeps a target with no remote part")
	}
}

// The part of the other kind would never be read, so the file that writes it
// does not load.
func TestAProjectFileWritesOnlyThePartOfItsKind(t *testing.T) {
	remoteOnLocal := valid + `
[[target]]
name = "pages"
  [target.remote.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	p := config.LoadProject(write(t, t.TempDir(), "near.toml", remoteOnLocal), nil)
	if err := refusalOf(p, "pages"); err == nil || !strings.Contains(err.Error(), "[target.remote]") {
		t.Errorf("err = %v, want [target.remote] refused on a local project", err)
	}
	// The mistake is one target's, and costs that target: the project loads
	// with its home, as any refused target leaves it (decisions.md D85).
	if p.Invalid != nil || refusalOf(p, "home") != nil {
		t.Errorf("Invalid = %v, home refused = %v; want the project and its home whole", p.Invalid, refusalOf(p, "home"))
	}

	localOnLink := link + `
[[target]]
name = "pages"
  [target.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	p = config.LoadProject(write(t, t.TempDir(), "far.toml", localOnLink), nil)
	if err := refusalOf(p, "pages"); err == nil || !strings.Contains(err.Error(), "[target.remote.window]") {
		t.Errorf("err = %v, want a link's realization asked for under [target.remote.window]", err)
	}
	if p.Invalid != nil || refusalOf(p, "home") != nil {
		t.Errorf("Invalid = %v, home refused = %v; want the link and its derived home whole", p.Invalid, refusalOf(p, "home"))
	}
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

// The drop rule runs both ways: a shared target with only a remote part is a
// link's, and a local project is not left holding a target with nothing to
// open.
func TestASharedTargetWithNoLocalPartIsNotALocalProjectsTarget(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "shell"
  [target.remote.runtime]
  name = "far"
  launch = ["ssh", "buildbox"]
  match = { title = "^far$" }
`)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "projects"), "near.toml", `
path = "/srv/near"
[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:near"
  launch = ["kitty"]
  match = { title = "^session:near$" }
`)
	_, loaded, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded[0].Target("shell"); ok {
		t.Error("a shared target with no local part is a link's; a local project does not get it")
	}
}

// A target the link declares itself is refused when it names no realization,
// the way a local project's is. Dropped instead, its key would answer to
// nothing and no message would say why.
func TestALinkTargetWithNoRealizationIsRefused(t *testing.T) {
	body := link + `
[[target]]
name = "editor"
key = "ctrl-o"
`
	err := loadErr(write(t, t.TempDir(), "far.toml", body), nil)
	if err == nil || !strings.Contains(err.Error(), "declares no realization") {
		t.Errorf("err = %v, want the link's own editor refused for declaring no realization", err)
	}
}

// A home the link opens as a window has named the tool that reaches the
// workspace, so the ssh pane is not put beside it: with no window host here
// the pane would answer instead of the window the link asked for.
func TestALinkHomeThatIsAWindowKeepsThePaneOut(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+buildbox"]
  match = { class = "^Code$" }
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if home, _ := p.Home(); home.Runtime != nil {
		t.Errorf("home runtime = %+v, want the declared window alone", home.Runtime)
	}
}

// A home that declares panels is launched by them, so the pane does not add
// a launch that the same load would then refuse beside them.
func TestALinkHomeWithPanelsKeepsThePanesLaunchOut(t *testing.T) {
	body := link + `
[[target]]
name = "home"
home = true
  [target.remote.runtime]
    [[target.remote.runtime.panels]]
    kind = "shell"
    title = "shell"
`
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	home, _ := p.Home()
	if len(home.Runtime.Launch) != 0 || len(home.Runtime.Panels) != 1 {
		t.Errorf("home runtime = %+v, want the panels alone", home.Runtime)
	}
	if home.Runtime.Name != "session:far" || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("home runtime = %+v, want the pane's name and match filled in", home.Runtime)
	}
}

// [target.remote] holds a realization each and nothing else: a key beside
// them is lifted onto the target for a link alone, and would change what the
// name and the key mean there.
func TestARemoteTableHoldsOnlyRealizations(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "editor"
  [target.window]
  launch = ["idea"]
  match = { class = "^idea$" }
  [target.remote]
  name = "renamed"
`)
	cfg, _, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0].Error(), "[target.remote] holds") {
		t.Errorf("problems = %v, want \"name\" under [target.remote] refused", cfg.Problems)
	}

	body := link + `
[[target]]
name = "editor"
  [target.remote]
  home = true
    [target.remote.window]
    launch = ["code"]
    match = { class = "^Code$" }
`
	if err := loadErr(write(t, t.TempDir(), "far.toml", body), nil); err == nil ||
		!strings.Contains(err.Error(), "[target.remote] holds") {
		t.Errorf("err = %v, want \"home\" under [target.remote] refused in a link file too", err)
	}
}

// `revier link` loads the new link with the shared targets, so the project it
// hands back is the one the next start reads, and a shared target that would
// refuse the link is caught before the file stays.
func TestCreateLinkGivesTheLinkItsSharedRemoteTargets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "config.toml", `
[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["idea"]
  match = { class = "^idea$" }
  [target.remote.window]
  launch = ["code", "--remote", "ssh-remote+{{.Remote.Host}}"]
  match = { class = "^Code$" }
`)
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	editor, ok := p.Target("editor")
	if !ok || editor.Key != "ctrl-o" {
		t.Fatalf("targets = %+v, want the shared editor under its key", p.Targets)
	}
	want := []string{"code", "--remote", "ssh-remote+buildbox"}
	if !slices.Equal(editor.Window.Launch, want) {
		t.Errorf("launch = %q, want %q", editor.Window.Launch, want)
	}
}

// `revier link` writes the smallest file that says where the project is,
// and loads it back: the name on the host only when it differs.
func TestCreateLinkWritesTheRemoteTableAndLoadsItBack(t *testing.T) {
	root := t.TempDir()
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if p.Name != "far" || p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Errorf("link = %+v, want far on buildbox", p.Project)
	}
	body, err := os.ReadFile(config.ProjectFile(root, "far"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "project =") {
		t.Errorf("file = %q: the same name is not written twice", body)
	}

	build, err := config.CreateLink(root, "build", "buildbox", revier.Project{Name: "far", Path: "/srv/far"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if build.Name != "build" || build.Remote.Project != "far" {
		t.Errorf("link = %+v, want build here and far there", build.Project)
	}
	if _, err := config.CreateLink(root, "far", "buildbox", revier.Project{Path: "/srv/far"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want the existing file kept", err)
	}
	if _, err := config.CreateLink(root, "bad", "-oProxyCommand=x", revier.Project{Path: "/srv/far"}); err == nil {
		t.Error("want the host refused before anything is written")
	}
	// A project that is a link on the host reaches a third machine, whose
	// checkout `revier agent exec` on the host refuses to serve.
	chained := revier.Project{Name: "far", Path: "/srv/far", Remote: &revier.Link{Host: "third", Project: "far"}}
	if _, err := config.CreateLink(root, "chained", "buildbox", chained); err == nil || !strings.Contains(err.Error(), "third") {
		t.Errorf("err = %v, want the link refused, naming the machine that holds the project", err)
	}
	if _, err := os.Stat(config.ProjectFile(root, "chained")); err == nil {
		t.Error("a refused link left a file behind")
	}
}

// A link holds the repository the host records, for a target here that
// renders it. The checkout it names is still the host's, which
// internal/checkout refuses to clone here (decisions.md D83).
func TestALinkKeepsTheHostsGitURL(t *testing.T) {
	body := "path = \"/srv/far\"\ngit_url = \"git@github.com:example/far.git\"\n" + link
	p := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if p.GitURL != "git@github.com:example/far.git" {
		t.Errorf("git_url = %q, want the host's", p.GitURL)
	}
	bad := "path = \"/srv/far\"\ngit_url = \"git@github.com:$(id).git\"\n" + link
	if err := loadErr(write(t, t.TempDir(), "bad.toml", bad), nil); err == nil {
		t.Error("a git_url a link records is validated as any other")
	}
}

// `revier link` records what the host says about the project, so a target
// here renders a path and a repository that name nothing on this machine.
func TestCreateLinkRecordsTheHostsPathAndRepository(t *testing.T) {
	root := t.TempDir()
	p, err := config.CreateLink(root, "far", "buildbox", revier.Project{
		Name:   "far",
		Path:   "/home/user/dev/far",
		GitURL: "git@github.com:example/far.git",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if p.Path != "/home/user/dev/far" || p.GitURL != "git@github.com:example/far.git" {
		t.Errorf("link = %+v, want the host's path and repository", p.Project)
	}
	body, err := os.ReadFile(config.ProjectFile(root, "far"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `path = "/home/user/dev/far"`) || !strings.Contains(string(body), "git_url =") {
		t.Errorf("file = %q, want the host's answer written into it", body)
	}
}

func TestHostIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"flag", "-oProxyCommand=x", "dash"},
		{"whitespace", "build box", "whitespace"},
	} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+tc.host+`"`, 1)
		err := loadErr(write(t, t.TempDir(), tc.name+".toml", body), nil)
		if err == nil || !strings.Contains(err.Error(), "remote.host:") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming remote.host and %q", tc.name, err, tc.want)
		}
	}
	for _, host := range []string{"buildbox", "hans@build.example.com", "10.0.0.7"} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+host+`"`, 1)
		if err := loadErr(write(t, t.TempDir(), "ok.toml", body), nil); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}

// loadErr is every reason a file did not load whole, joined: what LoadProject
// returned as its error before a project file's mistake stopped costing the
// project, or the set, more than the part that is wrong (decisions.md D85).
func loadErr(path string, shared []map[string]any) error {
	return errors.Join(config.Problems(config.LoadProject(path, shared))...)
}
