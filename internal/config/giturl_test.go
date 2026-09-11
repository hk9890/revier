package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/pkg/revier"
)

// Every form the shell tool recorded in the session files on this machine
// loads: https, scp-style ssh, and ssh:// with a user and a port.
func TestGitURLLoads(t *testing.T) {
	for _, u := range []string{
		"https://github.com/hk9890/revier.git",
		"git@github.com:hk9890/revier.git",
		"ssh://git@bitbucket.example.com:7999/team/repo.git",
		"/srv/git/revier.git",
	} {
		dir := t.TempDir()
		body := strings.Replace(valid, "[vars]", "git_url = \""+u+"\"\n\n[vars]", 1)
		p, err := config.LoadProject(write(t, dir, "revier.toml", body), "")
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		if p.GitURL != u {
			t.Errorf("git_url = %q, want %q", p.GitURL, u)
		}
	}
}

// An unsafe URL is refused at load, and the error names the field, so the
// line to fix can be found. The rules are the shell tool's.
func TestUnsafeGitURLIsRejectedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, url, want string }{
		{"whitespace", "https://example.com/a b.git", "whitespace"},
		{"control character", "https://example.com/a\x01b.git", "control character"},
		{"command substitution", "git@example.com:$(reboot).git", "shell metacharacter"},
		{"separator", "https://example.com/a.git;rm", "shell metacharacter"},
		{"credentials", "https://user:s3cret@example.com/a.git", "credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := revier.Project{Path: "/p", GitURL: tc.url, Targets: []revier.Target{{
				Name: "home", Home: true,
				Runtime: &revier.Realization{Launch: []string{"x"}, Match: revier.Match{Title: "^x$"}},
			}}}
			err := config.Validate(p)
			if err == nil {
				t.Fatalf("%q: want a refusal", tc.url)
			}
			if !strings.Contains(err.Error(), "git_url") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to name git_url and %q", err, tc.want)
			}
		})
	}
}

// The credentials rule exists to keep a token out of the file; printing it in
// the refusal would put it on the terminal instead.
func TestACredentialURLIsNotEchoed(t *testing.T) {
	err := config.ValidateGitURL("https://user:s3cret@example.com/a.git")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("err = %v, want a refusal that does not repeat the token", err)
	}
}

// A new project is a file revier loads, named after the directory, with the
// directory's origin recorded.
func TestCreateWritesAProjectThatLoads(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "demo")

	p, err := config.Create(root, "demo", dir, "git@github.com:hk9890/demo.git")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.File != filepath.Join(root, "projects", "demo.toml") {
		t.Errorf("file = %q", p.File)
	}
	if p.Path != dir || p.GitURL != "git@github.com:hk9890/demo.git" {
		t.Errorf("project = %+v, want the directory and the origin", p.Project)
	}
	if _, ok := p.Home(); !ok {
		t.Error("a new project needs a home target to open")
	}
	if _, ok := p.Target("editor"); !ok {
		t.Error("a new project has an editor target")
	}
	_, projects, err := config.Load(root)
	if err != nil || len(projects) != 1 {
		t.Fatalf("Load = %d projects, %v; want the new one", len(projects), err)
	}
}

func TestCreateWithoutOriginRecordsNoGitURL(t *testing.T) {
	root := t.TempDir()
	p, err := config.Create(root, "demo", t.TempDir(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, _ := os.ReadFile(p.File)
	if strings.Contains(string(b), "git_url") {
		t.Errorf("file = %s, want no git_url line", b)
	}
}

// A project file is the user's. A second create under the same name must not
// replace the one they may have edited.
func TestCreateNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	if _, err := config.Create(root, "demo", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	file := config.ProjectFile(root, "demo")
	if err := os.WriteFile(file, []byte("# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Create(root, "demo", t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if b, _ := os.ReadFile(file); string(b) != "# edited\n" {
		t.Errorf("file = %q, the edit was overwritten", b)
	}
}

// A dot in a name is a regexp wildcard. The match has to find this project's
// window and not also one whose name differs in that position.
func TestCreateQuotesANameARegexpWouldRead(t *testing.T) {
	root := t.TempDir()
	p, err := config.Create(root, "example.com", t.TempDir(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	home, _ := p.Home()
	if got := home.Runtime.Match.Title; got != `^session:example\.com$` {
		t.Errorf("match = %q, want the dot quoted", got)
	}
}

// A path under the home directory is written as ~/..., because the file is
// shared between machines and the home directory is not.
func TestCreateWritesThePathRelativeToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	p, err := config.Create(root, "demo", filepath.Join(home, "dev", "demo"), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, _ := os.ReadFile(p.File)
	if !strings.Contains(string(b), `path = "~/dev/demo"`) {
		t.Errorf("file = %s, want the path relative to home", b)
	}
	if p.Path != filepath.Join(home, "dev", "demo") {
		t.Errorf("loaded path = %q, want it expanded again", p.Path)
	}
}

func TestNameForReplacesWhatTheShellToolReplaces(t *testing.T) {
	if got := config.NameFor("/x/my project:two"); got != "my_project_two" {
		t.Errorf("NameFor = %q", got)
	}
}

func TestCreateRefusesANameThatIsNotAFileName(t *testing.T) {
	for _, name := range []revier.ProjectName{"", "..", "a/b", "a b"} {
		if _, err := config.Create(t.TempDir(), name, t.TempDir(), ""); err == nil {
			t.Errorf("Create(%q): want a refusal", name)
		}
	}
}
