package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/ledger"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// A machine whose check has not answered says so, and holds no other row: the
// cursor moves past it, and Enter asks a machine whatever its check says.
func TestAMachineStillCheckedHoldsNoOtherRow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	for _, host := range []string{"buildbox", "farbox"} {
		if err := config.AddMachine(root, host); err != nil {
			t.Fatal(err)
		}
	}
	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), core: &core.Core{}, list: 60, pane: 40, page: 5}
	s := newLinkScreen(th)
	checks, err := s.open(sf.core)
	if err != nil || checks == nil {
		t.Fatalf("open = %v, %v; want the machines asked", checks, err)
	}
	s.checked(checkedMsg{host: "farbox", ask: s.checks["farbox"].ask, version: "0.21.0"})

	states := map[string]string{}
	for _, item := range s.hosts.Items() {
		it := item.(hostItem)
		states[it.host], _ = it.check.state(th)
	}
	if states["buildbox"] != "checking…" || states["farbox"] != "revier 0.21.0" {
		t.Errorf("states = %v, want buildbox checking and farbox answered", states)
	}

	s.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyDown}))
	if _, ask := s.key(sf, pressedKey(tea.KeyPressMsg{Code: tea.KeyEnter})); ask == nil || s.asking != "farbox" {
		t.Errorf("asking = %q, want Enter on farbox to ask it while buildbox is checked", s.asking)
	}
}

// machinesScreen is a remote screen opened over config.toml's machines
// buildbox and farbox, with an ssh configuration that names router too.
func machinesScreen(t *testing.T) (*linkScreen, surface) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	for _, host := range []string{"buildbox", "farbox"} {
		if err := config.AddMachine(root, host); err != nil {
			t.Fatal(err)
		}
	}
	sshConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(sshConfig, []byte("Host buildbox\nHost farbox\nHost router\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIER_SSH_CONFIG", sshConfig)
	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), core: &core.Core{}, list: 60, pane: 40, page: 5}
	s := newLinkScreen(th)
	if _, err := s.open(sf.core); err != nil {
		t.Fatal(err)
	}
	return &s, sf
}

// "new" and a remove's question each leave the machines, so an ask of a
// machine's projects still out is abandoned with them: its answer must not
// open the host's projects over the step the user moved to.
func TestNewAndARemoveQuestionAbandonAnAskStillOut(t *testing.T) {
	enter := pressedKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	answer := askedMsg{host: "buildbox", views: []revier.ProjectView{{Project: revier.Project{Name: "alpha"}}}}

	s, sf := machinesScreen(t)
	s.key(sf, enter)
	if err := s.openAdd(); err != nil {
		t.Fatal(err)
	}
	_, _ = s.asked(nil, answer)
	if s.step != linkAdding {
		t.Errorf("step = %v, want the ssh hosts still up after the answer", s.step)
	}

	s, sf = machinesScreen(t)
	s.key(sf, enter)
	link := core.Project{Project: revier.Project{Name: "alpha", Remote: &revier.Link{Host: "buildbox", Project: "alpha"}}}
	if _, err := s.remove(sf.core, []core.Project{link}); err != nil || s.removing != "buildbox" {
		t.Fatalf("removing = %q, err = %v, want the question", s.removing, err)
	}
	_, _ = s.asked(nil, answer)
	if s.step != linkHosts || s.removing != "buildbox" {
		t.Errorf("step = %v, removing = %q, want the question still over the machines", s.step, s.removing)
	}
}

// Each visit asks again, and an answer to an ask of an earlier visit is not
// the one a row waits for: a machine that was down then and is up now must
// not read as down.
func TestAnAnswerOfAnEarlierVisitIsNotTaken(t *testing.T) {
	s, sf := machinesScreen(t)
	earlier := s.checks["buildbox"].ask
	if _, err := s.open(sf.core); err != nil {
		t.Fatal(err)
	}
	s.checked(checkedMsg{host: "buildbox", ask: earlier, err: errors.New("buildbox: no answer on port 22")})
	if got, _ := s.checks["buildbox"].state(sf.theme); got != "checking…" {
		t.Errorf("state = %q, want the ask of this visit still out", got)
	}
}

// The remote screen runs with no root model: its three steps, the link the
// last one writes, and Esc on the first one, which is all it tells the
// surface.
func TestTheLinkScreenWalksItsStepsWithNoRootModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REVIER_CONFIG_HOME", root)
	if err := config.AddMachine(root, "buildbox"); err != nil {
		t.Fatal(err)
	}

	th := theme.Default()
	sf := surface{theme: th, spun: th, keys: newKeyMap(nil), core: &core.Core{Ledger: ledger.File{Root: t.TempDir()}}, list: 60, pane: 40, page: 5}
	enter, esc := pressedKey(tea.KeyPressMsg{Code: tea.KeyEnter}), pressedKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	s := newLinkScreen(th)
	if _, err := s.open(sf.core); err != nil {
		t.Fatal(err)
	}
	if got := s.count(th); s.step != linkHosts || got == "" {
		t.Fatalf("step = %v, count = %q, want the machines counted", s.step, got)
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

	if _, err := s.open(sf.core); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.key(sf, esc); !res.closed {
		t.Errorf("result = %+v, want Esc on the machines to close the dialog", res)
	}
}
