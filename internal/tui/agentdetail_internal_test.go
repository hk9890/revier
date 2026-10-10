package tui

import (
	"testing"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// An agent is the same agent whatever its instance is titled: a window host
// retitles an instance as the program in it retitles itself.
func TestAnAgentsKeyIgnoresItsInstancesTitle(t *testing.T) {
	a := revier.AgentView{Panel: "1", Ref: revier.TargetRef{Host: "wm", ID: "7", Title: "✳ fixing the tests"}}
	b := a
	b.Ref.Title = "⠏ fixing the tests"
	if keyOf(a) != keyOf(b) {
		t.Errorf("keyOf differs by title: %+v and %+v", keyOf(a), keyOf(b))
	}
	b.Ref.ID = "8"
	if keyOf(a) == keyOf(b) {
		t.Error("keyOf is the same for two instances")
	}
}

// The asks for details are not answered in order: an answer older than the
// one the pane holds is dropped, as is one for a project the cursor left. An
// agent an answer has nothing for keeps what it said before.
func TestTheNewestAnswerForTheProjectShownIsTaken(t *testing.T) {
	one := agentKey{host: "rt", id: "1", panel: "1"}
	two := agentKey{host: "rt", id: "1", panel: "2"}
	said := func(text string) revier.AgentDetail { return revier.AgentDetail{Message: text, At: time.Unix(1, 0)} }
	m := Model{aasked: "demo"}

	m.took(detailsMsg{project: "demo", seq: 2, said: map[agentKey]revier.AgentDetail{one: said("newer"), two: said("two speaks")}})
	m.took(detailsMsg{project: "demo", seq: 1, said: map[agentKey]revier.AgentDetail{one: said("older")}})
	if got := m.adetails[one].Message; got != "newer" {
		t.Errorf("after an older answer the pane holds %q, want the newer", got)
	}
	m.took(detailsMsg{project: "other", seq: 3, said: map[agentKey]revier.AgentDetail{one: said("another project")}})
	if got := m.adetails[one].Message; got != "newer" {
		t.Errorf("after another project's answer the pane holds %q, want this project's", got)
	}
	m.took(detailsMsg{project: "demo", seq: 4, said: map[agentKey]revier.AgentDetail{one: {}, two: said("two again")}})
	if got := m.adetails[one].Message; got != "newer" {
		t.Errorf("after a read that failed the pane holds %q, want what the agent said before", got)
	}
	if got := m.adetails[two].Message; got != "two again" {
		t.Errorf("the other agent holds %q, want its new word", got)
	}
	// A turn read with no message in it: the last message is further back
	// than the probe reads, and still the last, with the time it was said.
	m.took(detailsMsg{project: "demo", seq: 5, said: map[agentKey]revier.AgentDetail{one: {Prompt: "now push it"}}})
	if got := m.adetails[one]; got.Message != "newer" || !got.At.Equal(time.Unix(1, 0)) || got.Prompt != "now push it" {
		t.Errorf("after a turn with no message the pane holds %+v, want the turn with the message and the time before it", got)
	}
}

func TestAgoSaysTheLargestWholeUnit(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		before time.Duration
		want   string
	}{
		{20 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{14 * time.Minute, "14 minutes ago"},
		{90 * time.Minute, "1 hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{50 * time.Hour, "2 days ago"},
	} {
		if got := ago(now, now.Add(-tc.before)); got != tc.want {
			t.Errorf("ago(%v before) = %q, want %q", tc.before, got, tc.want)
		}
	}
}
