package config_test

import (
	"os"
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
	p, err := config.LoadProject(write(t, t.TempDir(), "far.toml", link))
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
	p, err := config.LoadProject(write(t, t.TempDir(), "build.toml", body))
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

// A link may declare targets of its own, local windows onto the project; a
// declared home is kept and nothing is derived beside it. The host's path,
// when written, is kept as written for templates: it is not a path here.
func TestALinkKeepsItsOwnTargetsAndTheHostsPath(t *testing.T) {
	body := "path = \"~/dev/far\"\n" + link + `
[[target]]
name = "editor"
key = "ctrl-o"
  [target.window]
  launch = ["code", "--remote", "ssh-remote+buildbox", "{{.Path}}"]
  match = { title = "far \\[SSH: buildbox\\]" }
`
	p, err := config.LoadProject(write(t, t.TempDir(), "far.toml", body))
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
  [target.runtime]
  name = "far"
  launch = ["ssh", "buildbox"]
  match = { title = "^far$" }
`
	p, err = config.LoadProject(write(t, t.TempDir(), "far.toml", own))
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if home, _ := p.Home(); home.Name != "shell" || len(p.Targets) != 1 {
		t.Errorf("targets = %+v, want the declared home alone", p.Targets)
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
	_, err := config.LoadProject(write(t, t.TempDir(), "far.toml", body))
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
		_, err := config.LoadProject(write(t, t.TempDir(), tc.name+".toml", body))
		if err == nil || !strings.Contains(err.Error(), "remote.host:") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming remote.host and %q", tc.name, err, tc.want)
		}
	}
	for _, host := range []string{"buildbox", "hans@build.example.com", "10.0.0.7"} {
		body := strings.Replace(link, `host = "buildbox"`, `host = "`+host+`"`, 1)
		if _, err := config.LoadProject(write(t, t.TempDir(), "ok.toml", body)); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}
