package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

// link is a project on another machine, in the smallest file that says so.
const link = `
[remote]
host = "buildbox"
`

func TestALinkDerivesItsPaneAndItsNameOnTheHost(t *testing.T) {
	p, err := config.LoadProject(write(t, t.TempDir(), "far.toml", link), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Remote == nil || p.Remote.Host != "buildbox" || p.Remote.Project != "far" {
		t.Fatalf("remote = %+v, want buildbox and the link's own name there", p.Remote)
	}
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want a derived runtime target", home)
	}
	want := []string{"ssh", "-t", "buildbox", "revier", "open", "far", "--attach"}
	if !slices.Equal(home.Runtime.Launch, want) {
		t.Errorf("launch = %q, want %q", home.Runtime.Launch, want)
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
	p, err := config.LoadProject(write(t, t.TempDir(), "build.toml", body), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Name != "build" || p.Remote.Project != "far" {
		t.Errorf("name %q, on the host %q; want build here and far there", p.Name, p.Remote.Project)
	}
	home, _ := p.Home()
	if i := slices.Index(home.Runtime.Launch, "far"); i < 0 {
		t.Errorf("launch = %q, want the host's name opened there", home.Runtime.Launch)
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
	p, err := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
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
	p, err = config.LoadProject(write(t, t.TempDir(), "far.toml", own), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
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
	p, err := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	home, ok := p.Home()
	if !ok || home.Runtime == nil {
		t.Fatalf("home = %+v, want the derived pane under the declared target", home)
	}
	if home.Runtime.Place != "right top 75% 100%" {
		t.Errorf("place = %q, want the one the link declared", home.Runtime.Place)
	}
	want := []string{"ssh", "-t", "buildbox", "revier", "open", "far", "--attach"}
	if !slices.Equal(home.Runtime.Launch, want) || home.Runtime.Match.Title != "^session:far$" {
		t.Errorf("home runtime = %+v, want the derived pane's launch and match", home.Runtime)
	}
}

// A shared target reaches a link through its remote part alone. One with no
// remote part is a local target, and no link has it (decisions.md D80).
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
	remoteOnLocal := "path = \"/srv/near\"\n" + `
[[target]]
name = "editor"
  [target.remote.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	if _, err := config.LoadProject(write(t, t.TempDir(), "near.toml", remoteOnLocal), nil); err == nil ||
		!strings.Contains(err.Error(), "[target.remote]") {
		t.Errorf("err = %v, want [target.remote] refused on a local project", err)
	}

	localOnLink := link + `
[[target]]
name = "editor"
  [target.window]
  launch = ["code"]
  match = { class = "^Code$" }
`
	if _, err := config.LoadProject(write(t, t.TempDir(), "far.toml", localOnLink), nil); err == nil ||
		!strings.Contains(err.Error(), "[target.remote.window]") {
		t.Errorf("err = %v, want a link's realization asked for under [target.remote.window]", err)
	}
}

// `revier link` writes the smallest file that says where the project is,
// and loads it back: the name on the host only when it differs.
func TestCreateLinkWritesTheRemoteTableAndLoadsItBack(t *testing.T) {
	root := t.TempDir()
	p, err := config.CreateLink(root, "far", "buildbox", "")
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

	build, err := config.CreateLink(root, "build", "buildbox", "far")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if build.Name != "build" || build.Remote.Project != "far" {
		t.Errorf("link = %+v, want build here and far there", build.Project)
	}
	if _, err := config.CreateLink(root, "far", "buildbox", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want the existing file kept", err)
	}
	if _, err := config.CreateLink(root, "bad", "-oProxyCommand=x", ""); err == nil {
		t.Error("want the host refused before anything is written")
	}
}

func TestALinkRefusesAGitURL(t *testing.T) {
	body := "git_url = \"git@github.com:hk9890/far.git\"\n" + link
	_, err := config.LoadProject(write(t, t.TempDir(), "far.toml", body), nil)
	if err == nil || !strings.Contains(err.Error(), "git_url") {
		t.Errorf("err = %v, want git_url refused on a link", err)
	}
}

func TestHostIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"flag", "-oProxyCommand=x", "dash"},
		{"whitespace", "build box", "whitespace"},
	} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+tc.host+`"`, 1)
		_, err := config.LoadProject(write(t, t.TempDir(), tc.name+".toml", body), nil)
		if err == nil || !strings.Contains(err.Error(), "remote.host:") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming remote.host and %q", tc.name, err, tc.want)
		}
	}
	for _, host := range []string{"buildbox", "hans@build.example.com", "10.0.0.7"} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+host+`"`, 1)
		if _, err := config.LoadProject(write(t, t.TempDir(), "ok.toml", body), nil); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}
