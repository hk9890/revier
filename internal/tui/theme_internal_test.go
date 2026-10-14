package tui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/theme"
)

// A theme change repaints the field of every screen, also of a screen that is
// not up: the next visit must not show a field in the old theme's colours.
func TestAThemeChangeRepaintsTheFieldOfEveryScreen(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
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
		if in.Styles().Focused.Prompt.GetForeground() != th.Accent.GetForeground() {
			t.Errorf("the %s is still in the old theme's colours", name)
		}
	}
}

// A terminal with 16 or 256 colours gets the theme in the indices revier has
// always drawn it with, and so does a theme chosen after it said so.
func TestTheThemeIsInTheColoursTheTerminalShows(t *testing.T) {
	m := New(&core.Core{Runtime: hosttest.NewRuntime("rt")}, nil, t.TempDir(), &config.Config{}, time.Second, theme.Default(), "")
	next, _ := m.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	m = next.(Model)

	want := theme.Default().In(colorprofile.ANSI)
	if got := m.theme.Hover.GetBackground(); got != want.Hover.GetBackground() {
		t.Errorf("hover in 16 colours = %v, want %v", got, want.Hover.GetBackground())
	}
	if got := m.input.Styles().Focused.Prompt.GetForeground(); got != want.Accent.GetForeground() {
		t.Errorf("query prompt in 16 colours = %v, want %v", got, want.Accent.GetForeground())
	}

	latte, err := theme.Lookup("catppuccin-latte", "")
	if err != nil {
		t.Fatal(err)
	}
	m.applyTheme(latte)
	if got, want := m.theme.Hover.GetBackground(), latte.In(colorprofile.ANSI).Hover.GetBackground(); got != want {
		t.Errorf("latte hover in 16 colours = %v, want %v", got, want)
	}
}
