package tui

import (
	"testing"
	"time"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

func spinModel(status revier.Status) Model {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	m.views = []revier.ProjectView{{Agents: []revier.AgentView{{State: revier.AgentState{Status: status}}}}}
	m.spinning = true
	return m
}

func TestAWorkingAgentsGlyphMovesOnEachSpin(t *testing.T) {
	m := spinModel(revier.StatusRunning)
	before := m.spun().Glyphs.Working
	next, cmd := m.update(spinMsg{})
	if cmd == nil {
		t.Fatal("spin with an agent working scheduled no next frame")
	}
	if after := next.(Model).spun().Glyphs.Working; after == before {
		t.Errorf("working glyph stayed %q after a spin", after)
	}
	if next.(Model).theme.Glyphs.Working != theme.Default().Glyphs.Working {
		t.Error("the spin changed the theme's own working glyph, which the header count draws")
	}
}

func TestTheSpinnerStopsWhenNothingWorks(t *testing.T) {
	next, cmd := spinModel(revier.StatusIdle).update(spinMsg{})
	if cmd != nil {
		t.Error("spin with no agent working scheduled another frame")
	}
	if next.(Model).spinning {
		t.Error("spinner still marked running, so no survey would start it again")
	}
}
