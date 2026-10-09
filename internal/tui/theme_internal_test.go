package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
)

// A theme change repaints the field of every screen, also of a screen that is
// not up: the next visit must not show a field in the old theme's colours.
func TestAThemeChangeRepaintsTheFieldOfEveryScreen(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, "", &config.Config{}, time.Second, theme.Default(), "")
	th, err := theme.Lookup("catppuccin-latte", "")
	if err != nil {
		t.Fatal(err)
	}
	if th.Accent.GetForeground() == theme.Default().Accent.GetForeground() {
		t.Fatal("the two themes have one accent, so the test would prove nothing")
	}

	m.applyTheme(th)

	for name, in := range map[string]textinput.Model{
		"project query": m.input, "agent query": m.ainput, "agent list query": m.agents.query,
		"new-project path": m.create.path, "session name": m.sessions.name,
		"trigger key": m.config.chord, "project field": m.proj.edit,
		"link query": m.link.query, "link name": m.link.name,
	} {
		if in.PromptStyle.GetForeground() != th.Accent.GetForeground() {
			t.Errorf("the %s is still in the old theme's colours", name)
		}
	}
}
