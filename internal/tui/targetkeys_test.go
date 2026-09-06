package tui

import "testing"

// A target key is bound under the one spelling bubbletea reports, whatever
// spelling the project file used. Lowercasing and swapping the separator is
// not enough: "shift-ctrl-o" would be bound as "shift+ctrl+o", which no
// keypress produces, while `revier keys status` reports the same target as
// holding "ctrl+shift+o".
func TestATargetKeyIsBoundUnderItsCanonicalSpelling(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
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

// A bare letter is a filter character, and a key that does not parse binds
// nothing rather than binding something no press can reach.
func TestAKeyThatBindsNothingIsRefused(t *testing.T) {
	for _, in := range []string{"o", "ctrl-", "hyperx-o", ""} {
		if got, ok := chordName(in); ok {
			t.Errorf("chordName(%q) = %q, true; want it bound to nothing", in, got)
		}
	}
}
