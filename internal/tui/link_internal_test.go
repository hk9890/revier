package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The link dialog runs with no root model: its three steps, the link the
// last one writes, and Esc on the first one, which is all it tells the
// surface.
func TestTheLinkScreenWalksItsStepsWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), list: 60, pane: 40, page: 5}
	enter, esc := tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyEsc}
	s := newLinkScreen(th)
	if err := s.open(); err != nil {
		t.Fatal(err)
	}
	if got := s.count(th); s.step != linkHosts || got == "" {
		t.Fatalf("step = %v, count = %q, want the hosts counted", s.step, got)
	}

	if _, ask := s.key(sf, enter); ask == nil || s.asking != "buildbox" {
		t.Fatalf("asking = %q, want Enter on the host to ask it", s.asking)
	}
	alpha := revier.ProjectView{Project: revier.Project{Name: "alpha", Path: "/home/user/alpha"}, PathExists: true}
	if _, err := s.asked(nil, askedMsg{host: "buildbox", views: []revier.ProjectView{alpha}}); err != nil {
		t.Fatal(err)
	}
	if s.step != linkRemote || s.title() != "buildbox" {
		t.Fatalf("step = %v, title = %q, want the host's projects", s.step, s.title())
	}

	if res, _ := s.key(sf, enter); res.err != nil || s.step != linkNaming {
		t.Fatalf("step = %v, err = %v, want the step that names the link", s.step, res.err)
	}
	res, _ := s.key(sf, enter)
	if res.err != nil || res.linked == nil {
		t.Fatalf("result = %+v, want the link written", res)
	}
	if got := res.linked.project.Name; got != "rs-buildbox-alpha" {
		t.Errorf("link = %q, want the offered name", got)
	}
	if _, err := os.Stat(config.ProjectFile(root, "rs-buildbox-alpha")); err != nil {
		t.Errorf("the link's file: %v", err)
	}

	if err := s.open(); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.key(sf, esc); !res.closed {
		t.Errorf("result = %+v, want Esc on the hosts to close the dialog", res)
	}
}
