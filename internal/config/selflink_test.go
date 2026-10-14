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

// The project files are shared between machines, and a link that is right on
// one names the other one itself. Load leaves such a link out, by the host's
// own name, with a login in front of it or not, or by the HostName ssh
// connects to, and says which files it left.
func TestALinkToThisMachineIsNotLoaded(t *testing.T) {
	machine, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname:", err)
	}
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host alias\n  HostName "+machine+".local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)
	root := t.TempDir()
	dir := filepath.Join(root, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "far.toml", link)
	byName := write(t, dir, "byname.toml", strings.Replace(link, "buildbox", machine, 1))
	byAlias := write(t, dir, "byalias.toml", strings.Replace(link, "buildbox", "alias", 1))
	byLogin := write(t, dir, "bylogin.toml", strings.Replace(link, "buildbox", "me@"+machine, 1))

	cfg, projects, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []revier.ProjectName
	for _, p := range projects {
		got = append(got, p.Name)
	}
	if !slices.Equal(got, []revier.ProjectName{"far"}) {
		t.Errorf("projects = %v, want the link to buildbox alone", got)
	}
	slices.Sort(cfg.SelfLinks)
	if want := []string{byAlias, byLogin, byName}; !slices.Equal(cfg.SelfLinks, want) {
		t.Errorf("SelfLinks = %v, want %v", cfg.SelfLinks, want)
	}
}
