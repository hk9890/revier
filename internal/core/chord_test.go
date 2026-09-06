package core_test

import (
	"strings"
	"testing"

	"github.com/hk9890/revier/internal/core"
)

// Every spelling of a chord that exists in this system has to reach the same
// canonical form, or "does revier hold this key" answers no for a key it
// holds.
func TestParseChordAcceptsEverySpelling(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want core.Chord
	}{
		{"ctrl-shift-u", "ctrl+shift+u"},
		{"ctrl+shift+u", "ctrl+shift+u"},
		{"<Shift><Control>u", "ctrl+shift+u"},
		{"<Control><Shift>u", "ctrl+shift+u"},
		{"<CONTROL><shift>U", "ctrl+shift+u"},
		{"<Primary>u", "ctrl+u"},
		{"<Primary><Shift>U", "ctrl+shift+u"},
		{"alt-space", "alt+space"},
		{"<Alt>space", "alt+space"},
		{"<Super>Return", "super+return"},
		{"<Mod4>q", "super+q"},
		{"<Alt>F4", "alt+f4"},
		{"f12", "f12"},
		{"  <Alt>space  ", "alt+space"},
	} {
		got, err := core.ParseChord(tc.in)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseChord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A modifier revier does not know must not be dropped. Dropping it makes two
// different chords compare equal, which reports a key as held when it is not.
func TestParseChordRejectsWhatItCannotRead(t *testing.T) {
	for _, in := range []string{"", "   ", "<Hyper", "u<Alt>", "<Nonsense>u", "ctrl-", "<Alt>"} {
		if got, err := core.ParseChord(in); err == nil {
			t.Errorf("ParseChord(%q) = %q, want an error", in, got)
		}
	}
}

// An unknown modifier names itself, so a typo in a project file is fixable
// without reading this source.
func TestUnknownModifierNamesItself(t *testing.T) {
	_, err := core.ParseChord("<Nonsense>u")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "Nonsense") {
		t.Errorf("error = %q, want it to name the modifier", err)
	}
}

// GNOME's own spelling, back out. Key names come from gdk_keyval_from_name,
// which is case sensitive: "return" is not a key and "Return" is.
func TestChordRendersTheWayGNOMEStoresIt(t *testing.T) {
	for _, tc := range []struct {
		in   core.Chord
		want string
	}{
		{"ctrl+shift+u", "<Shift><Control>u"},
		{"alt+space", "<Alt>space"},
		{"super+return", "<Super>Return"},
		{"alt+f4", "<Alt>F4"},
		{"ctrl+alt+delete", "<Control><Alt>Delete"},
		{"u", "u"},
	} {
		if got := tc.in.GNOME(); got != tc.want {
			t.Errorf("Chord(%q).GNOME() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The round trip is what the comparison relies on: whatever GNOME reports,
// rendered back, has to reach the same canonical chord again.
func TestRoundTrip(t *testing.T) {
	for _, in := range []string{
		"<Shift><Control>u", "<Alt>space", "<Super>Return", "<Alt>F4",
		"<Primary><Alt>Delete", "<Super>Page_Up",
	} {
		first, err := core.ParseChord(in)
		if err != nil {
			t.Errorf("ParseChord(%q): %v", in, err)
			continue
		}
		again, err := core.ParseChord(first.GNOME())
		if err != nil {
			t.Errorf("ParseChord(%q.GNOME() = %q): %v", first, first.GNOME(), err)
			continue
		}
		if again != first {
			t.Errorf("%q: round trip %q -> %q -> %q", in, first, first.GNOME(), again)
		}
	}
}
