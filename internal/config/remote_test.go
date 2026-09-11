package config_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/config"
)

// remote is the fixture on another machine: the same file is read there, so
// its path is in that machine's terms, and the home target here is the ssh
// pane onto the workspace there.
const remote = `
name = "revier"
path = "~/dev/github/revier"
host = "buildbox"

[[target]]
name = "home"
home = true
  [target.runtime]
  name = "session:revier"
  launch = ["ssh", "-t", "buildbox", "revier", "open", "revier", "--attach"]
  match = { title = "^session:revier$" }
`

func TestRemoteProjectKeepsItsPathAsWritten(t *testing.T) {
	p, err := config.LoadProject(write(t, t.TempDir(), "revier.toml", remote), "")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Host != "buildbox" {
		t.Errorf("host = %q, want buildbox", p.Host)
	}
	if p.Path != "~/dev/github/revier" {
		t.Errorf("path = %q, want the tilde kept for the host to expand", p.Path)
	}
	home, _ := p.Target("home")
	if home.Runtime.Dir != "" {
		t.Errorf("dir = %q, want none: the path is not here", home.Runtime.Dir)
	}
}

// The same file is read on the host it names. There, with config.toml
// saying this machine is buildbox, the project is local: its path is
// expanded here, and nothing is asked of buildbox over ssh.
func TestAProjectOnThisMachineLoadsAsLocal(t *testing.T) {
	p, err := config.LoadProject(write(t, t.TempDir(), "revier.toml", remote), "buildbox")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if p.Host != "" {
		t.Errorf("host = %q, want none: this machine is buildbox", p.Host)
	}
	if strings.HasPrefix(p.Path, "~") {
		t.Errorf("path = %q, want it expanded here", p.Path)
	}
	other, err := config.LoadProject(write(t, t.TempDir(), "revier.toml", remote), "farbox")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if other.Host != "buildbox" {
		t.Errorf("host = %q on farbox, want buildbox kept", other.Host)
	}
}

func TestHostIsValidatedAtLoad(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"flag", "-oProxyCommand=x", "dash"},
		{"whitespace", "build box", "whitespace"},
		{"control character", `build\u0001box`, "control character"},
	} {
		body := strings.Replace(remote, `host = "buildbox"`, `host = "`+tc.host+`"`, 1)
		_, err := config.LoadProject(write(t, t.TempDir(), tc.name+".toml", body), "")
		if err == nil || !strings.Contains(err.Error(), "host:") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming host and %q", tc.name, err, tc.want)
		}
	}
	for _, host := range []string{"buildbox", "hans@build.example.com", "10.0.0.7"} {
		body := strings.Replace(remote, `host = "buildbox"`, `host = "`+host+`"`, 1)
		if _, err := config.LoadProject(write(t, t.TempDir(), "ok.toml", body), ""); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}
