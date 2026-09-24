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
}
