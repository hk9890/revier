package tui_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// linkWorld is a surface with an ssh configuration naming buildbox and
// farbox, a scratch configuration root to write links into, and a fake
// buildbox that has the named projects.
func linkWorld(t *testing.T, projects []core.Project, onHost ...string) (tui.Model, *hosttest.FakeRemote, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\n  HostName 10.0.0.7\nHost farbox\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)

	var views []revier.ProjectView
	for _, n := range onHost {
		views = append(views, revier.ProjectView{Project: revier.Project{Name: revier.ProjectName(n), Path: "/home/hans/dev/" + n}, PathExists: true})
	}
	remote := hosttest.NewRemote("buildbox", views...)
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), NewRemote: func(string) revier.Remote { return remote }}
	return resize(refreshed(t, c, projects, stateWith(t, nil), nil), 80, 20), remote, root
}

// step sends a key and runs the command it returns, feeding the message
// back, the way the program does.
func step(m tui.Model, msg tea.KeyMsg) tui.Model {
	m, cmd := send(m, msg)
	if cmd != nil {
		if out := cmd(); out != nil {
			next, _ := m.Update(out)
			m = next.(tui.Model)
		}
	}
	return m
}

var altR = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true}

// alt+r lists the hosts the ssh configuration names; Enter on one asks it
// and shows its projects, with the link here that already points at each.
func TestAltRListsHostsAndEnterListsTheHostsProjects(t *testing.T) {
	m, remote, _ := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha", "beta")

	m = step(m, altR)
	if r := rows(m); len(r) < 2 || !strings.Contains(r[0], "buildbox") || !strings.Contains(r[1], "farbox") {
		t.Fatalf("rows = %q, want the two hosts", r)
	}
	if h := lines(m)[0]; !strings.Contains(h, "link") {
		t.Errorf("header = %q, want the dialog named", h)
	}

	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	if last := remote.Asked[len(remote.Asked)-1]; len(last) != 0 {
		t.Errorf("asked %v, want the dialog's ask to be for every project", remote.Asked)
	}
	r := rows(m)
	if len(r) < 2 || !strings.Contains(r[0], "alpha") || !strings.Contains(r[0], "linked as alpha") {
		t.Errorf("rows = %q, want alpha marked as linked", r)
	}
	if !strings.Contains(r[1], "beta") || strings.Contains(r[1], "linked") {
		t.Errorf("rows = %q, want beta unlinked", r)
	}
	if h := lines(m)[0]; !strings.Contains(h, "buildbox") {
		t.Errorf("header = %q, want the host named", h)
	}
}

// Enter on an unlinked project writes the link and returns to the list,
// with the new row selected and shown as name@host.
func TestEnterOnAHostsProjectWritesTheLink(t *testing.T) {
	m, _, root := linkWorld(t, nil, "beta")

	m = step(m, altR)
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})

	body, err := os.ReadFile(config.ProjectFile(root, "beta"))
	if err != nil || !strings.Contains(string(body), `host = "buildbox"`) {
		t.Fatalf("file = %q, %v; want the link written", body, err)
	}
	if row := selectedRow(t, m); !strings.Contains(row, "beta@buildbox") {
		t.Errorf("selected %q, want the new link", row)
	}
	if f := footer(m); strings.Contains(f, "already") {
		t.Errorf("footer = %q, want no refusal", f)
	}
}

// A project already linked is refused, not written twice.
func TestEnterOnALinkedProjectIsRefused(t *testing.T) {
	m, _, root := linkWorld(t, remoteOnDisk(t, "alpha"), "alpha")
	m = step(m, altR)
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	if f := footer(m); !strings.Contains(f, "already linked as alpha") {
		t.Errorf("footer = %q, want the existing link named", f)
	}
	if _, err := os.Stat(config.ProjectFile(root, "alpha")); err == nil {
		t.Error("a refused link wrote a file")
	}
}

// Esc walks back a level at a time: the host's projects, the hosts, the
// list.
func TestEscWalksBackThroughTheDialog(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	m = step(m, altR)
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = press(m, "esc")
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want the hosts again", r)
	}
	m, _ = press(m, "esc")
	if h := lines(m)[0]; strings.Contains(h, "link") {
		t.Errorf("header = %q, want the project list", h)
	}
}

// A host that does not answer is said in the footer, and the hosts stay
// on screen for another choice.
func TestAHostThatDoesNotAnswerIsSaidInTheFooter(t *testing.T) {
	m, remote, _ := linkWorld(t, nil)
	remote.Err = errors.New("buildbox: connection refused")
	m = step(m, altR)
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	if f := footer(m); !strings.Contains(f, "connection refused") {
		t.Errorf("footer = %q, want the failure", f)
	}
	if r := rows(m); len(r) < 1 || !strings.Contains(r[0], "buildbox") {
		t.Errorf("rows = %q, want the hosts still", r)
	}
}

// With no hosts configured there is nothing to choose from, and the footer
// says where a host would come from.
func TestAltRWithNoHostsSaysSo(t *testing.T) {
	m, _, _ := linkWorld(t, nil)
	t.Setenv("REVIER_SSH_CONFIG", filepath.Join(t.TempDir(), "none"))
	m = step(m, altR)
	if f := footer(m); !strings.Contains(f, "no hosts") {
		t.Errorf("footer = %q, want no hosts said", f)
	}
}

// Esc while a host is being asked abandons the ask: the answer that lands
// afterwards must not pull the surface back into the dialog, nor leave the
// project list under an "asking" footer.
func TestEscDuringAnAskAbandonsIt(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	m = step(m, altR)
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on a host started no ask")
	}
	m, _ = press(m, "esc")
	if f := footer(m); strings.Contains(f, "asking") {
		t.Errorf("footer = %q, want the project list's own legend", f)
	}
	next, _ := m.Update(cmd())
	m = next.(tui.Model)
	if h := lines(m)[0]; strings.Contains(h, "buildbox") {
		t.Errorf("header = %q, want the project list, not the answered host", h)
	}
}

// A survey started before a link was written answers from the list it began
// with. The new row stays, and the cursor with it, rather than going off the
// screen until the next refresh.
func TestALinkSurvivesASurveyThatPredatesIt(t *testing.T) {
	m, _, _ := linkWorld(t, nil, "beta")
	stale := m.Survey()

	m = step(m, altR)
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = step(m, tea.KeyMsg{Type: tea.KeyEnter})

	next, _ := m.Update(stale())
	m = next.(tui.Model)
	if row := selectedRow(t, m); !strings.Contains(row, "beta") {
		t.Errorf("selected %q, want the new link still on the cursor", row)
	}
}
