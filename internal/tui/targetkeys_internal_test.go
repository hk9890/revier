package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/pkg/revier"
)

// A target key is bound under the one canonical spelling, whatever spelling
// the project file used. Lowercasing and swapping the separator is not enough:
// "shift-ctrl-o" would be bound as "shift+ctrl+o", which no keypress produces,
// while `revier keys status` reports the same target as holding
// "ctrl+shift+o".
func TestATargetKeyIsBoundUnderItsCanonicalSpelling(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want core.Chord
	}{
		{"ctrl-shift-o", "ctrl+shift+o"},
		{"shift-ctrl-o", "ctrl+shift+o"},
		{"Ctrl+Shift+O", "ctrl+shift+o"},
		{"<Shift><Control>o", "ctrl+shift+o"},
		{"<Primary>o", "ctrl+o"},
	} {
		got, ok := chordName(tc.in)
		if !ok || got != tc.want {
			t.Errorf("chordName(%q) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}
}

// A target refused at load binds no key here: the key it carries may be the
// one it was refused for, held by another target, which it would take over.
func TestARefusedTargetBindsNoKey(t *testing.T) {
	p := core.PrepareProject(revier.Project{Name: "demo", Path: "/p", Targets: []revier.Target{
		{Name: "editor", Key: "ctrl-o", Window: &revier.Realization{Launch: []string{"code"}, Match: revier.Match{Class: "^code$"}}},
		{Name: "pulls", Key: "ctrl-o", Window: &revier.Realization{Launch: []string{"chrome"}, Match: revier.Match{Class: "^chrome$"}}},
	}})
	p.Refuse(1, errors.New(`targets "editor" and "pulls" share key "ctrl+o"`))
	if got := targetKeys([]core.Project{p}, newKeyMap(nil)); got["ctrl+o"] != "editor" {
		t.Errorf("ctrl+o = %q, want editor, the target that holds the key", got["ctrl+o"])
	}
}

// A letter is a filter character, shifted or not, and a key that does not
// parse binds nothing rather than binding something no press can reach.
func TestAKeyThatBindsNothingIsRefused(t *testing.T) {
	for _, in := range []string{"o", "shift-o", "ctrl-", "hyperx-o", ""} {
		if got, ok := chordName(in); ok {
			t.Errorf("chordName(%q) = %q, true; want it bound to nothing", in, got)
		}
	}
}

// A press is read as the chord it is, not compared as bubbletea's text for
// it. The messages are the ones bubbletea's input reader produces: ctrl+alt+o
// is ESC then 0x0f, which it names "alt+ctrl+o"; alt+shift+o is ESC then "O".
func TestAPressIsReadAsItsCanonicalChord(t *testing.T) {
	for _, tc := range []struct {
		msg  tea.KeyMsg
		want core.Chord
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlO, Alt: true}, "ctrl+alt+o"},
		{tea.KeyMsg{Type: tea.KeyCtrlO}, "ctrl+o"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O"), Alt: true}, "alt+shift+o"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e"), Alt: true}, "alt+e"},
		{tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" "), Alt: true}, "alt+space"},
		{tea.KeyMsg{Type: tea.KeyF5}, "f5"},
	} {
		got, ok := pressed(tc.msg)
		if !ok || got != tc.want {
			t.Errorf("pressed(%q) = %q, %v; want %q", tc.msg, got, ok, tc.want)
		}
	}
}

// Text is no chord: runes typed faster than the reader reads arrive as one
// message, and a paste is text whatever it spells.
func TestTypedTextIsNoChord(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("ctrl+o")},
		{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true},
	} {
		if got, ok := pressed(msg); ok {
			t.Errorf("pressed(%q) = %q, want no chord", msg, got)
		}
	}
}

// Two projects may give one chord to two targets. A press means the
// highlighted project's own, whichever the vocabulary kept; a chord that only
// folds to the press yields to a target that declares it outright, as
// targetKeys binds it.
func TestAChordMeansTheHighlightedProjectsOwnTarget(t *testing.T) {
	window := func(class string) *revier.Realization {
		return &revier.Realization{Launch: []string{class}, Match: revier.Match{Class: "^" + class + "$"}}
	}
	a := core.PrepareProject(revier.Project{Name: "a", Path: "/a", Targets: []revier.Target{
		{Name: "editor", Key: "ctrl-o", Window: window("code")},
	}})
	b := core.PrepareProject(revier.Project{Name: "b", Path: "/b", Targets: []revier.Target{
		{Name: "web", Key: "ctrl-o", Window: window("firefox")},
	}})
	c := core.PrepareProject(revier.Project{Name: "c", Path: "/c", Targets: []revier.Target{
		{Name: "diff", Key: "ctrl-shift-o", Window: window("meld")},
	}})
	m := Model{projects: []core.Project{a, b, c}}
	m.tkeys = targetKeys(m.projects, newKeyMap(nil))

	for _, tc := range []struct {
		p    core.Project
		want revier.TargetName
	}{
		{a, "editor"},
		{b, "web"},
		{c, m.tkeys["ctrl+o"]}, // ctrl+shift+o is a desktop key only here
	} {
		if got := m.ownTarget(tc.p, "ctrl+o", m.tkeys["ctrl+o"]); got != tc.want {
			t.Errorf("ctrl+o on %s = %q, want %q", tc.p.Name, got, tc.want)
		}
	}
}
