package tui_test

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// surveyedHere applies the survey of this machine alone, as it answers while
// a linked host has yet to.
func surveyedHere(m tui.Model) (tui.Model, tea.Cmd) {
	next, cmd := m.Update(m.Survey()())
	return next.(tui.Model), cmd
}

// besideALink is the world of three projects, the last one open, beside a
// link to buildbox.
func besideALink(t *testing.T) (tui.Model, *hosttest.FakeRuntime, *hosttest.FakeRemote) {
	t.Helper()
	rt, _, c, local := world(t, 3)
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c.Remotes = map[string]revier.Remote{"buildbox": remote}
	projects := append(remoteOnDisk(t, "alpha"), local...)
	m := tui.New(c, projects, stateWith(t, nil), &config.Config{}, time.Millisecond, theme.Default(), "").StaticCursors()
	return resize(m, 150, 30), rt, remote
}

// A project closed here is off its row at the next survey of this machine,
// with no linked host asked: a host that is slow or gone holds no row here
// up (decisions.md D114).
func TestARowHereChangesBeforeALinkedHostAnswers(t *testing.T) {
	m, rt, remote := besideALink(t)
	th := theme.Default()

	m, _ = surveyedHere(m)
	if row := rows(m)[0]; !strings.Contains(row, "project-02") || !strings.Contains(row, th.Glyphs.Running) {
		t.Fatalf("row = %q, want project-02 open", row)
	}

	open, err := rt.Instances(context.Background())
	if err != nil || len(open) != 1 {
		t.Fatalf("instances = %v, %v; want the one workspace", open, err)
	}
	rt.Remove(open[0].Ref)
	m, _ = surveyedHere(m)

	for _, row := range rows(m) {
		if strings.Contains(row, "project-02") && strings.Contains(row, th.Glyphs.Running) {
			t.Errorf("row = %q, want project-02 closed", row)
		}
	}
	if len(remote.Asked) != 0 {
		t.Errorf("the host was asked %d times by the surveys of this machine, want none", len(remote.Asked))
	}
}

// A link's pane says the host is still to answer, and not that the checkout
// is missing: the survey of this machine says nothing about it.
func TestALinkSaysSurveyingUntilItsHostAnswers(t *testing.T) {
	remote := hosttest.NewRemote("buildbox", hostSays("alpha", revier.StatusIdle))
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Remotes: map[string]revier.Remote{"buildbox": remote}}
	m := tui.New(c, remoteOnDisk(t, "alpha"), stateWith(t, nil), &config.Config{}, time.Second, theme.Default(), "").StaticCursors()
	m, _ = surveyedHere(resize(m, 140, 30))

	if body := pane(m); !strings.Contains(body, "surveying") || strings.Contains(body, "not available") || strings.Contains(body, "clone") {
		t.Errorf("pane = %q, want surveying and nothing about the checkout", body)
	}

	next, _ := m.Update(m.AskRemotes()())
	if body := pane(next.(tui.Model)); strings.Contains(body, "surveying") {
		t.Errorf("pane = %q, want the host's answer", body)
	}
}

// The linked hosts are asked one round at a time: a survey that answers
// while a round runs starts no second one, and the first survey after it
// starts the next.
func TestTheLinkedHostsAreAskedOneRoundAtATime(t *testing.T) {
	m, _, remote := besideALink(t)

	m, first := surveyedHere(m)
	m, second := surveyedHere(m)
	m, _ = deliver(m, first)
	m, _ = deliver(m, second)
	if len(remote.Asked) != 1 {
		t.Fatalf("the host was asked %d times over two surveys, want one round", len(remote.Asked))
	}

	m, third := surveyedHere(m)
	deliver(m, third)
	if len(remote.Asked) != 2 {
		t.Errorf("the host was asked %d times, want a second round once the first answered", len(remote.Asked))
	}
}
