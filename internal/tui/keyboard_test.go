package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/hosttest"
	"github.com/hk9890/revier/internal/tui"
	"github.com/hk9890/revier/pkg/revier"
)

// disambiguating is the model once the terminal answered that it reports key
// disambiguation, as kitty answers the query bubbletea sends at start.
func disambiguating(m tui.Model) tui.Model {
	m, _ = update(m, tea.KeyboardEnhancementsMsg{Flags: ansi.KittyDisambiguateEscapeCodes})
	return m
}

// Each chord as kitty reports it with key disambiguation on. A terminal
// without it reports the same press folded (core.Chord.Terminal).
var kittyChords = []struct {
	chord core.Chord
	msg   tea.KeyPressMsg
	// legacy is whether the target bound to the chord runs where the terminal
	// does not disambiguate: the chord folds to a key the surface keeps.
	legacy bool
}{
	{"ctrl+shift+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl | tea.ModShift}, true},
	{"ctrl+alt+shift+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl | tea.ModAlt | tea.ModShift}, true},
	{"ctrl+shift+i", tea.KeyPressMsg{Code: 'i', Mod: tea.ModCtrl | tea.ModShift}, false}, // tab
	{"ctrl+i", tea.KeyPressMsg{Code: 'i', Mod: tea.ModCtrl}, false},                      // tab
	{"ctrl+m", tea.KeyPressMsg{Code: 'm', Mod: tea.ModCtrl}, false},                      // enter
	{"ctrl+alt+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl | tea.ModAlt}, true},
	{"alt+shift+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModAlt | tea.ModShift}, true},
	{"ctrl+shift+up", tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl | tea.ModShift}, true},
	{"ctrl+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}, true},
}

// webOn is one project whose web target, a window, carries key.
func webOn(t *testing.T, key core.Chord) (*hosttest.Fake, tui.Model) {
	t.Helper()
	raw := []revier.Project{{Name: "solo", Path: "/p/solo", Targets: []revier.Target{
		{Name: "home", Home: true, Runtime: &revier.Realization{
			Launch: []string{"x"}, Match: revier.Match{Title: "^session:solo$"}}},
		{Name: "web", Key: string(key), Window: &revier.Realization{
			Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}}},
	}}}
	wm := hosttest.New("wm")
	c := &core.Core{Runtime: hosttest.NewRuntime("rt"), Window: wm}
	return wm, refreshed(t, c, core.Prepare(raw), stateWith(t, nil), nil)
}

// A target key reaches the surface as pressed where the terminal reports key
// disambiguation, so every chord runs its target there. Elsewhere it reaches
// it as the folded key, and runs its target only where the surface does not
// keep that key for itself.
func TestATargetKeyRunsAsTheTerminalReportsIt(t *testing.T) {
	for _, tc := range kittyChords {
		for _, unfolds := range []bool{false, true} {
			wm, m := webOn(t, tc.chord)
			if unfolds {
				m = disambiguating(m)
			}
			_, cmd := send(m, tc.msg)
			runAll(cmd)
			want := 0
			if unfolds || tc.legacy {
				want = 1
			}
			if got := len(wm.Opened); got != want {
				t.Errorf("%s, disambiguation %v: the web window opened %d times, want %d", tc.chord, unfolds, got, want)
			}
		}
	}
}

// A ctrl+shift chord that no target binds does what ctrl and its letter do,
// with key disambiguation or without: ctrl+shift+w deletes a word of the
// query as ctrl+w does, and ctrl+shift+n moves down as ctrl+n does.
func TestAnUnboundCtrlShiftChordDoesWhatCtrlDoes(t *testing.T) {
	_, _, c, projects := world(t, 3)
	for _, unfolds := range []bool{false, true} {
		m := refreshed(t, c, projects, stateWith(t, nil), nil)
		if unfolds {
			m = disambiguating(m)
		}
		for _, letter := range []rune{'w', 'n'} {
			start, _ := press(m, "project")
			if letter == 'n' {
				start = m
			}
			want, _ := send(start, tea.KeyPressMsg{Code: letter, Mod: tea.ModCtrl})
			got, _ := send(start, tea.KeyPressMsg{Code: letter, Mod: tea.ModCtrl | tea.ModShift})
			if drawn(got) != drawn(want) {
				t.Errorf("disambiguation %v: ctrl+shift+%c draws\n%s\nwant what ctrl+%c draws\n%s", unfolds, letter, screen(got), letter, screen(want))
			}
		}
	}
}

// In a focused text field, ctrl+i is tab, ctrl+m is enter and ctrl+[ is esc,
// as a terminal without key disambiguation sends them, also in one that
// reports them apart. The fields are the query and the new project's path.
func TestAControlKeyInATextFieldIsItsLegacyKey(t *testing.T) {
	pairs := []struct {
		name          string
		press, legacy tea.KeyPressMsg
	}{
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"ctrl+i", tea.KeyPressMsg{Code: 'i', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyTab}},
		{"ctrl+m", tea.KeyPressMsg{Code: 'm', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"ctrl+[", tea.KeyPressMsg{Code: '[', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: tea.KeyEscape}},
	}
	fields := map[string]string{"the query": "project-01", "the path": "/nowhere/x"}
	// focused is a new surface with the field focused and text in it. Each
	// press gets its own: the screens are held by pointer, and an enter on
	// the path writes a project file.
	focused := func(field string, unfolds bool) tui.Model {
		t.Setenv("REVIER_CONFIG_HOME", t.TempDir())
		t.Setenv("REVIER_STATE_HOME", t.TempDir())
		_, _, c, projects := world(t, 2)
		m := resize(refreshed(t, c, projects, stateWith(t, nil), nil), 120, 30)
		if unfolds {
			m = disambiguating(m)
		}
		if field == "the path" {
			m, _ = press(m, "alt+n")
		}
		m, _ = press(m, fields[field])
		return m
	}
	for field := range fields {
		for _, unfolds := range []bool{false, true} {
			for _, p := range pairs {
				want, wantCmd := send(focused(field, unfolds), p.legacy)
				got, gotCmd := send(focused(field, unfolds), p.press)
				if drawn(got) != drawn(want) || (gotCmd == nil) != (wantCmd == nil) {
					t.Errorf("%s, disambiguation %v: %s draws\n%s\nwant what %s draws\n%s", field, unfolds, p.name, screen(got), p.legacy, screen(want))
				}
			}
		}
	}
}

// ctrl+o is an action's, and ctrl+shift+o the editor's. Where the terminal
// reports key disambiguation, each press means its own; elsewhere
// ctrl+shift+o reaches the surface as ctrl+o and runs the action, as it did
// before the terminal could tell the two apart.
func TestACtrlShiftTargetKeyRunsOnlyWhereTheTerminalTellsItApart(t *testing.T) {
	for _, unfolds := range []bool{false, true} {
		_, wm, c, projects := world(t, 1)
		m := refreshed(t, c, projects, stateWith(t, nil), []config.Action{{Key: "ctrl-o", Name: "sync"}})
		if unfolds {
			m = disambiguating(m)
		}

		after, cmd := send(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl | tea.ModShift})
		if cmd == nil {
			t.Fatalf("disambiguation %v: ctrl+shift+o ran nothing", unfolds)
		}
		after = run(after, cmd)
		ranAction := strings.Contains(footer(after), `action "sync"`)
		switch {
		case unfolds && (len(wm.Opened) != 1 || ranAction):
			t.Errorf("ctrl+shift+o with disambiguation: editor opened %d times, action ran %v; want the editor alone", len(wm.Opened), ranAction)
		case !unfolds && (len(wm.Opened) != 0 || !ranAction):
			t.Errorf("ctrl+shift+o without disambiguation: editor opened %d times, action ran %v; want the action alone", len(wm.Opened), ranAction)
		}

		opened := len(wm.Opened)
		after, cmd = send(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatalf("disambiguation %v: ctrl+o ran nothing", unfolds)
		}
		if after = run(after, cmd); len(wm.Opened) != opened || !strings.Contains(footer(after), `action "sync"`) {
			t.Errorf("ctrl+o, disambiguation %v: footer %q, editor opened %d more; want the action alone", unfolds, footer(after), len(wm.Opened)-opened)
		}
	}
}

// With key disambiguation, ctrl+shift+u reaches the surface, so the footer
// names home's key, which it leaves out where the query takes ctrl+u
// (TestFooterNamesTheHighlightedProjectsTargetKeys).
func TestTheFooterNamesACtrlShiftKeyTheTerminalTellsApart(t *testing.T) {
	_, _, c, projects := world(t, 2)
	m := disambiguating(resize(refreshed(t, c, projects, stateWith(t, nil), nil), 200, 20))
	if f := footer(m); !strings.Contains(f, "ctrl+shift+u home") {
		t.Errorf("footer = %q, want home's key", f)
	}
}
